#!/bin/sh
# Runner shim: llm CLI, Simon Willison (llm-pipe class).
# System injection: native (-s). Provider: llm://<model> or ollama://<model>
# (the latter needs the llm-ollama plugin) -> -m <model>.
set -eu
. "$(dirname "$0")/lib.sh"
MODEL=""
case "${EVOL_PROVIDER:-}" in
  "") ;;
  llm://*)    MODEL="${EVOL_PROVIDER#llm://}" ;;
  ollama://*) MODEL="${EVOL_PROVIDER#ollama://}"; MODEL="${MODEL%%\?*}" ;;
  *) echo "llm.sh: unsupported EVOL_PROVIDER '$EVOL_PROVIDER' (want llm://<m> or ollama://<m>)" >&2; exit 64 ;;
esac
if [ -n "$MODEL" ]; then
  printf '%s' "$TASK" | llm -s "$SYSTEM" -m "$MODEL"
else
  printf '%s' "$TASK" | llm -s "$SYSTEM"
fi
