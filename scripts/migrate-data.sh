#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CONFIG_PATH="${1:-${ROOT_DIR}/config.yaml}"

cd "${ROOT_DIR}"
GOCACHE="${GOCACHE:-/tmp/gocache}" go run ./cmd/migrate-data -c "${CONFIG_PATH}" -root "${ROOT_DIR}"
