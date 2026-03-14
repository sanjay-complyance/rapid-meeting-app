package fathom

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"strconv"
	"testing"
	"time"
)

func TestParseWebhookPayloadBuildsFathomImportInput(t *testing.T) {
	body := []byte(`{
		"data": {
			"recording_id": 42,
			"title": "Roadmap review",
			"url": "https://fathom.video/watch/42",
			"share_url": "https://fathom.video/share/42",
			"recorded_by": { "name": "Sanjay" },
			"transcript_language": "en",
			"default_summary": { "markdown": "## Summary\n\nReviewed roadmap" },
			"action_items": [{ "description": "Send the updated roadmap" }],
			"transcript": [
				{
					"speaker": { "display_name": "Alice" },
					"timestamp": "00:00:05",
					"text": "We should move the launch by one week."
				},
				{
					"speaker": { "display_name": "Bob" },
					"timestamp": "00:00:10",
					"text": "Agreed, and I will update the plan."
				}
			]
		}
	}`)

	input, err := ParseWebhookPayload(body)
	if err != nil {
		t.Fatalf("ParseWebhookPayload returned error: %v", err)
	}

	if input.RecordingID != "42" {
		t.Fatalf("expected recording id 42, got %q", input.RecordingID)
	}
	if input.Title != "Roadmap review" {
		t.Fatalf("expected title to be preserved, got %q", input.Title)
	}
	if len(input.Segments) != 2 {
		t.Fatalf("expected 2 transcript segments, got %d", len(input.Segments))
	}
	if input.Segments[0].StartMS != 5000 || input.Segments[0].EndMS != 10000 {
		t.Fatalf("expected first segment timing to be 5000-10000, got %d-%d", input.Segments[0].StartMS, input.Segments[0].EndMS)
	}
	if input.SummaryMarkdown == nil || *input.SummaryMarkdown == "" {
		t.Fatalf("expected summary markdown to be populated")
	}
	if input.ActionItemsJSON == nil || *input.ActionItemsJSON == "" {
		t.Fatalf("expected action items json to be populated")
	}
}

func TestVerifyWebhookAcceptsValidSignature(t *testing.T) {
	webhookID := "msg_123"
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	body := []byte(`{"hello":"world"}`)
	secretPayload := "super-secret-key"
	secret := "whsec_" + base64.StdEncoding.EncodeToString([]byte(secretPayload))

	mac := hmac.New(sha256.New, []byte(secretPayload))
	mac.Write([]byte(webhookID + "." + timestamp + "." + string(body)))
	signature := "v1," + base64.StdEncoding.EncodeToString(mac.Sum(nil))

	if err := VerifyWebhook(secret, webhookID, timestamp, signature, body, 365*24*time.Hour); err != nil {
		t.Fatalf("VerifyWebhook returned error for valid signature: %v", err)
	}
}
