package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"rapidassistant/backend/internal/models"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrNotFound     = errors.New("record not found")
	ErrReviewLocked = errors.New("meeting is locked by another review session")
)

const meetingColumns = `
	id,
	source,
	title,
	original_filename,
	content_type,
	size_bytes,
	storage_path,
	normalized_path,
	status,
	duration_seconds,
	error_message,
	draft_preview_json,
	report_json,
	report_markdown,
	review_lock_id,
	review_lock_expires_at,
	fathom_recording_id,
	fathom_url,
	fathom_share_url,
	fathom_recorded_by_name,
	fathom_transcript_language,
	fathom_summary_markdown,
	fathom_action_items_json,
	external_created_at,
	created_at,
	updated_at,
	submitted_at,
	processed_at,
	finalized_at
`

type PostgresStore struct {
	pool *pgxpool.Pool
}

func NewPostgresStore(ctx context.Context, databaseURL string) (*PostgresStore, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("connect postgres: %w", err)
	}
	return &PostgresStore{pool: pool}, nil
}

func (s *PostgresStore) Close() {
	s.pool.Close()
}

func (s *PostgresStore) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
}

func (s *PostgresStore) CreateMeeting(ctx context.Context, meetingID, title, originalFilename, contentType string, sizeBytes int64, storagePath string) (models.Meeting, error) {
	if meetingID == "" {
		meetingID = uuid.NewString()
	}

	query := `
		INSERT INTO meetings (
			id, source, title, original_filename, content_type, size_bytes, storage_path, status
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING ` + meetingColumns

	return scanMeeting(s.pool.QueryRow(ctx, query,
		meetingID,
		models.SourceUpload,
		title,
		originalFilename,
		contentType,
		sizeBytes,
		storagePath,
		models.StatusUploaded,
	))
}

func (s *PostgresStore) QueueMeetingProcessing(ctx context.Context, meetingID string) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	result, err := tx.Exec(ctx, `
		UPDATE meetings
		SET status = $2, submitted_at = NOW(), updated_at = NOW(), error_message = NULL
		WHERE id = $1`, meetingID, models.StatusQueued)
	if err != nil {
		return fmt.Errorf("update meeting status: %w", err)
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO jobs (meeting_id, type, status)
		VALUES ($1, 'process_meeting', 'queued')`, meetingID); err != nil {
		return fmt.Errorf("insert process job: %w", err)
	}

	return tx.Commit(ctx)
}

func (s *PostgresStore) ImportFathomMeeting(ctx context.Context, input models.FathomImportInput) (models.Meeting, bool, error) {
	if strings.TrimSpace(input.RecordingID) == "" {
		return models.Meeting{}, false, fmt.Errorf("recording_id is required")
	}
	if len(input.Segments) == 0 {
		return models.Meeting{}, false, fmt.Errorf("no Fathom transcript segments provided")
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return models.Meeting{}, false, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	if input.ProviderEventID != "" {
		result, err := tx.Exec(ctx, `
			INSERT INTO integration_events (provider, provider_event_id, payload_json)
			VALUES ('fathom', $1, COALESCE($2::jsonb, '{}'::jsonb))
			ON CONFLICT (provider, provider_event_id) DO NOTHING`,
			input.ProviderEventID,
			nilIfEmptyJSON(input.ProviderPayloadJSON),
		)
		if err != nil {
			return models.Meeting{}, false, fmt.Errorf("insert integration event: %w", err)
		}
		if result.RowsAffected() == 0 {
			var existingMeetingID *string
			if err := tx.QueryRow(ctx, `
				SELECT meeting_id
				FROM integration_events
				WHERE provider = 'fathom' AND provider_event_id = $1`,
				input.ProviderEventID,
			).Scan(&existingMeetingID); err != nil {
				return models.Meeting{}, false, fmt.Errorf("load existing integration event: %w", err)
			}
			if existingMeetingID == nil {
				return models.Meeting{}, true, ErrNotFound
			}
			meeting, err := scanMeeting(tx.QueryRow(ctx, `SELECT `+meetingColumns+` FROM meetings WHERE id = $1`, *existingMeetingID))
			return meeting, true, err
		}
	}

	meetingID, isNew, err := s.upsertFathomMeetingRecord(ctx, tx, input)
	if err != nil {
		return models.Meeting{}, false, err
	}

	if !isNew {
		if err := s.clearMeetingTranscript(ctx, tx, meetingID); err != nil {
			return models.Meeting{}, false, err
		}
	}

	speakerSlotIDs := map[string]string{}
	speakerLabels := map[string]string{}
	speakerOrder := 0

	for index, segment := range input.Segments {
		speakerKey := normalizedSpeakerKey(segment.SpeakerName, index)
		speakerSlotID, ok := speakerSlotIDs[speakerKey]
		if !ok {
			speakerOrder++
			speakerSlotID = uuid.NewString()
			speakerLabel := fmt.Sprintf("Speaker %d", speakerOrder)
			speakerLabels[speakerKey] = speakerLabel
			speakerSlotIDs[speakerKey] = speakerSlotID

			if _, err := tx.Exec(ctx, `
				INSERT INTO speaker_slots (id, meeting_id, label, assigned_name)
				VALUES ($1, $2, $3, $4)`,
				speakerSlotID,
				meetingID,
				speakerLabel,
				nilIfBlank(segment.SpeakerName),
			); err != nil {
				return models.Meeting{}, false, fmt.Errorf("insert speaker slot: %w", err)
			}
		}

		if _, err := tx.Exec(ctx, `
			INSERT INTO source_segments (
				id, meeting_id, speaker_slot_id, start_ms, end_ms, text, confidence, overlap_flag, unclear_flag
			) VALUES ($1, $2, $3, $4, $5, $6, $7, FALSE, FALSE)`,
			uuid.NewString(),
			meetingID,
			speakerSlotID,
			segment.StartMS,
			segment.EndMS,
			segment.Text,
			1.0,
		); err != nil {
			return models.Meeting{}, false, fmt.Errorf("insert source segment: %w", err)
		}
	}

	if _, err := tx.Exec(ctx, `
		UPDATE meetings
		SET status = $2,
		    draft_preview_json = $3,
		    duration_seconds = $4,
		    error_message = NULL,
		    report_json = NULL,
		    report_markdown = NULL,
		    review_lock_id = NULL,
		    review_lock_expires_at = NULL,
		    submitted_at = COALESCE(submitted_at, NOW()),
		    processed_at = NOW(),
		    updated_at = NOW()
		WHERE id = $1`,
		meetingID,
		models.StatusReviewRequired,
		input.DraftPreviewJSON,
		calculateDurationSeconds(input.Segments),
	); err != nil {
		return models.Meeting{}, false, fmt.Errorf("finalize Fathom meeting state: %w", err)
	}

	if input.ProviderEventID != "" {
		if _, err := tx.Exec(ctx, `
			UPDATE integration_events
			SET meeting_id = $3,
			    payload_json = COALESCE($2::jsonb, payload_json),
			    processed_at = NOW(),
			    updated_at = NOW()
			WHERE provider = 'fathom' AND provider_event_id = $1`,
			input.ProviderEventID,
			nilIfEmptyJSON(input.ProviderPayloadJSON),
			meetingID,
		); err != nil {
			return models.Meeting{}, false, fmt.Errorf("update integration event: %w", err)
		}
	}

	meeting, err := scanMeeting(tx.QueryRow(ctx, `SELECT `+meetingColumns+` FROM meetings WHERE id = $1`, meetingID))
	if err != nil {
		return models.Meeting{}, false, err
	}

	if err := tx.Commit(ctx); err != nil {
		return models.Meeting{}, false, fmt.Errorf("commit Fathom import: %w", err)
	}
	return meeting, false, nil
}

func (s *PostgresStore) upsertFathomMeetingRecord(ctx context.Context, tx pgx.Tx, input models.FathomImportInput) (string, bool, error) {
	var existingID string
	err := tx.QueryRow(ctx, `
		SELECT id
		FROM meetings
		WHERE source = $1 AND fathom_recording_id = $2`,
		models.SourceFathom,
		input.RecordingID,
	).Scan(&existingID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", false, fmt.Errorf("find Fathom meeting: %w", err)
	}

	title := strings.TrimSpace(input.Title)
	if title == "" {
		title = "Fathom meeting " + input.RecordingID
	}

	if existingID == "" {
		existingID = uuid.NewString()
		if _, err := tx.Exec(ctx, `
			INSERT INTO meetings (
				id, source, title, original_filename, content_type, size_bytes, storage_path, status,
				fathom_recording_id, fathom_url, fathom_share_url, fathom_recorded_by_name,
				fathom_transcript_language, fathom_summary_markdown, fathom_action_items_json,
				external_created_at, submitted_at, processed_at
			) VALUES (
				$1, $2, $3, $4, $5, 0, '', $6,
				$7, $8, $9, $10,
				$11, $12, $13,
				$14, NOW(), NOW()
			)`,
			existingID,
			models.SourceFathom,
			title,
			"fathom:"+input.RecordingID,
			"application/fathom+json",
			models.StatusReviewRequired,
			input.RecordingID,
			input.URL,
			input.ShareURL,
			input.RecordedByName,
			input.TranscriptLanguage,
			input.SummaryMarkdown,
			input.ActionItemsJSON,
			input.ExternalCreatedAt,
		); err != nil {
			return "", false, fmt.Errorf("insert Fathom meeting: %w", err)
		}
		return existingID, true, nil
	}

	if _, err := tx.Exec(ctx, `
		UPDATE meetings
		SET title = $2,
		    original_filename = $3,
		    content_type = $4,
		    status = $5,
		    fathom_url = $6,
		    fathom_share_url = $7,
		    fathom_recorded_by_name = $8,
		    fathom_transcript_language = $9,
		    fathom_summary_markdown = $10,
		    fathom_action_items_json = $11,
		    external_created_at = $12,
		    error_message = NULL,
		    updated_at = NOW()
		WHERE id = $1`,
		existingID,
		title,
		"fathom:"+input.RecordingID,
		"application/fathom+json",
		models.StatusReviewRequired,
		input.URL,
		input.ShareURL,
		input.RecordedByName,
		input.TranscriptLanguage,
		input.SummaryMarkdown,
		input.ActionItemsJSON,
		input.ExternalCreatedAt,
	); err != nil {
		return "", false, fmt.Errorf("update Fathom meeting: %w", err)
	}
	return existingID, false, nil
}

func (s *PostgresStore) clearMeetingTranscript(ctx context.Context, tx pgx.Tx, meetingID string) error {
	if _, err := tx.Exec(ctx, `DELETE FROM reviewed_segments WHERE review_version_id IN (SELECT id FROM review_versions WHERE meeting_id = $1)`, meetingID); err != nil {
		return fmt.Errorf("delete reviewed segments: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM reviewed_speaker_slots WHERE review_version_id IN (SELECT id FROM review_versions WHERE meeting_id = $1)`, meetingID); err != nil {
		return fmt.Errorf("delete reviewed speaker slots: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM review_versions WHERE meeting_id = $1`, meetingID); err != nil {
		return fmt.Errorf("delete review versions: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM source_segments WHERE meeting_id = $1`, meetingID); err != nil {
		return fmt.Errorf("delete source segments: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM speaker_slots WHERE meeting_id = $1`, meetingID); err != nil {
		return fmt.Errorf("delete speaker slots: %w", err)
	}
	return nil
}

func (s *PostgresStore) GetMeeting(ctx context.Context, meetingID string) (models.Meeting, error) {
	return scanMeeting(s.pool.QueryRow(ctx, `SELECT `+meetingColumns+` FROM meetings WHERE id = $1`, meetingID))
}

func (s *PostgresStore) GetDraft(ctx context.Context, meetingID, sessionID string, ttl time.Duration) (models.DraftPayload, error) {
	lockAcquired := false
	lockBlocked := false

	if sessionID != "" {
		acquired, err := s.acquireReviewLock(ctx, meetingID, sessionID, ttl)
		if err != nil && !errors.Is(err, ErrReviewLocked) {
			return models.DraftPayload{}, err
		}
		lockAcquired = acquired
		lockBlocked = errors.Is(err, ErrReviewLocked)
	}

	meeting, err := s.GetMeeting(ctx, meetingID)
	if err != nil {
		return models.DraftPayload{}, err
	}

	speakers, err := s.getSpeakerSlots(ctx, meetingID)
	if err != nil {
		return models.DraftPayload{}, err
	}

	latestReview, err := s.getLatestReviewVersion(ctx, meetingID)
	if err != nil {
		return models.DraftPayload{}, err
	}

	segments, err := s.getDraftSegments(ctx, meetingID, latestReview)
	if err != nil {
		return models.DraftPayload{}, err
	}

	return models.DraftPayload{
		Meeting:             meeting,
		SpeakerSlots:        speakers,
		Segments:            segments,
		LatestReviewVersion: latestReview,
		DraftPreviewJSON:    meeting.DraftPreviewJSON,
		LockAcquired:        lockAcquired,
		LockBlocked:         lockBlocked,
	}, nil
}

func (s *PostgresStore) SaveReview(ctx context.Context, meetingID, sessionID string, speakers []models.ReviewSpeakerInput, segments []models.ReviewSegmentInput) (models.ReviewVersion, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return models.ReviewVersion{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	if err := ensureLock(ctx, tx, meetingID, sessionID); err != nil {
		return models.ReviewVersion{}, err
	}

	var nextVersion int
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(MAX(version), 0) + 1
		FROM review_versions
		WHERE meeting_id = $1`, meetingID).Scan(&nextVersion); err != nil {
		return models.ReviewVersion{}, fmt.Errorf("get next review version: %w", err)
	}

	reviewVersion := models.ReviewVersion{
		ID:              uuid.NewString(),
		Version:         nextVersion,
		ReviewSessionID: sessionID,
		CreatedAt:       time.Now().UTC(),
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO review_versions (id, meeting_id, version, review_session_id, created_at)
		VALUES ($1, $2, $3, $4, $5)`,
		reviewVersion.ID, meetingID, reviewVersion.Version, reviewVersion.ReviewSessionID, reviewVersion.CreatedAt); err != nil {
		return models.ReviewVersion{}, fmt.Errorf("insert review version: %w", err)
	}

	for _, speaker := range speakers {
		if _, err := tx.Exec(ctx, `
			UPDATE speaker_slots
			SET assigned_name = $3, updated_at = NOW()
			WHERE id = $1 AND meeting_id = $2`,
			speaker.SpeakerSlotID, meetingID, speaker.AssignedName); err != nil {
			return models.ReviewVersion{}, fmt.Errorf("update speaker slot: %w", err)
		}

		if _, err := tx.Exec(ctx, `
			INSERT INTO reviewed_speaker_slots (review_version_id, speaker_slot_id, assigned_name)
			VALUES ($1, $2, $3)`,
			reviewVersion.ID, speaker.SpeakerSlotID, speaker.AssignedName); err != nil {
			return models.ReviewVersion{}, fmt.Errorf("insert reviewed speaker slot: %w", err)
		}
	}

	for _, segment := range segments {
		if _, err := tx.Exec(ctx, `
			INSERT INTO reviewed_segments (
				review_version_id, source_segment_id, edited_text, assigned_speaker_slot_id, unclear_override
			) VALUES ($1, $2, $3, $4, $5)`,
			reviewVersion.ID,
			segment.SourceSegmentID,
			segment.EditedText,
			segment.AssignedSpeakerSlotID,
			segment.UnclearOverride,
		); err != nil {
			return models.ReviewVersion{}, fmt.Errorf("insert reviewed segment: %w", err)
		}
	}

	if _, err := tx.Exec(ctx, `
		UPDATE meetings
		SET updated_at = NOW()
		WHERE id = $1`, meetingID); err != nil {
		return models.ReviewVersion{}, fmt.Errorf("touch meeting: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return models.ReviewVersion{}, fmt.Errorf("commit review tx: %w", err)
	}

	return reviewVersion, nil
}

func (s *PostgresStore) EnqueueReportGeneration(ctx context.Context, meetingID, sessionID string) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	if err := ensureLock(ctx, tx, meetingID, sessionID); err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, `
		UPDATE meetings
		SET status = $2, updated_at = NOW()
		WHERE id = $1`,
		meetingID, models.StatusReportGenerating); err != nil {
		return fmt.Errorf("update meeting status: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO jobs (meeting_id, type, status)
		VALUES ($1, 'generate_report', 'queued')`, meetingID); err != nil {
		return fmt.Errorf("insert report job: %w", err)
	}

	return tx.Commit(ctx)
}

func (s *PostgresStore) GetReport(ctx context.Context, meetingID string) (models.ReportPayload, error) {
	meeting, err := s.GetMeeting(ctx, meetingID)
	if err != nil {
		return models.ReportPayload{}, err
	}

	if meeting.ReportJSON == nil || meeting.ReportMarkdown == nil {
		return models.ReportPayload{}, ErrNotFound
	}

	return models.ReportPayload{
		Meeting:        meeting,
		StructuredJSON: *meeting.ReportJSON,
		Markdown:       *meeting.ReportMarkdown,
	}, nil
}

func (s *PostgresStore) acquireReviewLock(ctx context.Context, meetingID, sessionID string, ttl time.Duration) (bool, error) {
	result, err := s.pool.Exec(ctx, `
		UPDATE meetings
		SET review_lock_id = $2,
		    review_lock_expires_at = NOW() + $3 * INTERVAL '1 second',
		    updated_at = NOW()
		WHERE id = $1
		  AND (review_lock_id IS NULL OR review_lock_id = $2 OR review_lock_expires_at < NOW())`,
		meetingID, sessionID, int(ttl.Seconds()))
	if err != nil {
		return false, fmt.Errorf("acquire review lock: %w", err)
	}
	if result.RowsAffected() == 0 {
		return false, ErrReviewLocked
	}
	return true, nil
}

func ensureLock(ctx context.Context, tx pgx.Tx, meetingID, sessionID string) error {
	if sessionID == "" {
		return ErrReviewLocked
	}

	var currentID *string
	var expiresAt *time.Time
	if err := tx.QueryRow(ctx, `
		SELECT review_lock_id, review_lock_expires_at
		FROM meetings
		WHERE id = $1`, meetingID).Scan(&currentID, &expiresAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("read review lock: %w", err)
	}

	if currentID == nil || *currentID != sessionID || expiresAt == nil || expiresAt.Before(time.Now().UTC()) {
		return ErrReviewLocked
	}
	return nil
}

func (s *PostgresStore) getSpeakerSlots(ctx context.Context, meetingID string) ([]models.SpeakerSlot, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, label, assigned_name
		FROM speaker_slots
		WHERE meeting_id = $1
		ORDER BY created_at, label`, meetingID)
	if err != nil {
		return nil, fmt.Errorf("query speaker slots: %w", err)
	}
	defer rows.Close()

	var speakers []models.SpeakerSlot
	for rows.Next() {
		var speaker models.SpeakerSlot
		if err := rows.Scan(&speaker.ID, &speaker.Label, &speaker.AssignedName); err != nil {
			return nil, fmt.Errorf("scan speaker slot: %w", err)
		}
		speakers = append(speakers, speaker)
	}
	return speakers, rows.Err()
}

func (s *PostgresStore) getLatestReviewVersion(ctx context.Context, meetingID string) (*models.ReviewVersion, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, version, review_session_id, created_at
		FROM review_versions
		WHERE meeting_id = $1
		ORDER BY version DESC
		LIMIT 1`, meetingID)

	var review models.ReviewVersion
	if err := row.Scan(&review.ID, &review.Version, &review.ReviewSessionID, &review.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get latest review version: %w", err)
	}
	return &review, nil
}

func (s *PostgresStore) getDraftSegments(ctx context.Context, meetingID string, latestReview *models.ReviewVersion) ([]models.SourceSegment, error) {
	query := `
		SELECT ss.id, ss.speaker_slot_id, ss.start_ms, ss.end_ms, ss.text, ss.confidence,
		       ss.overlap_flag, ss.unclear_flag, rs.edited_text, rs.assigned_speaker_slot_id,
		       COALESCE(rs.unclear_override, FALSE)
		FROM source_segments ss
		LEFT JOIN reviewed_segments rs
		  ON ss.id = rs.source_segment_id
		 AND ($2::uuid IS NOT NULL AND rs.review_version_id = $2::uuid)
		WHERE ss.meeting_id = $1
		ORDER BY ss.start_ms`

	var reviewID any
	if latestReview != nil {
		reviewID = latestReview.ID
	}

	rows, err := s.pool.Query(ctx, query, meetingID, reviewID)
	if err != nil {
		return nil, fmt.Errorf("query draft segments: %w", err)
	}
	defer rows.Close()

	var segments []models.SourceSegment
	for rows.Next() {
		var segment models.SourceSegment
		if err := rows.Scan(
			&segment.ID,
			&segment.SpeakerSlotID,
			&segment.StartMS,
			&segment.EndMS,
			&segment.Text,
			&segment.Confidence,
			&segment.OverlapFlag,
			&segment.UnclearFlag,
			&segment.EditedText,
			&segment.ReviewedSpeaker,
			&segment.UnclearOverride,
		); err != nil {
			return nil, fmt.Errorf("scan source segment: %w", err)
		}
		segments = append(segments, segment)
	}
	return segments, rows.Err()
}

func scanMeeting(row pgx.Row) (models.Meeting, error) {
	var meeting models.Meeting
	if err := row.Scan(
		&meeting.ID,
		&meeting.Source,
		&meeting.Title,
		&meeting.OriginalFilename,
		&meeting.ContentType,
		&meeting.SizeBytes,
		&meeting.StoragePath,
		&meeting.NormalizedPath,
		&meeting.Status,
		&meeting.DurationSeconds,
		&meeting.ErrorMessage,
		&meeting.DraftPreviewJSON,
		&meeting.ReportJSON,
		&meeting.ReportMarkdown,
		&meeting.ReviewLockID,
		&meeting.ReviewLockExpires,
		&meeting.FathomRecordingID,
		&meeting.FathomURL,
		&meeting.FathomShareURL,
		&meeting.FathomRecordedBy,
		&meeting.FathomLanguage,
		&meeting.FathomSummary,
		&meeting.FathomActionItems,
		&meeting.ExternalCreatedAt,
		&meeting.CreatedAt,
		&meeting.UpdatedAt,
		&meeting.SubmittedAt,
		&meeting.ProcessedAt,
		&meeting.FinalizedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return models.Meeting{}, ErrNotFound
		}
		return models.Meeting{}, fmt.Errorf("scan meeting: %w", err)
	}
	return meeting, nil
}

func normalizedSpeakerKey(name string, index int) string {
	name = strings.TrimSpace(strings.ToLower(name))
	if name == "" {
		return fmt.Sprintf("speaker-%d", index+1)
	}
	return name
}

func nilIfBlank(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func nilIfEmptyJSON(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func calculateDurationSeconds(segments []models.FathomSegmentInput) float64 {
	if len(segments) == 0 {
		return 0
	}
	return float64(segments[len(segments)-1].EndMS) / 1000
}
