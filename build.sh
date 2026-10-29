#!/bin/sh
set -eu
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
cd "$ROOT/src/frontend"
npm ci
npm run build
cd "$ROOT"
mkdir -p runtime
go build -trimpath -o runtime/ReveriePlanner ./src/backend
printf 'Built %s/runtime/ReveriePlanner\n' "$ROOT"
