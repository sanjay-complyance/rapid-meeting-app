from __future__ import annotations

from dataclasses import dataclass
from pathlib import Path


@dataclass(frozen=True)
class LocalStorage:
    root: Path

    def absolute_path(self, relative_path: str) -> Path:
        return (self.root / Path(relative_path)).resolve()

    def artifacts_dir(self, meeting_id: str) -> Path:
        directory = self.root / "artifacts" / meeting_id
        directory.mkdir(parents=True, exist_ok=True)
        return directory

