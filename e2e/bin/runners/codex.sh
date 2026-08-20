#!/bin/sh
# Runner shim: Codex CLI (agent-cli class).
# System injection: PROMPT-PREFIX FALLBACK (exec takes one prompt; the
# candidate body is prepended with a separator). Clean output via
# --output-last-message. Provider: codex://<model> -> -m;
# ollama://<model> -> --oss --local-provider ollama -m <model>
# (endpoint via OLLAMA_HOST when the URI carries base_url).
set -eu
. "$(dirname "$0")/lib.sh"
OUT=$(mktemp); trap 'rm -f "$OUT"' EXIT
set -- exec -s read-only --color never -o "$OUT"
HOSTENV=""
case "${EVOL_PROVIDER:-}" in
  "") ;;
  codex://*)
    M="${EVOL_PROVIDER#codex://}"; M="${M%%\?*}"
    set -- "$@" -m "$M" ;;
  ollama://*)
    REST="${EVOL_PROVIDER#ollama://}"; M="${REST%%\?*}"
    case "$REST" in
      *base_url=*) HOSTENV="${REST#*base_url=}"; HOSTENV="${HOSTENV%%&*}" ;;
    esac
    set -- "$@" --oss --local-provider ollama -m "$M" ;;
  *) echo "codex.sh: unsupported EVOL_PROVIDER '$EVOL_PROVIDER' (want codex://<m> or ollama://<m>)" >&2; exit 64 ;;
esac
if [ -n "$HOSTENV" ]; then
  OLLAMA_HOST="$HOSTENV" codex "$@" "$SYSTEM

$TASK" >/dev/null
else
  codex "$@" "$SYSTEM

$TASK" >/dev/null
fi
cat "$OUT"
