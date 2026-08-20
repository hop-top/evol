#!/bin/sh
# Runner shim: opencode CLI (agent-cli class).
# System injection: PROMPT-PREFIX FALLBACK (no system flag on `run`).
# Provider: opencode://<provider/model> -> -m (opencode's own format),
# ollama://<model> -> -m ollama/<model> (needs ollama configured in
# opencode's provider list).
set -eu
. "$(dirname "$0")/lib.sh"
MODEL=""
case "${EVOL_PROVIDER:-}" in
  "") ;;
  opencode://*) MODEL="${EVOL_PROVIDER#opencode://}" ;;
  ollama://*)   M="${EVOL_PROVIDER#ollama://}"; MODEL="ollama/${M%%\?*}" ;;
  *) echo "opencode.sh: unsupported EVOL_PROVIDER '$EVOL_PROVIDER' (want opencode://<provider/model> or ollama://<m>)" >&2; exit 64 ;;
esac
if [ -n "$MODEL" ]; then
  exec opencode run --pure -m "$MODEL" "$SYSTEM

$TASK"
else
  exec opencode run --pure "$SYSTEM

$TASK"
fi
