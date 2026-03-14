from app.pipeline import SpeakerTurn, assign_speaker_label, normalize_speaker_turns


def test_normalize_speaker_turns_renames_pyannote_labels_in_order() -> None:
    turns = [
        SpeakerTurn(start=4.0, end=6.0, label="SPEAKER_02"),
        SpeakerTurn(start=0.0, end=2.0, label="SPEAKER_09"),
        SpeakerTurn(start=2.0, end=4.0, label="SPEAKER_02"),
    ]

    normalized = normalize_speaker_turns(turns)

    assert [turn.label for turn in normalized] == ["Speaker 1", "Speaker 2", "Speaker 2"]


def test_assign_speaker_label_prefers_overlap_then_nearest_turn() -> None:
    turns = [
        SpeakerTurn(start=0.0, end=2.0, label="Speaker 1"),
        SpeakerTurn(start=4.0, end=6.0, label="Speaker 2"),
    ]

    assert assign_speaker_label(1.0, 1.5, turns) == ("Speaker 1", False)
    assert assign_speaker_label(3.0, 3.5, turns) == ("Speaker 2", False)


def test_assign_speaker_label_marks_overlap_when_multiple_turns_match() -> None:
    turns = [
        SpeakerTurn(start=0.0, end=3.0, label="Speaker 1"),
        SpeakerTurn(start=1.0, end=4.0, label="Speaker 2"),
    ]

    assert assign_speaker_label(1.5, 2.5, turns) == ("Speaker 1", True)
