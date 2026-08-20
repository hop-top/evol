#!/bin/sh
# Runner shim: Claude Code CLI (agent-cli class).
# System injection: native (--append-system-prompt).
# Provider: claude://<model> -> --model. Foreign schemes fail fast.
set -eu
. "$(dirname "$0")/lib.sh"
MODEL="${EVOL_AGENT_MODEL:-haiku}"
case "${EVOL_PROVIDER:-}" in
  "") ;;
  claude://*)
    MODEL="${EVOL_PROVIDER#claude://}"; MODEL="${MODEL%%\?*}" ;;
  *)
    echo "claude.sh: unsupported EVOL_PROVIDER '$EVOL_PROVIDER' (want claude://<model>)" >&2
    exit 64 ;;
esac
exec claude -p --model "$MODEL" --append-system-prompt "$SYSTEM" "$TASK"
