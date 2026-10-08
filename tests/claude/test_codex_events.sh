#!/usr/bin/env bash
# Exercise event parsing and driver behavior with a fake CLI; no model or network.
set -euo pipefail
ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/../.." && pwd)
PYTHONDONTWRITEBYTECODE=1 python3 "$ROOT/tests/test_codex_events.py" -v
