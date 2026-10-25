#!/usr/bin/env python3
"""Rehearse WAL-safe save backup and release-pointer rollback in a temp directory.

This does not start systemd, exercise the production binary, or access production data.
"""
from __future__ import annotations

import hashlib
import json
import os
from pathlib import Path
import sqlite3
import stat
import subprocess
import sys
import tempfile

ROOT = Path(__file__).resolve().parents[1]


def set_current(root: Path, target: Path) -> None:
    staged = root / ".current-rehearsal"
    staged.unlink(missing_ok=True)
    staged.symlink_to(target)
    os.replace(staged, root / "current")


def healthy_current(root: Path) -> bool:
    release = (root / "current").resolve(strict=True)
    return (release / "server.marker").is_file() and (release / "healthy.marker").is_file()


def main() -> None:
    with tempfile.TemporaryDirectory(prefix="flytogether-recovery-") as temp:
        root = Path(temp)
        source = root / "live.db"
        backup = root / "backup.db"
        writer = sqlite3.connect(source)
        try:
            assert writer.execute("PRAGMA journal_mode=WAL").fetchone()[0].lower() == "wal"
            writer.execute("CREATE TABLE committed_events (id INTEGER PRIMARY KEY, value TEXT NOT NULL)")
            writer.execute("INSERT INTO committed_events(value) VALUES ('before-release')")
            writer.commit()
            wal = Path(str(source) + "-wal")
            assert wal.exists() and wal.stat().st_size > 0, "fixture must include committed WAL data"

            run = subprocess.run(
                [sys.executable, str(ROOT / "scripts/save_backup.py"), "--database", str(source), "--output", str(backup)],
                capture_output=True, text=True, check=True,
            )
            assert "integrity verified" in run.stdout
            assert stat.S_IMODE(backup.stat().st_mode) == 0o600
            with sqlite3.connect(backup) as restored:
                assert restored.execute("PRAGMA quick_check").fetchone()[0] == "ok"
                rows = restored.execute("SELECT value FROM committed_events ORDER BY id").fetchall()
                assert rows == [("before-release",)]
            before_hash = hashlib.sha256(backup.read_bytes()).hexdigest()
            overwrite = subprocess.run(
                [sys.executable, str(ROOT / "scripts/save_backup.py"), "--database", str(source), "--output", str(backup)],
                capture_output=True, text=True,
            )
            assert overwrite.returncode != 0 and "never overwritten" in overwrite.stderr
            assert hashlib.sha256(backup.read_bytes()).hexdigest() == before_hash
        finally:
            writer.close()

        releases = root / "releases"
        old = releases / "release-good"
        failed = releases / "release-bad"
        old.mkdir(parents=True)
        failed.mkdir(parents=True)
        (old / "server.marker").write_text("good")
        (old / "healthy.marker").write_text("ok")
        (failed / "server.marker").write_text("bad")
        set_current(root, old)
        assert healthy_current(root)

        # Simulate deploying and restarting an unhealthy candidate, then use the
        # runbook's atomic current-link switch to return to the last good release.
        set_current(root, failed)
        assert not healthy_current(root), "failed release should fail the health gate"
        set_current(root, old)
        assert healthy_current(root), "rollback should restore a healthy release pointer"
        assert (root / "current").resolve() == old.resolve()

        print(json.dumps({
            "passed": True,
            "backup": {"wal_committed_rows": len(rows), "quick_check": "ok", "mode": "0600", "overwrite_refused": True},
            "release_rollback": {"failed_candidate_rejected": True, "restored_release": old.name, "healthy_after_pointer_restore": True},
            "production_touched": False,
            "limits": ["systemd restart and real application health were not invoked", "database schema downgrade was not attempted"],
        }, ensure_ascii=False))


if __name__ == "__main__":
    main()
