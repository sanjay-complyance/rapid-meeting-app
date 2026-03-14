from __future__ import annotations

import os
from dataclasses import dataclass
from pathlib import Path


@dataclass(frozen=True)
class Settings:
    database_url: str
    storage_root: Path
    poll_interval_seconds: int
    job_stale_after_seconds: int
    max_meeting_minutes: int
    transcriber: str
    diarizer: str
    whisper_model: str
    whisper_device: str
    whisper_compute_type: str
    pyannote_auth_token: str | None
    gemini_api_key: str | None
    gemini_model: str
    worker_name: str


def load_settings() -> Settings:
    database_url = os.getenv("DATABASE_URL", "")
    if not database_url:
        raise RuntimeError("DATABASE_URL is required")

    return Settings(
        database_url=database_url,
        storage_root=Path(os.getenv("STORAGE_ROOT", "../data")).resolve(),
        poll_interval_seconds=int(os.getenv("JOB_POLL_INTERVAL_SECONDS", "5")),
        job_stale_after_seconds=int(os.getenv("JOB_STALE_AFTER_SECONDS", "600")),
        max_meeting_minutes=45,
        transcriber=os.getenv("RAPID_TRANSCRIBER", "faster_whisper"),
        diarizer=os.getenv("RAPID_DIARIZER", "pyannote"),
        whisper_model=os.getenv("RAPID_WHISPER_MODEL", "tiny.en"),
        whisper_device=os.getenv("RAPID_WHISPER_DEVICE", "auto"),
        whisper_compute_type=os.getenv("RAPID_WHISPER_COMPUTE_TYPE", "int8"),
        pyannote_auth_token=os.getenv("PYANNOTE_AUTH_TOKEN") or None,
        gemini_api_key=os.getenv("GEMINI_API_KEY") or None,
        gemini_model=os.getenv("GEMINI_MODEL", "gemini-2.0-flash"),
        worker_name=os.getenv("RAPID_WORKER_NAME", "worker-1"),
    )
