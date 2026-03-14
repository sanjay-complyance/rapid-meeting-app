# Fathom-First V2 Plan

## Goal

Import meetings recorded in Fathom and generate a RAPID document from:

- Fathom transcript
- Fathom summary
- Fathom action items
- optional Fathom recording/share URLs as evidence

For Fathom meetings, the system should treat Fathom data as the primary source and use the existing audio pipeline only as a fallback path.

## Why This Changes The Architecture

Our current v1 pipeline is audio-first:

- upload audio
- transcribe with Whisper
- diarize with pyannote
- review transcript
- generate RAPID

For Fathom-recorded meetings, that is no longer the best path. Fathom already provides:

- speaker-attributed transcript lines
- display names for speakers
- timestamps
- summary markdown
- action items

So v2 should become transcript-first for `source = fathom`.

## Product Scope

### In scope

- ingest new meeting content from Fathom via webhook
- persist Fathom meeting metadata, transcript, summary, and action items
- reuse the existing review UI with prefilled speaker names from Fathom
- generate RAPID from the reviewed Fathom transcript and summary
- support manual backfill of meetings using Fathom API polling by `recording_id`

### Out of scope

- publishing a public OAuth integration
- multi-tenant app installation flow
- syncing edits back into Fathom
- replacing the current audio upload path

## External Integration Strategy

### Authentication mode

Use a single internal Fathom API key for v2.

Rationale:

- this is the fastest path for internal use
- API keys are sufficient for meetings recorded by the account owner or shared to the team
- OAuth adds app registration, callback handling, token lifecycle management, and install UX that we do not need yet

### Delivery mode

Use Fathom webhooks as the primary ingestion path.

Also support a manual sync job for recovery/backfill:

- list meetings
- fetch transcript by `recording_id`
- fetch summary by `recording_id`

## Fathom Data Model

Add a `source` field to meetings with values:

- `upload`
- `fathom`

Add Fathom-specific fields to the meeting model:

- `fathom_recording_id`
- `fathom_title`
- `fathom_meeting_title`
- `fathom_url`
- `fathom_share_url`
- `fathom_recorded_by_name`
- `fathom_recorded_by_email`
- `fathom_transcript_language`
- `fathom_summary_markdown`
- `fathom_action_items_json`
- `fathom_payload_json`
- `external_created_at`

Add an integration event table for webhook idempotency and auditing:

- `provider`
- `provider_event_id`
- `meeting_id`
- `payload_json`
- `processed_at`
- unique key on `(provider, provider_event_id)`

## Ingestion Flow

### Primary flow

1. Fathom sends `newMeeting` webhook to our backend.
2. Backend verifies the webhook signature using the webhook secret.
3. Backend upserts a meeting with `source = fathom`.
4. Backend stores transcript, summary, action items, and payload metadata.
5. Backend creates speaker slots from transcript `speaker.display_name`.
6. Backend creates source transcript segments from Fathom transcript lines.
7. Meeting enters `review_required`.
8. User reviews the transcript and speaker names.
9. Backend queues RAPID generation.
10. Worker generates the final RAPID report from reviewed transcript + summary.

### Recovery flow

If a webhook payload arrives without transcript or summary, or if we want to backfill:

1. Fetch transcript from Fathom `GET /recordings/{recording_id}/transcript`
2. Fetch summary from Fathom `GET /recordings/{recording_id}/summary`
3. Upsert the meeting again
4. Rebuild source segments and draft review state

## Backend Changes

### New routes

- `POST /v1/integrations/fathom/webhook`
- `POST /v1/integrations/fathom/import`

The manual import route should accept:

- `recording_id`

### New behavior

- for `source = fathom`, skip local transcription and diarization entirely
- create review payload directly from Fathom transcript lines
- prefill speaker names from Fathom `speaker.display_name`
- keep timestamps from Fathom transcript in source segments

### Webhook verification

Use the raw request body and Fathom’s webhook headers:

- `webhook-id`
- `webhook-timestamp`
- `webhook-signature`

Reject requests with invalid signatures or timestamps outside the allowed tolerance.

## Review UX Changes

For Fathom meetings:

- show speaker names prefilled from Fathom
- show transcript lines grouped by speaker where possible
- show summary metadata in the draft summary panel
- keep manual speaker renaming available because Fathom display names may still be generic or duplicated

## RAPID Generation Changes

Current RAPID generation uses the reviewed transcript only.

For `source = fathom`, generation input should include:

- reviewed transcript
- Fathom summary markdown
- Fathom action items

Generation prompt should be updated so:

- transcript is the primary evidence
- summary is supporting context, not the authority
- explicit action items are preserved when they exist

## Suggested Acceptance Criteria

- a Fathom webhook creates a meeting in `review_required` without using Whisper
- imported Fathom meetings show named speakers in the review UI
- manual backfill by `recording_id` succeeds for an existing Fathom meeting
- duplicate webhook deliveries do not create duplicate meetings
- RAPID report generation works for `source = fathom`
- transcript timestamps remain visible in the review UI

## Implementation Sequence

1. schema changes for `source = fathom` and integration event tracking
2. Fathom webhook endpoint with signature verification
3. Fathom payload mapping into meetings, speakers, and source segments
4. manual import endpoint by `recording_id`
5. review UI adjustments for Fathom meeting metadata
6. RAPID generation prompt update for transcript + summary + action items
7. end-to-end validation with a real Fathom-recorded meeting

## Required Credentials And Setup

For internal v2 we need:

- `FATHOM_API_KEY`
- `FATHOM_WEBHOOK_SECRET`

We also need:

- a reachable HTTPS webhook URL for local or deployed testing

## Setup Steps For Credentials

### Fathom API key

1. Open Fathom user settings.
2. Go to `API Access`.
3. Generate an API key.
4. Add it to our app as `FATHOM_API_KEY`.

### Fathom webhook secret

Option 1: create the webhook in Fathom settings.

1. Go to `API Access`.
2. Go to `Manage` and choose `Add Webhook`.
3. Enter our destination URL.
4. Enable:
   - transcript
   - summary
   - action items
5. Save the webhook.
6. Copy the returned webhook secret and add it as `FATHOM_WEBHOOK_SECRET`.

Option 2: create the webhook via API using the Fathom API key.

## Environment Variables To Add

- `FATHOM_API_KEY=...`
- `FATHOM_WEBHOOK_SECRET=whsec_...`
- `FATHOM_WEBHOOK_TOLERANCE_SECONDS=300`

## Risks

- Fathom API keys are user-scoped, so the key only sees meetings recorded by that user or shared to their team
- webhook delivery can be duplicated, so idempotency is required
- some meetings may still have imperfect speaker names and require review
- Fathom availability should not block the rest of the app; imports must fail cleanly
