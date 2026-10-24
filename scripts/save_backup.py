#!/usr/bin/env python3
"""Create a consistent SQLite save backup, including a live WAL, without credentials."""
import argparse
import os
from pathlib import Path
import sqlite3
import tempfile

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--database', type=Path, required=True)
parser.add_argument('--output', type=Path, required=True)
args = parser.parse_args()
source = args.database.expanduser().resolve()
target = args.output.expanduser().resolve()
if not source.is_file():
    raise SystemExit('Save database does not exist')
if source == target or target.exists():
    raise SystemExit('Use a new backup path; existing files are never overwritten')
target.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
fd, temporary = tempfile.mkstemp(prefix='.flytogether-backup-', dir=target.parent)
os.close(fd)
try:
    with sqlite3.connect(source.as_uri() + '?mode=ro', uri=True, timeout=10) as origin:
        with sqlite3.connect(temporary) as destination:
            origin.backup(destination)
            if destination.execute('PRAGMA quick_check').fetchone()[0] != 'ok':
                raise RuntimeError('Backup failed integrity verification')
    os.chmod(temporary, 0o600)
    with open(temporary, 'rb') as backup:
        os.fsync(backup.fileno())
    # Atomic no-overwrite publish in the same directory.
    os.link(temporary, target)
    directory = os.open(target.parent, os.O_RDONLY)
    try:
        os.fsync(directory)
    finally:
        os.close(directory)
    print('Backup created and integrity verified')
finally:
    Path(temporary).unlink(missing_ok=True)
