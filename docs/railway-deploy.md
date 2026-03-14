# Railway Deployment

This monorepo is set up to run on Railway as three separate services:

- `rapid-api` from `/backend`
- `rapid-worker` from `/worker`
- `rapid-frontend` from `/frontend`

Each service now includes its own `railway.toml` and `Dockerfile` in that service
directory.

## Important Deployment Caveat

This deployment is intended for the current `Fathom-first` production path.

Direct audio uploads rely on shared file storage between the Go API and the Python
worker. Railway services do not share the local filesystem, so production Railway
deployments should disable direct uploads until we add S3-compatible object storage.

Set these values in Railway:

- backend: `ENABLE_AUDIO_UPLOADS=false`
- frontend: `PUBLIC_ENABLE_AUDIO_UPLOADS=false`

## Services

### 1. API service

- Name: `rapid-api`
- Root directory: `/backend`
- Builder: `Dockerfile`
- Public domain: enabled

Required env vars:

- `DATABASE_URL`
- `PORT`
- `FRONTEND_ORIGIN`
- `ENABLE_AUDIO_UPLOADS=false`
- `FATHOM_API_KEY`
- `FATHOM_WEBHOOK_SECRET`
- `FATHOM_WEBHOOK_TOLERANCE_SECONDS=300`
- `STORAGE_ROOT=/app/data`

Recommended values:

- `FRONTEND_ORIGIN=https://${{rapid-frontend.RAILWAY_PUBLIC_DOMAIN}}`

Health check:

- path: `/healthz`

### 2. Worker service

- Name: `rapid-worker`
- Root directory: `/worker`
- Builder: `Dockerfile`
- Public domain: disabled

Required env vars:

- `DATABASE_URL`
- `GEMINI_API_KEY`
- `GEMINI_MODEL`
- `JOB_POLL_INTERVAL_SECONDS=5`
- `JOB_STALE_AFTER_SECONDS=600`
- `STORAGE_ROOT=/app/data`

Optional env vars:

- `PYANNOTE_AUTH_TOKEN`
- `RAPID_WHISPER_MODEL`
- `RAPID_WHISPER_DEVICE`
- `RAPID_WHISPER_COMPUTE_TYPE`

For the current Fathom-first deployment, the worker still runs because it generates
the final RAPID report jobs from reviewed transcript data stored in Postgres.

### 3. Frontend service

- Name: `rapid-frontend`
- Root directory: `/frontend`
- Builder: `Dockerfile`
- Public domain: enabled

Required env vars:

- `PUBLIC_API_BASE_URL`
- `PUBLIC_ENABLE_AUDIO_UPLOADS=false`

Recommended values:

- `PUBLIC_API_BASE_URL=https://${{rapid-api.RAILWAY_PUBLIC_DOMAIN}}`

Optional env vars:

- `ORIGIN=https://${{rapid-frontend.RAILWAY_PUBLIC_DOMAIN}}`

## Database

Use the existing Neon Postgres instance:

- `DATABASE_URL=<your neon connection string>`

Apply these migrations before the first deploy:

```bash
psql "$DATABASE_URL" -f backend/migrations/001_init.sql
psql "$DATABASE_URL" -f backend/migrations/002_fathom.sql
```

## Fathom webhook setup

After `rapid-api` has a public Railway domain, use this URL in Fathom:

```text
https://${{rapid-api.RAILWAY_PUBLIC_DOMAIN}}/v1/integrations/fathom/webhook
```

Enable transcript, summary, and action items in the webhook configuration.

## Deploy order

1. Deploy `rapid-api`
2. Deploy `rapid-worker`
3. Deploy `rapid-frontend`
4. Verify frontend can reach the API
5. Register the Fathom webhook against the API public domain
6. Test manual Fathom import with a real `recording_id`
7. Test webhook delivery from Fathom

## First live verification

Once the services are up:

1. Open the frontend
2. Import a Fathom meeting by `recording_id`
3. Confirm the meeting lands in `review_required`
4. Review and finalize
5. Confirm the RAPID document is generated

## Next infrastructure step

If you want audio uploads to work in Railway too, add shared object storage and move
the API/worker file handling off the local filesystem.
