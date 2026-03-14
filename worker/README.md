# RAPID Worker

Python worker for:

- audio normalization
- transcription and diarization
- draft segment persistence
- RAPID report generation

The worker supports a stub pipeline by default and can be upgraded to real
`faster-whisper`, `pyannote.audio`, and Gemini-backed reporting by setting the
relevant environment variables.

