from __future__ import annotations

import logging
import math
import inspect
import subprocess
from dataclasses import dataclass
from pathlib import Path

LOGGER = logging.getLogger(__name__)
LOW_CONFIDENCE_THRESHOLD = 0.45


@dataclass
class Segment:
    start_ms: int
    end_ms: int
    text: str
    confidence: float
    speaker_label: str
    overlap_flag: bool = False
    unclear_flag: bool = False


@dataclass(frozen=True)
class SpeakerTurn:
    start: float
    end: float
    label: str


@dataclass
class ProcessingResult:
    duration_seconds: float
    normalized_relative_path: str
    speakers: list[str]
    segments: list[Segment]
    draft_preview_json: str


class AudioPipeline:
    def __init__(
        self,
        storage_root: Path,
        transcriber: str,
        diarizer: str,
        whisper_model: str,
        whisper_device: str,
        whisper_compute_type: str,
        pyannote_auth_token: str | None,
    ) -> None:
        self.storage_root = storage_root
        self.transcriber = transcriber
        self.diarizer = diarizer
        self.whisper_model = whisper_model
        self.whisper_device = whisper_device
        self.whisper_compute_type = whisper_compute_type
        self.pyannote_auth_token = pyannote_auth_token

    def process(self, meeting_id: str, input_path: Path, artifacts_dir: Path, max_minutes: int) -> ProcessingResult:
        duration = probe_duration(input_path)
        if duration > max_minutes * 60:
            raise RuntimeError(f"meeting exceeds {max_minutes} minute limit")

        normalized_path = artifacts_dir / "normalized.wav"
        normalize_audio(input_path, normalized_path)

        segments, preview = self._transcribe_and_diarize(normalized_path)
        if not segments:
            raise RuntimeError("transcription returned no segments")
        speakers = sorted({segment.speaker_label for segment in segments})

        return ProcessingResult(
            duration_seconds=duration,
            normalized_relative_path=str(normalized_path.relative_to(self.storage_root)),
            speakers=speakers,
            segments=segments,
            draft_preview_json=preview,
        )

    def _transcribe_and_diarize(self, normalized_path: Path) -> tuple[list[Segment], str]:
        if self.transcriber != "faster_whisper":
            raise RuntimeError(f"unsupported transcriber: {self.transcriber}")
        return faster_whisper_segments(
            normalized_path=normalized_path,
            model_name=self.whisper_model,
            device=self.whisper_device,
            compute_type=self.whisper_compute_type,
            diarizer=self.diarizer,
            pyannote_auth_token=self.pyannote_auth_token,
        )


def probe_duration(path: Path) -> float:
    result = subprocess.run(
        [
            "ffprobe",
            "-v",
            "error",
            "-show_entries",
            "format=duration",
            "-of",
            "default=noprint_wrappers=1:nokey=1",
            str(path),
        ],
        check=True,
        capture_output=True,
        text=True,
    )
    return float(result.stdout.strip())


def normalize_audio(input_path: Path, output_path: Path) -> None:
    subprocess.run(
        [
            "ffmpeg",
            "-y",
            "-i",
            str(input_path),
            "-ac",
            "1",
            "-ar",
            "16000",
            str(output_path),
        ],
        check=True,
        capture_output=True,
        text=True,
    )


def build_draft_preview(segments: list[Segment], diarization_mode: str) -> str:
    low_confidence = sum(1 for segment in segments if segment.confidence < LOW_CONFIDENCE_THRESHOLD)
    overlap = sum(1 for segment in segments if segment.overlap_flag)
    unclear = sum(1 for segment in segments if segment.unclear_flag)
    speaker_count = len({segment.speaker_label for segment in segments})
    return (
        "{"
        f"\"summary\": \"Draft transcript ready for review.\", "
        f"\"diarization_mode\": \"{diarization_mode}\", "
        f"\"speaker_count\": {speaker_count}, "
        f"\"quality_flags\": {{\"low_confidence_segments\": {low_confidence}, "
        f"\"overlap_segments\": {overlap}, \"unclear_segments\": {unclear}}}"
        "}"
    )


def faster_whisper_segments(
    normalized_path: Path,
    model_name: str,
    device: str,
    compute_type: str,
    diarizer: str,
    pyannote_auth_token: str | None,
) -> tuple[list[Segment], str]:
    from faster_whisper import WhisperModel

    LOGGER.info(
        "loading faster-whisper model=%s device=%s compute_type=%s",
        model_name,
        device,
        compute_type,
    )
    model = WhisperModel(model_name, device=device, compute_type=compute_type)
    LOGGER.info("starting transcription for %s", normalized_path.name)
    raw_segments, _ = model.transcribe(str(normalized_path), language="en", vad_filter=True)
    segments = list(raw_segments)
    LOGGER.info("transcription completed with %d raw segments", len(segments))
    if not segments:
        return [], build_draft_preview([], "none")

    speaker_assignments: list[SpeakerTurn] = []
    diarization_mode = "single_speaker"
    if diarizer == "pyannote" and pyannote_auth_token:
        try:
            from pyannote.audio import Pipeline

            signature = inspect.signature(Pipeline.from_pretrained)
            auth_kwargs: dict[str, str] = {}
            if "token" in signature.parameters:
                auth_kwargs["token"] = pyannote_auth_token
            else:
                auth_kwargs["use_auth_token"] = pyannote_auth_token

            pipeline = Pipeline.from_pretrained(
                "pyannote/speaker-diarization-3.1",
                **auth_kwargs,
            )
            diarization = pipeline(str(normalized_path))
            annotation = diarization
            if hasattr(diarization, "exclusive_speaker_diarization"):
                annotation = diarization.exclusive_speaker_diarization
            elif hasattr(diarization, "speaker_diarization"):
                annotation = diarization.speaker_diarization

            raw_turns = [SpeakerTurn(turn.start, turn.end, speaker) for turn, _, speaker in annotation.itertracks(yield_label=True)]
            speaker_assignments = normalize_speaker_turns(raw_turns)
            if speaker_assignments:
                diarization_mode = "pyannote"
        except Exception as exc:
            LOGGER.warning("pyannote diarization unavailable, falling back to single-speaker mode: %s", exc)
            diarization_mode = "single_speaker"
    elif diarizer == "pyannote":
        LOGGER.warning("PYANNOTE_AUTH_TOKEN is not set, falling back to single-speaker mode")

    output: list[Segment] = []
    for chunk in segments:
        start_ms = int(chunk.start * 1000)
        end_ms = int(chunk.end * 1000)
        speaker_label, overlap_flag = assign_speaker_label(chunk.start, chunk.end, speaker_assignments)
        output.append(
            Segment(
                start_ms=start_ms,
                end_ms=end_ms,
                text=normalize_text(chunk.text),
                confidence=confidence_from_logprob(getattr(chunk, "avg_logprob", None)),
                speaker_label=speaker_label,
                overlap_flag=overlap_flag,
                unclear_flag=not normalize_text(chunk.text),
            )
        )
    LOGGER.info("finalized %d transcript segments with diarization mode %s", len(output), diarization_mode)
    return output, build_draft_preview(output, diarization_mode)


def normalize_speaker_turns(turns: list[SpeakerTurn]) -> list[SpeakerTurn]:
    ordered = sorted(turns, key=lambda turn: (turn.start, turn.end, turn.label))
    label_map: dict[str, str] = {}
    normalized: list[SpeakerTurn] = []
    for turn in ordered:
        if turn.label not in label_map:
            label_map[turn.label] = f"Speaker {len(label_map) + 1}"
        normalized.append(SpeakerTurn(start=turn.start, end=turn.end, label=label_map[turn.label]))
    return normalized


def assign_speaker_label(segment_start: float, segment_end: float, turns: list[SpeakerTurn]) -> tuple[str, bool]:
    if not turns:
        return "Speaker 1", False

    overlaps: list[tuple[float, int, str]] = []
    for index, turn in enumerate(turns):
        overlap = max(0.0, min(segment_end, turn.end) - max(segment_start, turn.start))
        if overlap > 0:
            overlaps.append((overlap, index, turn.label))

    if overlaps:
        overlaps.sort(key=lambda item: (-item[0], item[1]))
        overlap_flag = len({label for _, _, label in overlaps}) > 1
        return overlaps[0][2], overlap_flag

    nearest = min(turns, key=lambda turn: distance_to_turn(segment_start, segment_end, turn))
    return nearest.label, False


def distance_to_turn(segment_start: float, segment_end: float, turn: SpeakerTurn) -> float:
    if segment_end < turn.start:
        return turn.start - segment_end
    if segment_start > turn.end:
        return segment_start - turn.end
    return 0.0


def normalize_text(text: str) -> str:
    return " ".join(text.strip().split())


def confidence_from_logprob(avg_logprob: float | None) -> float:
    if avg_logprob is None:
        return 0.6
    bounded = max(min(avg_logprob, 0.0), -6.0)
    return round(math.exp(bounded), 3)
