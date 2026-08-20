#!/bin/sh
# Runner shim: Gemini CLI (agent-cli class).
# System injection: PROMPT-PREFIX FALLBACK (no system flag in headless
# mode; candidate body prepended with a separator).
# Provider: gemini://<model> -> -m.
set -eu
. "$(dirname "$0")/lib.sh"
MODEL=""
case "${EVOL_PROVIDER:-}" in
  "") ;;
  gemini://*) MODEL="${EVOL_PROVIDER#gemini://}"; MODEL="${MODEL%%\?*}" ;;
  *) echo "gemini.sh: unsupported EVOL_PROVIDER '$EVOL_PROVIDER' (want gemini://<model>)" >&2; exit 64 ;;
esac
if [ -n "$MODEL" ]; then
  exec gemini -m "$MODEL" -p "$SYSTEM

$TASK"
else
  exec gemini -p "$SYSTEM

$TASK"
fi
