#!/bin/sh
# Calibration harness: score a skill over the holdout cases with real
# runner calls. Usage: e2e/bin/calibrate.sh <skill-file> [provider] [trials]
# Prints per-case-per-trial scores and the overall mean. Run from repo root.
set -eu
SKILL="${1:?skill file required}"
export EVOL_PROVIDER="${2:-claude://haiku}"
TRIALS="${3:-1}"
CASES="${EVOL_CASES_FILE:-e2e/cases/cases.jsonl}"
export EVOL_CASES_FILE="$CASES"
total=0; n=0
while IFS= read -r row; do
  split=$(printf '%s' "$row" | python3 -c "import sys,json;print(json.load(sys.stdin).get('split',''))")
  [ "$split" = "holdout" ] || continue
  id=$(printf '%s' "$row" | python3 -c "import sys,json;print(json.load(sys.stdin)['id'])")
  input=$(printf '%s' "$row" | python3 -c "import sys,json;print(json.load(sys.stdin)['input'])")
  t=0
  while [ $t -lt $TRIALS ]; do
    t=$((t + 1))
    out=$(printf '%s' "$input" | EVOL_CANDIDATE_REF="$SKILL" e2e/bin/runners/claude.sh)
  score=$(python3 -c "
import json,sys
req={'evol':'1','port':'scorer','action':'score','case':{'id':'$id'},'transcript':{'output':sys.stdin.read()}}
print(json.dumps(req))" <<EOF2 | e2e/bin/score-commit.py | python3 -c "import sys,json;r=json.load(sys.stdin);print(r['score']['value'],'|',r['score']['reason'])"
$out
EOF2
)
    echo "$id/t$t: $score"
    v=$(printf '%s' "$score" | cut -d' ' -f1)
    total=$(python3 -c "print($total + $v)")
    n=$((n + 1))
  done
done < "$CASES"
echo "mean over $n holdout cases: $(python3 -c "print(round($total / $n, 4))")"
