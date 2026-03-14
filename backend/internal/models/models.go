package models

import "time"

type MeetingStatus string
type MeetingSource string

const (
	StatusUploaded         MeetingStatus = "uploaded"
	StatusQueued           MeetingStatus = "queued"
	StatusProcessing       MeetingStatus = "processing"
	StatusReviewRequired   MeetingStatus = "review_required"
	StatusReportGenerating MeetingStatus = "report_generating"
	StatusCompleted        MeetingStatus = "completed"
	StatusFailed           MeetingStatus = "failed"

	SourceUpload MeetingSource = "upload"
	SourceFathom MeetingSource = "fathom"
)

type Meeting struct {
	ID                string        `json:"id"`
	Source            MeetingSource `json:"source"`
	Title             string        `json:"title"`
	OriginalFilename  string        `json:"original_filename"`
	ContentType       string        `json:"content_type"`
	SizeBytes         int64         `json:"size_bytes"`
	StoragePath       string        `json:"storage_path"`
	NormalizedPath    *string       `json:"normalized_path,omitempty"`
	Status            MeetingStatus `json:"status"`
	DurationSeconds   *float64      `json:"duration_seconds,omitempty"`
	ErrorMessage      *string       `json:"error_message,omitempty"`
	DraftPreviewJSON  *string       `json:"draft_preview_json,omitempty"`
	ReportJSON        *string       `json:"report_json,omitempty"`
	ReportMarkdown    *string       `json:"report_markdown,omitempty"`
	ReviewLockID      *string       `json:"review_lock_id,omitempty"`
	ReviewLockExpires *time.Time    `json:"review_lock_expires_at,omitempty"`
	FathomRecordingID *string       `json:"fathom_recording_id,omitempty"`
	FathomURL         *string       `json:"fathom_url,omitempty"`
	FathomShareURL    *string       `json:"fathom_share_url,omitempty"`
	FathomRecordedBy  *string       `json:"fathom_recorded_by_name,omitempty"`
	FathomLanguage    *string       `json:"fathom_transcript_language,omitempty"`
	FathomSummary     *string       `json:"fathom_summary_markdown,omitempty"`
	FathomActionItems *string       `json:"fathom_action_items_json,omitempty"`
	ExternalCreatedAt *time.Time    `json:"external_created_at,omitempty"`
	CreatedAt         time.Time     `json:"created_at"`
	UpdatedAt         time.Time     `json:"updated_at"`
	SubmittedAt       *time.Time    `json:"submitted_at,omitempty"`
	ProcessedAt       *time.Time    `json:"processed_at,omitempty"`
	FinalizedAt       *time.Time    `json:"finalized_at,omitempty"`
}

type SpeakerSlot struct {
	ID           string  `json:"id"`
	Label        string  `json:"label"`
	AssignedName *string `json:"assigned_name,omitempty"`
}

type SourceSegment struct {
	ID              string  `json:"id"`
	SpeakerSlotID   string  `json:"speaker_slot_id"`
	StartMS         int     `json:"start_ms"`
	EndMS           int     `json:"end_ms"`
	Text            string  `json:"text"`
	Confidence      float64 `json:"confidence"`
	OverlapFlag     bool    `json:"overlap_flag"`
	UnclearFlag     bool    `json:"unclear_flag"`
	EditedText      *string `json:"edited_text,omitempty"`
	ReviewedSpeaker *string `json:"reviewed_speaker_slot_id,omitempty"`
	UnclearOverride bool    `json:"unclear_override"`
}

type ReviewVersion struct {
	ID              string    `json:"id"`
	Version         int       `json:"version"`
	ReviewSessionID string    `json:"review_session_id"`
	CreatedAt       time.Time `json:"created_at"`
}

type DraftPayload struct {
	Meeting             Meeting         `json:"meeting"`
	SpeakerSlots        []SpeakerSlot   `json:"speaker_slots"`
	Segments            []SourceSegment `json:"segments"`
	LatestReviewVersion *ReviewVersion  `json:"latest_review_version,omitempty"`
	DraftPreviewJSON    *string         `json:"draft_preview_json,omitempty"`
	LockAcquired        bool            `json:"lock_acquired"`
	LockBlocked         bool            `json:"lock_blocked"`
}

type ReportPayload struct {
	Meeting        Meeting `json:"meeting"`
	StructuredJSON string  `json:"structured_json"`
	Markdown       string  `json:"markdown"`
}

type CreateMeetingResponse struct {
	MeetingID string `json:"meeting_id"`
	SubmitURL string `json:"submit_url"`
	ReviewURL string `json:"review_url"`
	StatusURL string `json:"status_url"`
	ReportURL string `json:"report_url"`
}

type ImportFathomRequest struct {
	RecordingID string `json:"recording_id"`
	ShareURL    string `json:"share_url"`
}

type FathomSegmentInput struct {
	SpeakerName string
	StartMS     int
	EndMS       int
	Text        string
}

type FathomImportInput struct {
	ProviderEventID     string
	ProviderPayloadJSON string
	RecordingID         string
	Title               string
	MeetingTitle        *string
	URL                 *string
	ShareURL            *string
	RecordedByName      *string
	TranscriptLanguage  *string
	SummaryMarkdown     *string
	ActionItemsJSON     *string
	ExternalCreatedAt   *time.Time
	DraftPreviewJSON    *string
	Segments            []FathomSegmentInput
}

type ReviewSpeakerInput struct {
	SpeakerSlotID string  `json:"speaker_slot_id"`
	AssignedName  *string `json:"assigned_name"`
}

type ReviewSegmentInput struct {
	SourceSegmentID       string  `json:"source_segment_id"`
	EditedText            *string `json:"edited_text"`
	AssignedSpeakerSlotID string  `json:"assigned_speaker_slot_id"`
	UnclearOverride       bool    `json:"unclear_override"`
}

type SaveReviewRequest struct {
	Speakers []ReviewSpeakerInput `json:"speakers"`
	Segments []ReviewSegmentInput `json:"segments"`
}

type SaveReviewResponse struct {
	MeetingID     string        `json:"meeting_id"`
	ReviewVersion ReviewVersion `json:"review_version"`
}

type FinalizeResponse struct {
	MeetingID string        `json:"meeting_id"`
	Status    MeetingStatus `json:"status"`
}
