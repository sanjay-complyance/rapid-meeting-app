package fathom

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"rapidassistant/backend/internal/models"
)

const providerName = "fathom"

type Client struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
}

func NewClient(apiKey string) *Client {
	return &Client{
		apiKey:     apiKey,
		baseURL:    "https://api.fathom.video/v1",
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *Client) Available() bool {
	return strings.TrimSpace(c.apiKey) != ""
}

func (c *Client) FetchRecording(ctx context.Context, recordingID string) (models.FathomImportInput, error) {
	recordingID = strings.TrimSpace(recordingID)
	if recordingID == "" {
		return models.FathomImportInput{}, fmt.Errorf("recording_id is required")
	}
	if !c.Available() {
		return models.FathomImportInput{}, fmt.Errorf("FATHOM_API_KEY is not configured")
	}

	transcriptPayload, err := c.getJSON(ctx, "/recordings/"+url.PathEscape(recordingID)+"/transcript")
	if err != nil {
		return models.FathomImportInput{}, err
	}
	summaryPayload, err := c.getJSON(ctx, "/recordings/"+url.PathEscape(recordingID)+"/summary")
	if err != nil {
		return models.FathomImportInput{}, err
	}

	meetingPayload, _ := c.findMeeting(ctx, recordingID)
	return mergeImportInput(recordingID, transcriptPayload, summaryPayload, meetingPayload)
}

func VerifyWebhook(secret, webhookID, webhookTimestamp, webhookSignature string, body []byte, tolerance time.Duration) error {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return fmt.Errorf("missing Fathom webhook secret")
	}
	if webhookID == "" || webhookTimestamp == "" || webhookSignature == "" {
		return fmt.Errorf("missing required Fathom webhook headers")
	}

	timestamp, err := strconv.ParseInt(webhookTimestamp, 10, 64)
	if err != nil {
		return fmt.Errorf("parse webhook timestamp: %w", err)
	}
	signedAt := time.Unix(timestamp, 0)
	if tolerance > 0 && time.Since(signedAt) > tolerance {
		return fmt.Errorf("webhook timestamp outside allowed tolerance")
	}

	key, err := decodeSecret(secret)
	if err != nil {
		return fmt.Errorf("decode webhook secret: %w", err)
	}

	signedContent := []byte(webhookID + "." + webhookTimestamp + "." + string(body))
	mac := hmac.New(sha256.New, key)
	mac.Write(signedContent)
	expected := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	for _, part := range strings.Fields(webhookSignature) {
		for _, candidate := range strings.Split(part, ",") {
			candidate = strings.TrimSpace(candidate)
			if candidate == "" {
				continue
			}
			value := candidate
			if strings.Contains(candidate, ",") {
				value = strings.TrimSpace(candidate[strings.LastIndex(candidate, ",")+1:])
			}
			if strings.HasPrefix(value, "v1,") {
				value = strings.TrimPrefix(value, "v1,")
			}
			if strings.HasPrefix(value, "v1=") {
				value = strings.TrimPrefix(value, "v1=")
			}
			if hmac.Equal([]byte(value), []byte(expected)) {
				return nil
			}
		}
	}
	return fmt.Errorf("invalid webhook signature")
}

func ParseWebhookPayload(body []byte) (models.FathomImportInput, error) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return models.FathomImportInput{}, fmt.Errorf("decode webhook payload: %w", err)
	}
	meeting := payload
	if nested, ok := payload["data"].(map[string]any); ok {
		meeting = nested
	}
	return importInputFromMeetingMap(meeting, body)
}

func mergeImportInput(recordingID string, transcriptPayload, summaryPayload, meetingPayload map[string]any) (models.FathomImportInput, error) {
	meetingInput, err := importInputFromMeetingMap(meetingPayload, nil)
	if err != nil && len(meetingPayload) > 0 {
		return models.FathomImportInput{}, err
	}

	segments, err := transcriptSegmentsFromPayload(transcriptPayload)
	if err != nil {
		return models.FathomImportInput{}, err
	}

	summaryMarkdown := firstNonEmpty(
		stringPtrValue(meetingInput.SummaryMarkdown),
		extractSummaryMarkdown(summaryPayload),
	)
	actionItemsJSON := meetingInput.ActionItemsJSON
	if actionItemsJSON == nil {
		if encoded, ok := encodeActionItems(extractActionItems(meetingPayload)); ok {
			actionItemsJSON = &encoded
		}
	}

	title := strings.TrimSpace(meetingInput.Title)
	if title == "" {
		title = "Fathom meeting " + recordingID
	}

	draftPreview := buildDraftPreviewJSON("fathom", len(segments), len(uniqueSpeakerNames(segments)))
	input := models.FathomImportInput{
		RecordingID:        recordingID,
		Title:              title,
		MeetingTitle:       meetingInput.MeetingTitle,
		URL:                meetingInput.URL,
		ShareURL:           meetingInput.ShareURL,
		RecordedByName:     meetingInput.RecordedByName,
		TranscriptLanguage: firstStringPtr(meetingInput.TranscriptLanguage, extractString(meetingPayload, "transcript_language")),
		SummaryMarkdown:    firstStringPtr(meetingInput.SummaryMarkdown, summaryMarkdown),
		ActionItemsJSON:    actionItemsJSON,
		ExternalCreatedAt:  firstTimePtr(meetingInput.ExternalCreatedAt, extractTime(meetingPayload, "created_at"), extractTime(meetingPayload, "recording_started_at")),
		DraftPreviewJSON:   &draftPreview,
		Segments:           segments,
	}
	return input, nil
}

func importInputFromMeetingMap(meeting map[string]any, rawBody []byte) (models.FathomImportInput, error) {
	if len(meeting) == 0 {
		return models.FathomImportInput{}, nil
	}

	recordingID := extractRecordingID(meeting)
	segments, err := transcriptSegmentsFromMeeting(meeting)
	if err != nil && len(segments) == 0 {
		return models.FathomImportInput{}, err
	}

	summaryMarkdown := extractSummaryMarkdown(meeting)
	actionItems, hasActionItems := encodeActionItems(extractActionItems(meeting))
	draftPreview := buildDraftPreviewJSON("fathom", len(segments), len(uniqueSpeakerNames(segments)))
	input := models.FathomImportInput{
		RecordingID:        recordingID,
		Title:              firstNonEmpty(extractString(meeting, "title"), extractString(meeting, "meeting_title"), "Fathom meeting "+recordingID),
		MeetingTitle:       ptrFromString(extractString(meeting, "meeting_title")),
		URL:                ptrFromString(extractString(meeting, "url")),
		ShareURL:           ptrFromString(extractString(meeting, "share_url")),
		RecordedByName:     ptrFromString(extractNestedString(meeting, []string{"recorded_by", "name"}, []string{"recorded_by", "display_name"})),
		TranscriptLanguage: ptrFromString(extractString(meeting, "transcript_language")),
		SummaryMarkdown:    ptrFromString(summaryMarkdown),
		ExternalCreatedAt:  extractTime(meeting, "created_at"),
		DraftPreviewJSON:   &draftPreview,
		Segments:           segments,
	}
	if hasActionItems {
		input.ActionItemsJSON = &actionItems
	}
	if len(rawBody) > 0 {
		input.ProviderPayloadJSON = string(rawBody)
	}
	return input, nil
}

func (c *Client) getJSON(ctx context.Context, path string) (map[string]any, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, fmt.Errorf("build Fathom request: %w", err)
	}
	request.Header.Set("X-Api-Key", c.apiKey)
	request.Header.Set("Accept", "application/json")

	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("request Fathom %s: %w", path, err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(io.LimitReader(response.Body, 5<<20))
	if err != nil {
		return nil, fmt.Errorf("read Fathom response: %w", err)
	}
	if response.StatusCode >= 300 {
		return nil, fmt.Errorf("Fathom %s returned %s: %s", path, response.Status, strings.TrimSpace(string(body)))
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode Fathom response: %w", err)
	}
	return payload, nil
}

func (c *Client) findMeeting(ctx context.Context, recordingID string) (map[string]any, error) {
	cursor := ""
	for attempt := 0; attempt < 10; attempt++ {
		path := "/meetings?include_action_items=true&include_summary=true&limit=100"
		if cursor != "" {
			path += "&cursor=" + url.QueryEscape(cursor)
		}
		payload, err := c.getJSON(ctx, path)
		if err != nil {
			return nil, err
		}
		for _, meeting := range extractMeetingItems(payload) {
			if extractRecordingID(meeting) == recordingID {
				return meeting, nil
			}
		}
		cursor = extractNextCursor(payload)
		if cursor == "" {
			break
		}
	}
	return nil, fmt.Errorf("Fathom recording %s not found in meeting list", recordingID)
}

func transcriptSegmentsFromPayload(payload map[string]any) ([]models.FathomSegmentInput, error) {
	if len(payload) == 0 {
		return nil, fmt.Errorf("empty transcript payload")
	}

	items := extractTranscriptItems(payload)
	if len(items) == 0 {
		return nil, fmt.Errorf("transcript payload did not contain any transcript items")
	}
	return buildSegments(items, nil)
}

func transcriptSegmentsFromMeeting(meeting map[string]any) ([]models.FathomSegmentInput, error) {
	items := extractTranscriptItems(meeting)
	if len(items) == 0 {
		return nil, nil
	}
	recordingEnd := extractTime(meeting, "recording_ended_at")
	return buildSegments(items, recordingEnd)
}

func buildSegments(items []map[string]any, recordingEnd *time.Time) ([]models.FathomSegmentInput, error) {
	segments := make([]models.FathomSegmentInput, 0, len(items))

	for index, item := range items {
		startMS, err := parseTimestampMS(extractString(item, "timestamp"))
		if err != nil {
			return nil, fmt.Errorf("parse transcript timestamp at index %d: %w", index, err)
		}
		speakerName := extractNestedString(item, []string{"speaker", "display_name"}, []string{"speaker", "name"})
		if strings.TrimSpace(speakerName) == "" {
			speakerName = fmt.Sprintf("Speaker %d", len(uniqueSpeakerNames(segments))+1)
		}
		text := strings.TrimSpace(extractString(item, "text"))
		if text == "" {
			text = strings.TrimSpace(extractString(item, "markdown"))
		}
		if text == "" {
			continue
		}

		segments = append(segments, models.FathomSegmentInput{
			SpeakerName: speakerName,
			StartMS:     startMS,
			Text:        text,
		})
	}

	if len(segments) == 0 {
		return nil, fmt.Errorf("no transcript segments found")
	}

	for index := range segments {
		nextStart := segments[index].StartMS + 5000
		if index+1 < len(segments) && segments[index+1].StartMS > segments[index].StartMS {
			nextStart = segments[index+1].StartMS
		}
		if recordingEnd != nil && index == len(segments)-1 {
			nextStart = max(nextStart, segments[index].StartMS+5000)
		}
		if nextStart <= segments[index].StartMS {
			nextStart = segments[index].StartMS + 5000
		}
		segments[index].EndMS = nextStart
	}
	return segments, nil
}

func buildDraftPreviewJSON(source string, segmentCount, speakerCount int) string {
	payload, _ := json.Marshal(map[string]any{
		"source":           source,
		"speaker_count":    speakerCount,
		"segment_count":    segmentCount,
		"diarization_mode": "fathom",
	})
	return string(payload)
}

func extractMeetingItems(payload map[string]any) []map[string]any {
	return arrayOfMaps(firstValue(payload, "data", "meetings"))
}

func extractTranscriptItems(payload map[string]any) []map[string]any {
	if items := arrayOfMaps(firstValue(payload, "data", "transcript")); len(items) > 0 {
		return items
	}
	if nested, ok := payload["recording"].(map[string]any); ok {
		if items := arrayOfMaps(firstValue(nested, "transcript", "data")); len(items) > 0 {
			return items
		}
	}
	return nil
}

func extractActionItems(payload map[string]any) []any {
	switch raw := firstValue(payload, "action_items", "actionItems").(type) {
	case []any:
		return raw
	default:
		return nil
	}
}

func extractSummaryMarkdown(payload map[string]any) string {
	if nested, ok := payload["default_summary"].(map[string]any); ok {
		if value := firstNonEmpty(extractString(nested, "markdown"), extractString(nested, "content")); value != "" {
			return value
		}
	}
	if nested, ok := payload["summary"].(map[string]any); ok {
		if value := firstNonEmpty(extractString(nested, "markdown"), extractString(nested, "content")); value != "" {
			return value
		}
	}
	return firstNonEmpty(
		extractString(payload, "markdown"),
		extractString(payload, "content"),
		extractString(payload, "summary_markdown"),
	)
}

func extractRecordingID(payload map[string]any) string {
	return firstNonEmpty(extractString(payload, "recording_id"), extractString(payload, "id"))
}

func extractNextCursor(payload map[string]any) string {
	return firstNonEmpty(extractString(payload, "next_cursor"), extractNestedString(payload, []string{"page_info", "next_cursor"}))
}

func extractString(payload map[string]any, key string) string {
	value, ok := payload[key]
	if !ok || value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case float64:
		return strconv.FormatInt(int64(typed), 10)
	case json.Number:
		return typed.String()
	default:
		return strings.TrimSpace(fmt.Sprint(typed))
	}
}

func extractNestedString(payload map[string]any, paths ...[]string) string {
	for _, path := range paths {
		current := any(payload)
		for _, part := range path {
			nested, ok := current.(map[string]any)
			if !ok {
				current = nil
				break
			}
			current = nested[part]
		}
		if current == nil {
			continue
		}
		if value, ok := current.(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func extractTime(payload map[string]any, key string) *time.Time {
	value := extractString(payload, key)
	if value == "" {
		return nil
	}
	formats := []string{time.RFC3339, "2006-01-02T15:04:05.000Z07:00", "2006-01-02T15:04:05Z07:00"}
	for _, layout := range formats {
		parsed, err := time.Parse(layout, value)
		if err == nil {
			return &parsed
		}
	}
	return nil
}

func encodeActionItems(items []any) (string, bool) {
	if len(items) == 0 {
		return "", false
	}
	encoded, err := json.Marshal(items)
	if err != nil {
		return "", false
	}
	return string(encoded), true
}

func parseTimestampMS(value string) (int, error) {
	parts := strings.Split(strings.TrimSpace(value), ":")
	if len(parts) != 3 {
		return 0, fmt.Errorf("unsupported timestamp format %q", value)
	}
	hours, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, err
	}
	minutes, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, err
	}
	seconds, err := strconv.Atoi(parts[2])
	if err != nil {
		return 0, err
	}
	return ((hours*60+minutes)*60 + seconds) * 1000, nil
}

func decodeSecret(secret string) ([]byte, error) {
	trimmed := strings.TrimPrefix(secret, "whsec_")
	if decoded, err := base64.StdEncoding.DecodeString(trimmed); err == nil {
		return decoded, nil
	}
	return base64.RawStdEncoding.DecodeString(trimmed)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func firstStringPtr(existing *string, fallback string) *string {
	if existing != nil && strings.TrimSpace(*existing) != "" {
		return existing
	}
	return ptrFromString(fallback)
}

func firstTimePtr(values ...*time.Time) *time.Time {
	for _, value := range values {
		if value != nil && !value.IsZero() {
			return value
		}
	}
	return nil
}

func ptrFromString(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func stringPtrValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func arrayOfMaps(value any) []map[string]any {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	output := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if typed, ok := item.(map[string]any); ok {
			output = append(output, typed)
		}
	}
	return output
}

func firstValue(payload map[string]any, keys ...string) any {
	for _, key := range keys {
		if value, ok := payload[key]; ok {
			return value
		}
	}
	return nil
}

func uniqueSpeakerNames(segments []models.FathomSegmentInput) []string {
	seen := map[string]struct{}{}
	output := make([]string, 0, len(segments))
	for _, segment := range segments {
		name := strings.TrimSpace(segment.SpeakerName)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		output = append(output, name)
	}
	return output
}

func JSONBytes(payload map[string]any) []byte {
	if len(payload) == 0 {
		return nil
	}
	encoded, _ := json.Marshal(payload)
	return bytes.TrimSpace(encoded)
}
