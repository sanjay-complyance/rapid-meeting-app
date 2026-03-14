# RAPID Assistant

English-only v1 RAPID meeting assistant built as a monorepo:

- `frontend`: SvelteKit SSR app, managed with Bun
- `backend`: Go API for meetings, files, review state, and report retrieval
- `worker`: Python job worker for audio processing and RAPID synthesis

## Current implementation status

This repo is scaffolded for the full upload -> process -> review -> finalize flow.

- Uploads are stored on the local filesystem under `./data`
- PostgreSQL metadata is stored in Neon-compatible SQL
- Worker transcription uses `faster-whisper`
- Worker diarization uses `pyannote.audio` when `PYANNOTE_AUTH_TOKEN` is set
- The UI is intentionally minimal and optimized for wiring the flow first

## Planning docs

- [Fathom-first V2 plan](/Users/sanjaykumarv/Documents/RAPID assistant/docs/fathom-v2-plan.md)
- [Local Cloudflare Tunnel setup](/Users/sanjaykumarv/Documents/RAPID assistant/docs/local-cloudflare-tunnel.md)
- [Railway deployment guide](/Users/sanjaykumarv/Documents/RAPID assistant/docs/railway-deploy.md)

## Dev setup

1. Copy `.env.example` to `.env`
2. Put your Neon connection string in the root `.env` file:

```bash
DATABASE_URL="postgresql://USER:PASSWORD@YOUR-NEON-HOST.neon.tech/rapid_assistant?sslmode=require"
```

3. Install dependencies:

```bash
cd backend && go mod tidy
cd ../worker && uv sync --extra ai
cd ../frontend && bun install
```

4. Create the database schema from:

- [`backend/migrations/001_init.sql`](/Users/sanjaykumarv/Documents/RAPID assistant/backend/migrations/001_init.sql)
- [`backend/migrations/002_fathom.sql`](/Users/sanjaykumarv/Documents/RAPID assistant/backend/migrations/002_fathom.sql)

```bash
psql "$DATABASE_URL" -f backend/migrations/001_init.sql
psql "$DATABASE_URL" -f backend/migrations/002_fathom.sql
```

5. Start the full stack:

```bash
./scripts/dev.sh
```

6. If you want Fathom webhooks without paying for hosting, expose the backend with
   a free Cloudflare Quick Tunnel:

```bash
./scripts/tunnel.sh
```

Then use:

```text
https://<public-url>/v1/integrations/fathom/webhook
```

See [Local Cloudflare Tunnel setup](/Users/sanjaykumarv/Documents/RAPID assistant/docs/local-cloudflare-tunnel.md)
for the full no-cost flow.

## Important env vars

- `DATABASE_URL`: add your Neon URL here in the root `.env`
- `ENABLE_AUDIO_UPLOADS`: set this to `false` if you want a Fathom-only flow, or in Railway unless you also add shared object storage
- `PUBLIC_ENABLE_AUDIO_UPLOADS`: hide the upload UI when the backend upload path is disabled
- `PYANNOTE_AUTH_TOKEN`: optional, enables true multi-speaker diarization when the worker runs on Python 3.11+
- `GEMINI_API_KEY`: optional, enables Gemini-based RAPID report generation
- `FATHOM_API_KEY`: enables manual Fathom import and webhook hydration
- `FATHOM_WEBHOOK_SECRET`: enables verified Fathom webhooks
- `RAPID_WHISPER_MODEL`: defaults to `tiny.en` for faster local Mac development; move up to `base.en` or `small.en` later on GPU
- `RAPID_WHISPER_DEVICE`: `auto`, `cpu`, or later `cuda`
- `RAPID_WHISPER_COMPUTE_TYPE`: defaults to `int8`, which is a reasonable Mac-friendly default
- `JOB_STALE_AFTER_SECONDS`: defaults to `600`; stale `running` jobs older than this are re-queued when the worker polls

If `PYANNOTE_AUTH_TOKEN` is not set, or if the worker is still running on Python 3.10, the worker still runs and labels the meeting as single-speaker instead of fabricating diarization.

Recommended low-cost path: run the stack locally and use
[Local Cloudflare Tunnel setup](/Users/sanjaykumarv/Documents/RAPID assistant/docs/local-cloudflare-tunnel.md)
for Fathom webhooks.

If you do come back to hosted deployment later, use
[docs/railway-deploy.md](/Users/sanjaykumarv/Documents/RAPID assistant/docs/railway-deploy.md).
The Railway path is intended for `Fathom-first` use; direct uploads should stay
disabled there until shared object storage is added.
