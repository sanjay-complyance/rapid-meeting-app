from __future__ import annotations

import json
import logging
import re
import time
from dataclasses import dataclass
from typing import Any

import requests


RAPID_FALLBACK = "Not established in discussion"
LOGGER = logging.getLogger(__name__)
RETRYABLE_STATUS_CODES = {429, 500, 503, 504}
MAX_GEMINI_TRANSCRIPT_CHARS = 16000
REPORT_SCHEMA = {
    "type": "object",
    "properties": {
        "summary": {"type": "string"},
        "decision": {"type": "string"},
        "recommend": {"type": "string"},
        "agree": {"type": "string"},
        "perform": {"type": "string"},
        "input": {"type": "string"},
        "decide": {"type": "string"},
        "rationale": {"type": "string"},
        "risks_or_open_questions": {"type": "string"},
        "action_items": {"type": "array", "items": {"type": "string"}},
        "evidence_segment_ids": {"type": "array", "items": {"type": "string"}},
    },
    "required": [
        "summary",
        "decision",
        "recommend",
        "agree",
        "perform",
        "input",
        "decide",
        "rationale",
        "risks_or_open_questions",
        "action_items",
        "evidence_segment_ids",
    ],
}


@dataclass
class ReportGenerator:
    api_key: str | None
    model: str

    def generate(
        self,
        transcript: str,
        evidence_segment_ids: list[str],
        summary_markdown: str | None = None,
        action_items: list[str] | None = None,
        source: str = "upload",
    ) -> dict[str, Any]:
        if self.api_key:
            try:
                report = self._generate_with_gemini(transcript, summary_markdown, action_items, source)
                if report:
                    report.setdefault("evidence_segment_ids", evidence_segment_ids)
                    return ensure_report_shape(report)
            except requests.RequestException as exc:
                details = exc.response.text[:500] if exc.response is not None else str(exc)
                LOGGER.warning("Gemini generation failed, falling back to template report: %s", details)
            except Exception as exc:  # noqa: BLE001
                LOGGER.warning("Gemini generation failed, falling back to template report: %s", exc)
        return ensure_report_shape(template_report(transcript, evidence_segment_ids, action_items))

    def _generate_with_gemini(
        self,
        transcript: str,
        summary_markdown: str | None,
        action_items: list[str] | None,
        source: str,
    ) -> dict[str, Any] | None:
        compact_transcript = compact_transcript_for_llm(transcript)
        summary_section = f"\nFathom summary (supporting context only):\n{summary_markdown}\n" if summary_markdown else ""
        action_item_section = ""
        if action_items:
            action_item_section = "\nImported action items (preserve if supported by transcript):\n" + "\n".join(
                f"- {item}" for item in action_items
            )
        prompt = f"""
You are producing a RAPID decision record from an English meeting transcript.
Return a concise RAPID decision record grounded only in the transcript.
If a field is not supported by the transcript, use "Not established in discussion".
Do not invent names, decisions, action owners, or rationale.
Only include action items explicitly stated in the meeting.
This meeting source is: {source}.{summary_section}{action_item_section}

Transcript:
{compact_transcript}
"""
        request_body = {
            "contents": [{"parts": [{"text": prompt}]}],
            "generationConfig": {
                "temperature": 0.2,
                "responseMimeType": "application/json",
                "responseJsonSchema": REPORT_SCHEMA,
            },
        }

        last_error: Exception | None = None
        for attempt in range(3):
            try:
                response = requests.post(
                    f"https://generativelanguage.googleapis.com/v1beta/models/{self.model}:generateContent",
                    headers={
                        "Content-Type": "application/json",
                        "x-goog-api-key": self.api_key or "",
                    },
                    json=request_body,
                    timeout=120,
                )
                if response.status_code in RETRYABLE_STATUS_CODES and attempt < 2:
                    time.sleep(2**attempt)
                    continue
                response.raise_for_status()
                payload = response.json()
                text = extract_candidate_text(payload)
                return extract_json(text)
            except requests.RequestException as exc:
                last_error = exc
                response = getattr(exc, "response", None)
                if response is not None and response.status_code in RETRYABLE_STATUS_CODES and attempt < 2:
                    time.sleep(2**attempt)
                    continue
                if attempt < 2:
                    time.sleep(2**attempt)
                    continue
                raise
        if last_error is not None:
            raise last_error
        return None


def extract_candidate_text(payload: dict[str, Any]) -> str:
    candidates = payload.get("candidates") or []
    if not candidates:
        raise ValueError(f"Gemini returned no candidates: {json.dumps(payload)[:500]}")

    parts = candidates[0].get("content", {}).get("parts", [])
    texts = [part.get("text", "") for part in parts if isinstance(part, dict) and part.get("text")]
    if not texts:
        raise ValueError(f"Gemini candidate did not contain text parts: {json.dumps(payload)[:500]}")
    return "\n".join(texts)


def compact_transcript_for_llm(transcript: str) -> str:
    compact_lines: list[str] = []
    current_speaker: str | None = None
    current_parts: list[str] = []

    for raw_line in transcript.splitlines():
        line = raw_line.strip()
        if not line or ":" not in line:
            continue
        speaker, text = line.split(":", 1)
        speaker = speaker.strip()
        text = normalize_llm_line(text)
        if not text or is_filler_line(text):
            continue

        if current_speaker == speaker:
            current_parts.append(text)
            continue

        if current_speaker and current_parts:
            compact_lines.append(f"{current_speaker}: {' '.join(current_parts)}")
        current_speaker = speaker
        current_parts = [text]

    if current_speaker and current_parts:
        compact_lines.append(f"{current_speaker}: {' '.join(current_parts)}")

    compact = "\n".join(compact_lines)
    if len(compact) <= MAX_GEMINI_TRANSCRIPT_CHARS:
        return compact
    return compact[:MAX_GEMINI_TRANSCRIPT_CHARS]


def normalize_llm_line(text: str) -> str:
    return re.sub(r"\s+", " ", text).strip()


def is_filler_line(text: str) -> bool:
    cleaned = re.sub(r"[^a-zA-Z ]", "", text).strip().lower()
    if not cleaned:
        return True
    filler_phrases = {
        "uh",
        "um",
        "hmm",
        "yeah",
        "okay",
        "ok",
        "right",
        "cool",
        "sure",
        "huh",
        "oh",
        "no",
        "yes",
    }
    words = cleaned.split()
    return len(words) <= 3 and cleaned in filler_phrases


def extract_json(text: str) -> dict[str, Any] | None:
    cleaned = text.strip()
    if cleaned.startswith("```"):
        cleaned = re.sub(r"^```(?:json)?", "", cleaned).strip()
        cleaned = re.sub(r"```$", "", cleaned).strip()
    try:
        return json.loads(cleaned)
    except json.JSONDecodeError:
        match = re.search(r"\{.*\}", cleaned, flags=re.DOTALL)
        if not match:
            return None
        try:
            return json.loads(match.group(0))
        except json.JSONDecodeError:
            return None


def template_report(transcript: str, evidence_segment_ids: list[str], action_items: list[str] | None = None) -> dict[str, Any]:
    lines = [line.strip() for line in transcript.splitlines() if line.strip()]
    summary = " ".join(lines[:3])[:500] if lines else RAPID_FALLBACK
    decision = first_matching_line(lines, ("decide", "decision", "approved", "agreed", "go with"))
    rationale = first_matching_line(lines, ("because", "due to", "reason", "rationale"))
    action_lines = action_items or [
        clean_action_line(line)
        for line in lines
        if any(token in line.lower() for token in ("action", "next step", "follow up", "will ", "owner", "todo"))
    ]
    action_items = [line for line in action_lines if line]

    return {
        "summary": summary or RAPID_FALLBACK,
        "decision": decision,
        "recommend": RAPID_FALLBACK,
        "agree": RAPID_FALLBACK,
        "perform": RAPID_FALLBACK,
        "input": RAPID_FALLBACK,
        "decide": RAPID_FALLBACK,
        "rationale": rationale,
        "risks_or_open_questions": RAPID_FALLBACK,
        "action_items": action_items or [RAPID_FALLBACK],
        "evidence_segment_ids": evidence_segment_ids,
    }


def ensure_report_shape(report: dict[str, Any]) -> dict[str, Any]:
    output = {
        "summary": report.get("summary") or RAPID_FALLBACK,
        "decision": report.get("decision") or RAPID_FALLBACK,
        "recommend": report.get("recommend") or RAPID_FALLBACK,
        "agree": report.get("agree") or RAPID_FALLBACK,
        "perform": report.get("perform") or RAPID_FALLBACK,
        "input": report.get("input") or RAPID_FALLBACK,
        "decide": report.get("decide") or RAPID_FALLBACK,
        "rationale": report.get("rationale") or RAPID_FALLBACK,
        "risks_or_open_questions": report.get("risks_or_open_questions") or RAPID_FALLBACK,
        "action_items": report.get("action_items") or [RAPID_FALLBACK],
        "evidence_segment_ids": report.get("evidence_segment_ids") or [],
    }
    return output


def markdown_from_report(report: dict[str, Any]) -> str:
    action_items = report.get("action_items") or [RAPID_FALLBACK]
    action_lines = "\n".join(f"- {item}" for item in action_items)
    evidence = ", ".join(report.get("evidence_segment_ids") or [])
    sections = [
        "# RAPID Report",
        f"## Summary\n\n{report['summary']}",
        f"## Decision\n\n{report['decision']}",
        f"## Recommend\n\n{report['recommend']}",
        f"## Agree\n\n{report['agree']}",
        f"## Perform\n\n{report['perform']}",
        f"## Input\n\n{report['input']}",
        f"## Decide\n\n{report['decide']}",
        f"## Rationale\n\n{report['rationale']}",
        f"## Risks Or Open Questions\n\n{report['risks_or_open_questions']}",
        f"## Action Items\n\n{action_lines}",
    ]
    if evidence:
        sections.append(f"## Evidence Segment IDs\n\n{evidence}")
    return "\n\n".join(sections)


def first_matching_line(lines: list[str], tokens: tuple[str, ...]) -> str:
    for line in lines:
        lowered = line.lower()
        if any(token in lowered for token in tokens):
            return line
    return RAPID_FALLBACK


def clean_action_line(line: str) -> str:
    cleaned = re.sub(r"^\s*[-*]\s*", "", line).strip()
    return cleaned[:240]
