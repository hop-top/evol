# Shared shim plumbing for the reference runner contract
# (spec/port-executor.md): stdin = case input; EVOL_CANDIDATE_REF =
# candidate body path; EVOL_PROVIDER = optional provider URI; stdout =
# agent output only. Argv fallback for manual use:
#   <runner>.sh <candidate_ref> <input...>
# Sourced by every runner; sets CANDIDATE, INPUT, SYSTEM, TASK.

if [ -n "${EVOL_CANDIDATE_REF:-}" ]; then
  CANDIDATE="$EVOL_CANDIDATE_REF"
else
  CANDIDATE="${1:?candidate ref required (EVOL_CANDIDATE_REF or first arg)}"
  shift
fi
if [ $# -gt 0 ]; then
  INPUT="$*"
else
  INPUT=$(cat)
fi
if [ -z "$INPUT" ]; then
  echo "runner: empty case input" >&2
  exit 65
fi

SKILL_BODY=$(cat "$CANDIDATE")

SYSTEM="You have the following skill loaded. Apply it exactly.

$SKILL_BODY

Output ONLY the commit message (subject line, optionally a blank line and body). No preamble, no quotes, no code fences."

TASK="Write the commit message for this change. Change: $INPUT"
