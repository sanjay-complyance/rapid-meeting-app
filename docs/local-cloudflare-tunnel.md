# Local Fathom Webhook Setup

This is the recommended no-cost path for this project:

- `Neon` for Postgres
- local `Go API` on your Mac
- local `Python worker` on your Mac
- local `SvelteKit` frontend on your Mac
- `Cloudflare Tunnel` for the public Fathom webhook URL

## Why this path

Railway is optional. For the current `Fathom-first` workflow, the only public
endpoint you actually need is the backend webhook:

```text
POST /v1/integrations/fathom/webhook
```

Fathom sends the meeting data to your backend. The worker then reads the job from
Postgres and generates the RAPID output locally.

## Prerequisites

1. Create and fill the root `.env` file from `.env.example`
2. Apply the database migrations to your Neon instance:

```bash
psql "$DATABASE_URL" -f backend/migrations/001_init.sql
psql "$DATABASE_URL" -f backend/migrations/002_fathom.sql
```

3. Install local dependencies:

```bash
cd backend && go mod tidy
cd ../worker && uv sync
cd ../frontend && bun install
```

4. Install `cloudflared` on your Mac:

```bash
brew install cloudflared
```

## Run the stack

Start the app stack from the repo root:

```bash
./scripts/dev.sh
```

That starts:

- backend at `http://localhost:8080`
- frontend at `http://localhost:5173`
- worker as a local background poller

## Open the public webhook URL

In a second terminal, from the repo root:

```bash
./scripts/tunnel.sh
```

`cloudflared` will print a temporary public HTTPS URL like:

```text
https://something.trycloudflare.com
```

Use this full Fathom webhook URL:

```text
https://something.trycloudflare.com/v1/integrations/fathom/webhook
```

## Fathom configuration

In Fathom:

1. Open webhook settings
2. Paste the Cloudflare public webhook URL
3. Enable transcript, summary, and action items if available
4. Save the webhook

Keep these values in your local `.env`:

```bash
FATHOM_API_KEY=...
FATHOM_WEBHOOK_SECRET=...
FATHOM_WEBHOOK_TOLERANCE_SECONDS=300
```

## Testing

You have two good test paths:

### 1. Webhook test

- record or re-send a Fathom meeting
- let the webhook create the meeting automatically
- open the local frontend at `http://localhost:5173`

### 2. Manual import test

- open `http://localhost:5173`
- paste either:
  - a Fathom `recording_id`, or
  - a Fathom `share` link

The import flow now supports both.

## Important caveats

- Cloudflare Quick Tunnels are good for dev/testing, not production
- the public tunnel URL changes when you restart it
- your Mac must stay online while Fathom sends webhook events
- the frontend can stay local; only the backend webhook needs public access

## Current recommended `.env` values for local Fathom work

```bash
PUBLIC_API_BASE_URL=http://localhost:8080
ENABLE_AUDIO_UPLOADS=true
PUBLIC_ENABLE_AUDIO_UPLOADS=true
```

If you want to focus only on the Fathom path, you can also hide local uploads:

```bash
ENABLE_AUDIO_UPLOADS=false
PUBLIC_ENABLE_AUDIO_UPLOADS=false
```
