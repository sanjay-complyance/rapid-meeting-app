CREATE TABLE IF NOT EXISTS meetings (
    id UUID PRIMARY KEY,
    title TEXT NOT NULL,
    original_filename TEXT NOT NULL,
    content_type TEXT NOT NULL,
    size_bytes BIGINT NOT NULL,
    storage_path TEXT NOT NULL,
    normalized_path TEXT,
    status TEXT NOT NULL,
    duration_seconds DOUBLE PRECISION,
    error_message TEXT,
    draft_preview_json TEXT,
    report_json TEXT,
    report_markdown TEXT,
    review_lock_id TEXT,
    review_lock_expires_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    submitted_at TIMESTAMPTZ,
    processed_at TIMESTAMPTZ,
    finalized_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS jobs (
    id BIGSERIAL PRIMARY KEY,
    meeting_id UUID NOT NULL REFERENCES meetings(id) ON DELETE CASCADE,
    type TEXT NOT NULL,
    status TEXT NOT NULL,
    attempts INTEGER NOT NULL DEFAULT 0,
    available_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    claimed_by TEXT,
    claimed_at TIMESTAMPTZ,
    last_error TEXT,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS jobs_claimable_idx
    ON jobs (status, available_at, type, meeting_id);

CREATE TABLE IF NOT EXISTS speaker_slots (
    id UUID PRIMARY KEY,
    meeting_id UUID NOT NULL REFERENCES meetings(id) ON DELETE CASCADE,
    label TEXT NOT NULL,
    assigned_name TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS speaker_slots_meeting_label_idx
    ON speaker_slots (meeting_id, label);

CREATE TABLE IF NOT EXISTS source_segments (
    id UUID PRIMARY KEY,
    meeting_id UUID NOT NULL REFERENCES meetings(id) ON DELETE CASCADE,
    speaker_slot_id UUID NOT NULL REFERENCES speaker_slots(id) ON DELETE CASCADE,
    start_ms INTEGER NOT NULL,
    end_ms INTEGER NOT NULL,
    text TEXT NOT NULL,
    confidence DOUBLE PRECISION NOT NULL,
    overlap_flag BOOLEAN NOT NULL DEFAULT FALSE,
    unclear_flag BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS source_segments_meeting_idx
    ON source_segments (meeting_id, start_ms);

CREATE TABLE IF NOT EXISTS review_versions (
    id UUID PRIMARY KEY,
    meeting_id UUID NOT NULL REFERENCES meetings(id) ON DELETE CASCADE,
    version INTEGER NOT NULL,
    review_session_id TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS review_versions_meeting_version_idx
    ON review_versions (meeting_id, version);

CREATE TABLE IF NOT EXISTS reviewed_speaker_slots (
    review_version_id UUID NOT NULL REFERENCES review_versions(id) ON DELETE CASCADE,
    speaker_slot_id UUID NOT NULL REFERENCES speaker_slots(id) ON DELETE CASCADE,
    assigned_name TEXT,
    PRIMARY KEY (review_version_id, speaker_slot_id)
);

CREATE TABLE IF NOT EXISTS reviewed_segments (
    review_version_id UUID NOT NULL REFERENCES review_versions(id) ON DELETE CASCADE,
    source_segment_id UUID NOT NULL REFERENCES source_segments(id) ON DELETE CASCADE,
    edited_text TEXT,
    assigned_speaker_slot_id UUID NOT NULL REFERENCES speaker_slots(id) ON DELETE CASCADE,
    unclear_override BOOLEAN NOT NULL DEFAULT FALSE,
    PRIMARY KEY (review_version_id, source_segment_id)
);

