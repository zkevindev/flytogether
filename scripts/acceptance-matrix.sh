#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
: "${QA_BASE_URL:?Set QA_BASE_URL to an isolated test server}"
OUT_ROOT="${QA_OUTPUT_ROOT:-$ROOT/test-results/acceptance}"

run_browser_case() {
  local name="$1" script="$2"
  printf '\n=== %s ===\n' "$script"
  QA_OUTPUT_DIR="$OUT_ROOT/$name" npm --prefix "$ROOT/web" run "$script"
}

run_browser_case network-humans test:network:browser
run_browser_case network-ai test:network:bot
run_browser_case props test:props:browser
run_browser_case lifecycle test:lifecycle:simulation

printf '\n=== voice client and PCM capture ===\n'
npm --prefix "$ROOT/web" run test:voice
