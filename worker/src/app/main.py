from __future__ import annotations

import json
import logging
import time
import uuid

import psycopg

from app.config import load_settings
from app.db import Job, claim_job, complete_job, connect, fail_job, requeue_stale_jobs
from app.pipeline import AudioPipeline
from app.reporting import ReportGenerator, markdown_from_report
from app.storage import LocalStorage


logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(message)s")
LOGGER = logging.getLogger(__name__)


def main() -> None:
    settings = load_settings()
    storage = LocalStorage(settings.storage_root)
    connection = connect(settings.database_url)
    pipeline = AudioPipeline(
        storage_root=settings.storage_root,
        transcriber=settings.transcriber,
        diarizer=settings.diarizer,
        whisper_model=settings.whisper_model,
        whisper_device=settings.whisper_device,
        whisper_compute_type=settings.whisper_compute_type,
        pyannote_auth_token=settings.pyannote_auth_token,
    )
    reports = ReportGenerator(settings.gemini_api_key, settings.gemini_model)

    LOGGER.info("worker started as %s", settings.worker_name)

    while True:
        try:
            recovered = requeue_stale_jobs(connection, settings.job_stale_after_seconds)
            if recovered:
                LOGGER.warning("requeued %s stale running job(s)", recovered)

            job = claim_job(connection, settings.worker_name)
            if job is None:
                time.sleep(settings.poll_interval_seconds)
                continue
        except psycopg.OperationalError as exc:
            LOGGER.warning("database connection lost while polling; reconnecting: %s", exc)
            connection = reconnect(connection, settings.database_url)
            time.sleep(settings.poll_interval_seconds)
            continue

        try:
            if job.type == "process_meeting":
                process_meeting_job(settings.database_url, storage, pipeline, job, settings.max_meeting_minutes)
            elif job.type == "generate_report":
                generate_report_job(settings.database_url, reports, job)
            else:
                raise RuntimeError(f"unsupported job type: {job.type}")
            with temporary_connection(settings.database_url) as db:
                complete_job(db, job.id)
        except Exception as exc:  # noqa: BLE001
            LOGGER.exception("job %s failed", job.id)
            with temporary_connection(settings.database_url) as db:
                mark_meeting_failed(db, job.meeting_id, str(exc))
                fail_job(db, job.id, str(exc))


def process_meeting_job(database_url: str, storage: LocalStorage, pipeline: AudioPipeline, job: Job, max_meeting_minutes: int) -> None:
    with temporary_connection(database_url) as db:
        meeting = fetch_meeting(db, job.meeting_id)
        update_meeting_status(db, job.meeting_id, "processing")

    input_path = storage.absolute_path(meeting["storage_path"])
    artifacts_dir = storage.artifacts_dir(job.meeting_id)
    LOGGER.info("processing meeting %s from %s", job.meeting_id, input_path)
    result = pipeline.process(job.meeting_id, input_path, artifacts_dir, max_meeting_minutes)

    with temporary_connection(database_url) as db:
        with db.transaction():
            with db.cursor() as cursor:
                cursor.execute("DELETE FROM reviewed_segments WHERE review_version_id IN (SELECT id FROM review_versions WHERE meeting_id = %s)", (job.meeting_id,))
                cursor.execute("DELETE FROM reviewed_speaker_slots WHERE review_version_id IN (SELECT id FROM review_versions WHERE meeting_id = %s)", (job.meeting_id,))
                cursor.execute("DELETE FROM review_versions WHERE meeting_id = %s", (job.meeting_id,))
                cursor.execute("DELETE FROM source_segments WHERE meeting_id = %s", (job.meeting_id,))
                cursor.execute("DELETE FROM speaker_slots WHERE meeting_id = %s", (job.meeting_id,))

                speaker_map: dict[str, str] = {}
                for label in result.speakers:
                    speaker_id = str(uuid.uuid4())
                    speaker_map[label] = speaker_id
                    cursor.execute(
                        """
                        INSERT INTO speaker_slots (id, meeting_id, label)
                        VALUES (%s, %s, %s)
                        """,
                        (speaker_id, job.meeting_id, label),
                    )

                for segment in result.segments:
                    cursor.execute(
                        """
                        INSERT INTO source_segments (
                            id, meeting_id, speaker_slot_id, start_ms, end_ms, text, confidence, overlap_flag, unclear_flag
                        ) VALUES (%s, %s, %s, %s, %s, %s, %s, %s, %s)
                        """,
                        (
                            str(uuid.uuid4()),
                            job.meeting_id,
                            speaker_map[segment.speaker_label],
                            segment.start_ms,
                            segment.end_ms,
                            segment.text,
                            segment.confidence,
                            segment.overlap_flag,
                            segment.unclear_flag,
                        ),
                    )

                cursor.execute(
                    """
                    UPDATE meetings
                    SET status = 'review_required',
                        normalized_path = %s,
                        duration_seconds = %s,
                        draft_preview_json = %s,
                        processed_at = NOW(),
                        updated_at = NOW(),
                        error_message = NULL
                    WHERE id = %s
                    """,
                    (
                        result.normalized_relative_path,
                        result.duration_seconds,
                        result.draft_preview_json,
                        job.meeting_id,
                    ),
                )
    LOGGER.info("meeting %s is ready for review with %d segments", job.meeting_id, len(result.segments))


def generate_report_job(database_url: str, reports: ReportGenerator, job: Job) -> None:
    with temporary_connection(database_url) as db:
        draft = fetch_reviewed_transcript(db, job.meeting_id)

    report = reports.generate(
        draft["transcript"],
        draft["evidence_segment_ids"],
        summary_markdown=draft.get("summary_markdown"),
        action_items=draft.get("action_items"),
        source=draft.get("source", "upload"),
    )
    markdown = markdown_from_report(report)

    with temporary_connection(database_url) as db:
        with db.transaction():
            with db.cursor() as cursor:
                cursor.execute(
                    """
                    UPDATE meetings
                    SET status = 'completed',
                        report_json = %s,
                        report_markdown = %s,
                        finalized_at = NOW(),
                        updated_at = NOW(),
                        review_lock_id = NULL,
                        review_lock_expires_at = NULL
                    WHERE id = %s
                    """,
                    (json.dumps(report), markdown, job.meeting_id),
                )
    LOGGER.info("meeting %s report generation completed", job.meeting_id)


def reconnect(connection, database_url: str):
    try:
        connection.close()
    except Exception:  # noqa: BLE001
        pass
    return connect(database_url)


class temporary_connection:
    def __init__(self, database_url: str) -> None:
        self.database_url = database_url
        self.connection = None

    def __enter__(self):
        self.connection = connect(self.database_url)
        return self.connection

    def __exit__(self, exc_type, exc, tb):
        if self.connection is not None:
            self.connection.close()
        return False


def fetch_meeting(connection, meeting_id: str) -> dict:
    with connection.cursor() as cursor:
        cursor.execute("SELECT id, storage_path FROM meetings WHERE id = %s", (meeting_id,))
        row = cursor.fetchone()
        if row is None:
            raise RuntimeError(f"meeting {meeting_id} not found")
        return row


def fetch_reviewed_transcript(connection, meeting_id: str) -> dict[str, object]:
    with connection.cursor() as cursor:
        cursor.execute(
            """
            SELECT source, fathom_summary_markdown, fathom_action_items_json
            FROM meetings
            WHERE id = %s
            """,
            (meeting_id,),
        )
        meeting = cursor.fetchone()
        if meeting is None:
            raise RuntimeError(f"meeting {meeting_id} not found")

        cursor.execute(
            """
            WITH latest_review AS (
                SELECT id
                FROM review_versions
                WHERE meeting_id = %s
                ORDER BY version DESC
                LIMIT 1
            )
            SELECT
                ss.id,
                COALESCE(rss.assigned_name, sp.assigned_name, sp.label) AS speaker_name,
                COALESCE(rs.edited_text, ss.text) AS text,
                COALESCE(rs.unclear_override, FALSE) AS unclear_override,
                ss.start_ms
            FROM source_segments ss
            JOIN speaker_slots sp ON sp.id = ss.speaker_slot_id
            LEFT JOIN latest_review lr ON TRUE
            LEFT JOIN reviewed_segments rs
              ON rs.review_version_id = lr.id
             AND rs.source_segment_id = ss.id
            LEFT JOIN reviewed_speaker_slots rss
              ON rss.review_version_id = lr.id
             AND rss.speaker_slot_id = COALESCE(rs.assigned_speaker_slot_id, ss.speaker_slot_id)
            WHERE ss.meeting_id = %s
            ORDER BY ss.start_ms
            """,
            (meeting_id, meeting_id),
        )
        rows = cursor.fetchall()

    transcript_lines: list[str] = []
    evidence_segment_ids: list[str] = []
    for row in rows:
        speaker = row["speaker_name"]
        text = row["text"]
        if row["unclear_override"]:
            text = "Unclear segment flagged during review."
        transcript_lines.append(f"{speaker}: {text}")
        evidence_segment_ids.append(str(row["id"]))

    return {
        "source": meeting["source"],
        "summary_markdown": meeting.get("fathom_summary_markdown"),
        "action_items": parse_action_items(meeting.get("fathom_action_items_json")),
        "transcript": "\n".join(transcript_lines),
        "evidence_segment_ids": evidence_segment_ids,
    }


def parse_action_items(raw_value) -> list[str] | None:
    if not raw_value:
        return None
    try:
        items = json.loads(raw_value)
    except json.JSONDecodeError:
        return None

    parsed: list[str] = []
    for item in items:
        if isinstance(item, str) and item.strip():
            parsed.append(item.strip())
            continue
        if not isinstance(item, dict):
            continue
        for key in ("description", "text", "title", "action_item"):
            value = item.get(key)
            if isinstance(value, str) and value.strip():
                parsed.append(value.strip())
                break
    return parsed or None


def update_meeting_status(connection, meeting_id: str, status: str) -> None:
    with connection.transaction():
        with connection.cursor() as cursor:
            cursor.execute(
                """
                UPDATE meetings
                SET status = %s, updated_at = NOW(), error_message = NULL
                WHERE id = %s
                """,
                (status, meeting_id),
            )


def mark_meeting_failed(connection, meeting_id: str, error_message: str) -> None:
    with connection.transaction():
        with connection.cursor() as cursor:
            cursor.execute(
                """
                UPDATE meetings
                SET status = 'failed',
                    error_message = %s,
                    updated_at = NOW()
                WHERE id = %s
                """,
                (error_message, meeting_id),
            )


if __name__ == "__main__":
    main()
