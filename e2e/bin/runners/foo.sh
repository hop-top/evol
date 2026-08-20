#!/bin/sh
# Runner shim: foo CLI (llm-pipe class, kit/llm-backed).
# System injection: native (hermetic pattern under a temp XDG_CONFIG_HOME —
# the user's ~/.config/foo is never touched).
# Provider: EVOL_PROVIDER becomes a hermetic pool entry in $XDG/hop/llm.yaml
# selected via -m <alias>.
# KNOWN LIMITATION (foo docs, configure-models.md): current foo builds have
# no base_url support — local models (ollama/lmstudio) are not reachable yet;
# hosted schemes need their provider key exported. The pool entry written
# here already carries base_url so this shim starts working the moment foo
# ships local-model support.
set -eu
. "$(dirname "$0")/lib.sh"
PROVIDER="${EVOL_PROVIDER:-ollama://llama3.2:3b}"
SCHEME="${PROVIDER%%://*}"
REST="${PROVIDER#*://}"
MODEL="${REST%%\?*}"
BASE_URL=""
case "$REST" in
  *\?*)
    QUERY="${REST#*\?}"; OLDIFS=$IFS; IFS='&'
    for kv in $QUERY; do case "$kv" in base_url=*) BASE_URL="${kv#base_url=}" ;; esac; done
    IFS=$OLDIFS ;;
esac
[ -n "$MODEL" ] || { echo "foo.sh: no model in EVOL_PROVIDER '$PROVIDER'" >&2; exit 64; }

XDG=$(mktemp -d); trap 'rm -rf "$XDG"' EXIT
mkdir -p "$XDG/foo/patterns/evol-candidate" "$XDG/hop" "$XDG/data"
printf '%s' "$SYSTEM" > "$XDG/foo/patterns/evol-candidate/system.md"
printf 'model: evol-under-test\npatterns_path: %s/foo/patterns\n' "$XDG" > "$XDG/foo/config.yaml"
{
  printf 'pool:\n  - alias: evol-under-test\n    scheme: %s\n    model: %s\n' "$SCHEME" "$MODEL"
  [ -n "$BASE_URL" ] && printf '    base_url: %s\n' "$BASE_URL"
} > "$XDG/hop/llm.yaml"

XDG_CONFIG_HOME="$XDG" XDG_DATA_HOME="$XDG/data" \
  exec foo -p evol-candidate -m evol-under-test \
    --no-hints --no-color --no-stream --format text "$TASK"
