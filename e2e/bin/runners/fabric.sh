#!/bin/sh
# Runner shim: fabric CLI (llm-pipe class).
# System injection: PROMPT-PREFIX FALLBACK (no per-call system flag;
# patterns live in user config which this shim must not touch).
# Provider: ollama://<model> -> -V Ollama -m <model>; anthropic://<model>
# -> -V Anthropic. base_url in the URI is IGNORED (fabric endpoint comes
# from its own config) — a mismatch warning goes to stderr.
set -eu
. "$(dirname "$0")/lib.sh"
VENDOR=""; MODEL=""
case "${EVOL_PROVIDER:-}" in
  "") ;;
  ollama://*)   VENDOR="Ollama";    MODEL="${EVOL_PROVIDER#ollama://}" ;;
  anthropic://*) VENDOR="Anthropic"; MODEL="${EVOL_PROVIDER#anthropic://}" ;;
  *) echo "fabric.sh: unsupported EVOL_PROVIDER '$EVOL_PROVIDER' (want ollama://<m> or anthropic://<m>)" >&2; exit 64 ;;
esac
case "$MODEL" in
  *\?*) echo "fabric.sh: note — URI params ignored; fabric uses its own endpoint config" >&2
        MODEL="${MODEL%%\?*}" ;;
esac
if [ -n "$MODEL" ]; then
  printf '%s\n\n%s' "$SYSTEM" "$TASK" | fabric -V "$VENDOR" -m "$MODEL"
else
  printf '%s\n\n%s' "$SYSTEM" "$TASK" | fabric
fi
