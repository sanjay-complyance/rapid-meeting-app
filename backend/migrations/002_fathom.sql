ALTER TABLE meetings
    ADD COLUMN IF NOT EXISTS source TEXT NOT NULL DEFAULT 'upload',
    ADD COLUMN IF NOT EXISTS fathom_recording_id TEXT,
    ADD COLUMN IF NOT EXISTS fathom_url TEXT,
    ADD COLUMN IF NOT EXISTS fathom_share_url TEXT,
    ADD COLUMN IF NOT EXISTS fathom_recorded_by_name TEXT,
    ADD COLUMN IF NOT EXISTS fathom_transcript_language TEXT,
    ADD COLUMN IF NOT EXISTS fathom_summary_markdown TEXT,
    ADD COLUMN IF NOT EXISTS fathom_action_items_json TEXT,
    ADD COLUMN IF NOT EXISTS external_created_at TIMESTAMPTZ;

CREATE UNIQUE INDEX IF NOT EXISTS meetings_fathom_recording_idx
    ON meetings (source, fathom_recording_id)
    WHERE fathom_recording_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS integration_events (
    id BIGSERIAL PRIMARY KEY,
    provider TEXT NOT NULL,
    provider_event_id TEXT NOT NULL,
    meeting_id UUID REFERENCES meetings(id) ON DELETE SET NULL,
    payload_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    processed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS integration_events_provider_event_idx
    ON integration_events (provider, provider_event_id);
