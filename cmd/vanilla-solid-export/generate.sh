#!/usr/bin/env bash
set -euo pipefail
ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
cd "$ROOT"
./cmd/vanilla-solid-export/run.sh | go run ./cmd/vanilla-solid-export -input /dev/stdin -output server/internal/javadata/block_solid_generated.go -defaults-output server/internal/javadata/block_defaults_generated.go -faces-output server/internal/javadata/block_faces_generated.go -properties-output server/internal/javadata/block_properties_generated.go
