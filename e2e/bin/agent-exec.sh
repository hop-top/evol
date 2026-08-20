#!/bin/sh
# Executor target: run the agent under test with a candidate skill.
# Usage: agent-exec.sh <candidate_ref> <case input...>
# The engine substitutes {candidate_ref} and {input} into EVOL_EXEC_CMD.
set -eu
CANDIDATE="$1"; shift
INPUT="$*"
SKILL_BODY=$(cat "$CANDIDATE")
MODEL="${EVOL_AGENT_MODEL:-haiku}"
exec claude -p \
  --model "$MODEL" \
  --append-system-prompt "You have the following skill loaded. Apply it exactly.

$SKILL_BODY" \
  "Write the commit message for this change. Output ONLY the commit message (subject line, optionally a blank line and body). Change: $INPUT"
