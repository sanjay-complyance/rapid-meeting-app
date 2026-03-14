from app.reporting import compact_transcript_for_llm, ensure_report_shape, extract_json, template_report


def test_extract_json_from_fenced_block() -> None:
    payload = extract_json("""```json\n{"summary":"ok"}\n```""")
    assert payload == {"summary": "ok"}


def test_template_report_returns_required_fields() -> None:
    report = template_report("Alice: We decided to ship next week.\nBob: Next step is QA.", ["seg-1"])
    ensured = ensure_report_shape(report)
    assert ensured["summary"]
    assert ensured["decision"]
    assert ensured["action_items"]
    assert ensured["evidence_segment_ids"] == ["seg-1"]


def test_compact_transcript_for_llm_merges_speaker_lines_and_drops_fillers() -> None:
    transcript = "\n".join(
        [
            "Speaker 1: okay",
            "Speaker 1: We should switch the screens now.",
            "Speaker 1: It will take some time.",
            "Speaker 2: uh",
            "Speaker 2: I can stay online if needed.",
        ]
    )

    compact = compact_transcript_for_llm(transcript)

    assert "Speaker 1: We should switch the screens now. It will take some time." in compact
    assert "Speaker 2: I can stay online if needed." in compact
    assert "Speaker 1: okay" not in compact
