#!/bin/sh
set -eu
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
if [ ! -x "$ROOT/runtime/ReveriePlanner" ]; then
  printf 'Missing executable. Build with ./build.sh or extract a portable package.\n' >&2
  exit 1
fi
printf 'Reverie Planner: open http://127.0.0.1:8765 (or the port in config/app.json).\n'
printf 'Keep this terminal open. Use ./stop.sh for graceful shutdown.\n'
exec "$ROOT/runtime/ReveriePlanner" --root "$ROOT"
