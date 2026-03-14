from __future__ import annotations

import json
from dataclasses import dataclass
from typing import Any

import psycopg
from psycopg.rows import dict_row


@dataclass
class Job:
    id: int
    meeting_id: str
    type: str
    payload: dict[str, Any]


def connect(database_url: str) -> psycopg.Connection:
    connection = psycopg.connect(database_url, row_factory=dict_row)
    # Use explicit transaction blocks for writes; autocommit avoids implicit
    # outer transactions turning later `connection.transaction()` calls into
    # savepoints that never become visible to other sessions.
    connection.autocommit = True
    return connection


def claim_job(connection: psycopg.Connection, worker_name: str) -> Job | None:
    with connection.transaction():
        with connection.cursor() as cursor:
            cursor.execute(
                """
                WITH next_job AS (
                    SELECT id
                    FROM jobs
                    WHERE status = 'queued'
                      AND available_at <= NOW()
                    ORDER BY created_at
                    FOR UPDATE SKIP LOCKED
                    LIMIT 1
                )
                UPDATE jobs j
                SET status = 'running',
                    claimed_by = %s,
                    claimed_at = NOW(),
                    attempts = attempts + 1,
                    updated_at = NOW()
                FROM next_job
                WHERE j.id = next_job.id
                RETURNING j.id, j.meeting_id, j.type, j.payload
                """,
                (worker_name,),
            )
            row = cursor.fetchone()
            if row is None:
                return None
            payload = row["payload"] if isinstance(row["payload"], dict) else json.loads(row["payload"] or "{}")
            return Job(
                id=row["id"],
                meeting_id=str(row["meeting_id"]),
                type=row["type"],
                payload=payload,
            )


def requeue_stale_jobs(connection: psycopg.Connection, stale_after_seconds: int) -> int:
    with connection.transaction():
        with connection.cursor() as cursor:
            cursor.execute(
                """
                UPDATE jobs
                SET status = 'queued',
                    claimed_by = NULL,
                    claimed_at = NULL,
                    updated_at = NOW(),
                    last_error = COALESCE(last_error, 'Recovered stale running job')
                WHERE status = 'running'
                  AND claimed_at IS NOT NULL
                  AND claimed_at < NOW() - (%s * INTERVAL '1 second')
                """,
                (stale_after_seconds,),
            )
            return cursor.rowcount


def complete_job(connection: psycopg.Connection, job_id: int) -> None:
    with connection.transaction():
        with connection.cursor() as cursor:
            cursor.execute(
                """
                UPDATE jobs
                SET status = 'completed', updated_at = NOW()
                WHERE id = %s
                """,
                (job_id,),
            )


def fail_job(connection: psycopg.Connection, job_id: int, error_message: str) -> None:
    with connection.transaction():
        with connection.cursor() as cursor:
            cursor.execute(
                """
                UPDATE jobs
                SET status = 'failed',
                    last_error = %s,
                    updated_at = NOW()
                WHERE id = %s
                """,
                (error_message, job_id),
            )
