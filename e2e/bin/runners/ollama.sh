#!/bin/sh
# Runner shim: Ollama HTTP API (llm-pipe class).
# System injection: native (chat messages[role=system]).
# Provider: ollama://<model>?base_url=<url> (default llama3.2:3b @ 127.0.0.1:11434).
set -eu
. "$(dirname "$0")/lib.sh"
PROVIDER="${EVOL_PROVIDER:-ollama://llama3.2:3b}"
case "$PROVIDER" in
  ollama://*) ;;
  *) echo "ollama.sh: unsupported EVOL_PROVIDER '$PROVIDER' (want ollama://<model>[?base_url=<url>])" >&2; exit 64 ;;
esac
REST="${PROVIDER#ollama://}"
MODEL="${REST%%\?*}"
BASE_URL="http://127.0.0.1:11434"
case "$REST" in
  *\?*)
    QUERY="${REST#*\?}"; OLDIFS=$IFS; IFS='&'
    for kv in $QUERY; do case "$kv" in base_url=*) BASE_URL="${kv#base_url=}" ;; esac; done
    IFS=$OLDIFS ;;
esac
[ -n "$MODEL" ] || { echo "ollama.sh: no model in '$PROVIDER'" >&2; exit 64; }
PAYLOAD=$(SYSTEM="$SYSTEM" TASK="$TASK" MODEL="$MODEL" python3 -c '
import json, os
print(json.dumps({"model": os.environ["MODEL"], "stream": False, "messages": [
    {"role": "system", "content": os.environ["SYSTEM"]},
    {"role": "user", "content": os.environ["TASK"]}]}))')
printf '%s' "$PAYLOAD" | curl -sS --fail --max-time "${EVOL_OLLAMA_TIMEOUT:-120}" \
  -H 'Content-Type: application/json' -X POST "$BASE_URL/api/chat" --data-binary @- \
  | python3 -c 'import json, sys; print(json.load(sys.stdin)["message"]["content"].strip())'
