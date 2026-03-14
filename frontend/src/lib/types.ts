export type MeetingStatus =
	| 'uploaded'
	| 'queued'
	| 'processing'
	| 'review_required'
	| 'report_generating'
	| 'completed'
	| 'failed';

export type MeetingSource = 'upload' | 'fathom';

export interface Meeting {
	id: string;
	source: MeetingSource;
	title: string;
	original_filename: string;
	content_type: string;
	size_bytes: number;
	storage_path: string;
	status: MeetingStatus;
	duration_seconds?: number;
	error_message?: string;
	draft_preview_json?: string;
	report_json?: string;
	report_markdown?: string;
	fathom_recording_id?: string;
	fathom_url?: string;
	fathom_share_url?: string;
	fathom_recorded_by_name?: string;
	fathom_transcript_language?: string;
	fathom_summary_markdown?: string;
	fathom_action_items_json?: string;
	external_created_at?: string;
	created_at: string;
	updated_at: string;
	submitted_at?: string;
	processed_at?: string;
	finalized_at?: string;
}

export interface SpeakerSlot {
	id: string;
	label: string;
	assigned_name?: string;
}

export interface SourceSegment {
	id: string;
	speaker_slot_id: string;
	start_ms: number;
	end_ms: number;
	text: string;
	confidence: number;
	overlap_flag: boolean;
	unclear_flag: boolean;
	edited_text?: string;
	reviewed_speaker_slot_id?: string;
	unclear_override: boolean;
}

export interface ReviewVersion {
	id: string;
	version: number;
	review_session_id: string;
	created_at: string;
}

export interface DraftPayload {
	meeting: Meeting;
	speaker_slots: SpeakerSlot[];
	segments: SourceSegment[];
	latest_review_version?: ReviewVersion;
	draft_preview_json?: string;
	lock_acquired: boolean;
	lock_blocked: boolean;
}

export interface ReportPayload {
	meeting: Meeting;
	structured_json: string;
	markdown: string;
}
