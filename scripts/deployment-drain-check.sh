#!/usr/bin/env bash
set -euo pipefail

data_dir="${1:-${DATA_DIR:-}}"
if [[ -z "$data_dir" ]]; then
  printf 'DATA_DIR is required; source the service environment or pass the data directory as the first argument.\n' >&2
  exit 2
fi

journal="$data_dir/.active-jobs.json"
if [[ -s "$journal" ]]; then
  printf 'deployment blocked: accepted agent jobs are still queued or active; wait for completion, then check again.\n' >&2
  exit 1
fi

printf 'deployment drain check passed: no accepted agent jobs are pending.\n'
