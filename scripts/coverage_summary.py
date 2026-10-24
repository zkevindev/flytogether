#!/usr/bin/env python3
"""Render Go statement coverage by package without averaging package percentages."""
import sys
from collections import defaultdict
from pathlib import Path

counts = defaultdict(lambda: [0, 0])
blocks = {}
for line in Path(sys.argv[1]).read_text().splitlines()[1:]:
    location, statements, hits = line.rsplit(' ', 2)
    n = int(statements)
    prior = blocks.get(location, (n, 0))
    if prior[0] != n:
        raise ValueError('Inconsistent coverage block size')
    blocks[location] = (n, prior[1] + int(hits))
for location, (n, hits) in blocks.items():
    package = location.rsplit('/', 1)[0]
    counts[package][1] += n
    if hits > 0:
        counts[package][0] += n
covered = sum(v[0] for v in counts.values())
total = sum(v[1] for v in counts.values())
ratio = lambda a, b: f'{100*a/b:.1f}%' if b else 'N/A'
print(f'## Go statement coverage: {ratio(covered, total)}\n')
print('Includes all Go packages via `-coverpkg=./...`; not frontend or whole-product coverage.\n')
print('| Package | Covered statements | Total | Coverage |\n| --- | ---: | ---: | ---: |')
for package, (a, b) in sorted(counts.items()):
    print(f'| `{package}` | {a} | {b} | {ratio(a,b)} |')
print('\nDownload the run artifact for coverage.out, function coverage, HTML and test output. A report may be partial after failed tests; inspect job status.')
