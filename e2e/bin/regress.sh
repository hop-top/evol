#!/usr/bin/env bash
# Regression gate: replay committed cassettes against the CURRENT artifact
# and fail on drift. Zero agent calls in replay mode — CI needs no keys.
#
# Usage:
#   e2e/bin/regress.sh              # CI mode: replay e2e/fixtures/cassettes,
#                                   #   compare scores vs e2e/regress-baseline.json
#   e2e/bin/regress.sh --update     # promotion mode: copy runtime cassettes
#                                   #   (e2e/cassettes) into fixtures, verify all
#                                   #   holdout cases replay, write the baseline
#   e2e/bin/regress.sh --allow-miss # tolerate cassette misses (bootstrap aid)
#
# Exit: 0 pass or clean bootstrap-skip · 1 miss/drift/config failure.
#
# Env overrides:
#   REGRESS_FIXTURES  committed cassette dir   (default e2e/fixtures/cassettes)
#   REGRESS_BASELINE  committed score snapshot (default e2e/regress-baseline.json)
#   REGRESS_RUNTIME   runtime cassette dir     (default e2e/cassettes; --update source)
#   REGRESS_EPSILON   allowed score drop       (default 0.01)
#   EVOL_PROVIDER     provider URI             (default: executor_provider in e2e/evol.yaml)
#   REGRESS_RUNNER    runner shim              (default e2e/bin/runners/claude.sh)
#   REGRESS_ARTIFACT  artifact under test      (default e2e/artifacts/commit-messages/SKILL.md)
#   REGRESS_CASES     cases file               (default e2e/cases/cases.jsonl)
set -euo pipefail

cd "$(dirname "$0")/../.."   # repo root

UPDATE=0
ALLOW_MISS=0
for arg in "$@"; do
  case "$arg" in
    --update) UPDATE=1 ;;
    --allow-miss) ALLOW_MISS=1 ;;
    *) echo "regress.sh: unknown flag '$arg'" >&2; exit 1 ;;
  esac
done

FIXTURES="${REGRESS_FIXTURES:-e2e/fixtures/cassettes}"
BASELINE="${REGRESS_BASELINE:-e2e/regress-baseline.json}"
RUNTIME="${REGRESS_RUNTIME:-e2e/cassettes}"
RUNNER="${REGRESS_RUNNER:-e2e/bin/runners/claude.sh}"
ARTIFACT="${REGRESS_ARTIFACT:-e2e/artifacts/commit-messages/SKILL.md}"
CASES="${REGRESS_CASES:-e2e/cases/cases.jsonl}"
EPSILON="${REGRESS_EPSILON:-0.01}"
SHIM="e2e/bin/runner-xrr"

# Provider must match what the recordings were made with (fingerprint input).
if [ -z "${EVOL_PROVIDER:-}" ]; then
  EVOL_PROVIDER="$(sed -n 's/^executor_provider:[[:space:]]*//p' e2e/evol.yaml | head -1)"
  EVOL_PROVIDER="${EVOL_PROVIDER:-claude://haiku}"
fi
export EVOL_PROVIDER

# --- bootstrap detection (CI stays green before first promotion) -----------
if [ "$UPDATE" -eq 1 ]; then
  if [ ! -d "$RUNTIME" ] || [ -z "$(ls -A "$RUNTIME" 2>/dev/null)" ]; then
    echo "SKIP (bootstrap): no runtime cassettes at $RUNTIME — run a recorded evolution run first (XRR_MODE=record)."
    exit 0
  fi
else
  if [ ! -d "$FIXTURES" ] || [ -z "$(ls -A "$FIXTURES" 2>/dev/null)" ] || [ ! -f "$BASELINE" ]; then
    echo "SKIP (bootstrap): no committed fixtures ($FIXTURES) and/or baseline ($BASELINE) yet."
    echo "Arm the gate after a promotion: e2e/bin/regress.sh --update && git add e2e/fixtures e2e/regress-baseline.json"
    exit 0
  fi
fi

# --- shim binary -----------------------------------------------------------
if [ ! -x "$SHIM" ]; then
  if command -v go >/dev/null 2>&1; then
    echo "building $SHIM"
    go build -buildvcs=false -o "$SHIM" ./adapters/runner-xrr
  else
    echo "regress.sh: $SHIM missing and no Go toolchain to build it" >&2
    exit 1
  fi
fi

if [ "$UPDATE" -eq 1 ]; then
  mkdir -p "$FIXTURES"
  # full copy; cassettes are small text files and pruning would need read-tracing
  rm -rf "$FIXTURES"
  mkdir -p "$FIXTURES"
  cp "$RUNTIME"/*.yaml "$FIXTURES"/ 2>/dev/null || true
  echo "copied $(ls "$FIXTURES" | wc -l | tr -d ' ') cassette files -> $FIXTURES"
fi

# --- replay + score + compare (python for JSON/JSONL sanity) ---------------
UPDATE="$UPDATE" ALLOW_MISS="$ALLOW_MISS" FIXTURES="$FIXTURES" \
BASELINE="$BASELINE" RUNNER="$RUNNER" ARTIFACT="$ARTIFACT" CASES="$CASES" \
EPSILON="$EPSILON" SHIM="$SHIM" python3 - <<'PY'
import hashlib, json, os, subprocess, sys, tempfile

update     = os.environ["UPDATE"] == "1"
allow_miss = os.environ["ALLOW_MISS"] == "1"
fixtures   = os.environ["FIXTURES"]
baseline_p = os.environ["BASELINE"]
runner     = os.environ["RUNNER"]
artifact   = os.environ["ARTIFACT"]
cases_p    = os.environ["CASES"]
epsilon    = float(os.environ["EPSILON"])
shim       = os.environ["SHIM"]

holdout = []
with open(cases_p) as f:
    for line in f:
        line = line.strip()
        if not line:
            continue
        c = json.loads(line)
        if c.get("split") == "holdout":
            holdout.append(c)
if not holdout:
    print("regress: no holdout cases in", cases_p, file=sys.stderr)
    sys.exit(1)

# The engine never hands runners the source file — it stages a reassembled
# document (fs-artifact splitFrontmatter -> engine stage). Cassette identity
# is the STAGED content hash, so regress must stage identically:
#   split : first "---\n" fence; fm keeps its trailing newline; body starts
#           right after the closing fence line's newline
#   stage : "---\n" + fm.rstrip("\n") + "\n---\n\n" + body   (fm non-empty)
raw = open(artifact, encoding="utf-8").read()
fm, body = "", raw
if raw.startswith("---\n"):
    rest = raw[4:]
    idx = rest.find("\n---")
    if idx >= 0:
        fm = rest[:idx + 1]
        after = rest[idx + 1:]
        nl = after.find("\n")
        body = after[nl + 1:] if nl >= 0 else ""
doc = body if not fm.strip() else "---\n" + fm.rstrip("\n") + "\n---\n\n" + body
staged = tempfile.NamedTemporaryFile("w", suffix=".md", delete=False,
                                     encoding="utf-8")
staged.write(doc); staged.close()
print(f"staged artifact: content hash {hashlib.sha256(doc.encode()).hexdigest()[:12]}")

env = dict(os.environ,
           XRR_MODE="replay",
           XRR_CASSETTE_DIR=fixtures,
           EVOL_CANDIDATE_REF=staged.name)

def replay(case_input: str):
    """runner-xrr replay; returns (status, output). status: ok|miss|fail."""
    # fingerprint stdin must match the engine's byte-for-byte; the engine
    # sends case.input verbatim — try exact, then with trailing newline.
    for payload in (case_input, case_input + "\n"):
        p = subprocess.run([shim, runner], input=payload, env=env,
                           capture_output=True, text=True, timeout=120)
        if p.returncode == 0:
            return "ok", p.stdout
        if p.returncode != 21:
            print(f"  shim exit {p.returncode}: {p.stderr.strip()[:200]}",
                  file=sys.stderr)
            return "fail", ""
    return "miss", ""

def score(case_id: str, output: str) -> float:
    req = {"evol": "1", "port": "scorer", "action": "score",
           "case": {"id": case_id}, "transcript": {"output": output}}
    p = subprocess.run(["python3", "e2e/bin/score-commit.py"],
                       input=json.dumps(req), env=dict(os.environ,
                       EVOL_CASES_FILE=cases_p),
                       capture_output=True, text=True, timeout=30)
    if p.returncode != 0:
        raise RuntimeError(f"scorer failed for {case_id}: {p.stderr.strip()[:200]}")
    return float(json.loads(p.stdout)["score"]["value"])

results, misses, fails = {}, [], []
for c in holdout:
    status, out = replay(c["input"])
    if status == "miss":
        misses.append(c["id"]); print(f"  {c['id']}: MISS (no recording for current artifact)")
        continue
    if status == "fail":
        fails.append(c["id"]); print(f"  {c['id']}: REPLAY FAILURE")
        continue
    s = score(c["id"], out)
    results[c["id"]] = s
    print(f"  {c['id']}: replayed, score {s:.4f}")

os.unlink(staged.name)

if fails:
    print(f"regress: {len(fails)} replay failure(s): {', '.join(fails)}", file=sys.stderr)
    sys.exit(1)
if misses and not allow_miss:
    print(f"regress: {len(misses)} cassette miss(es): {', '.join(misses)}", file=sys.stderr)
    print("  the artifact changed since recording (or fixtures are stale).", file=sys.stderr)
    print("  after an intentional promotion: e2e/bin/regress.sh --update", file=sys.stderr)
    sys.exit(1)

if update:
    with open(baseline_p, "w") as f:
        json.dump({"artifact": artifact, "provider": os.environ.get("EVOL_PROVIDER", ""),
                   "epsilon": epsilon, "scores": results}, f, indent=2, sort_keys=True)
        f.write("\n")
    print(f"baseline written: {baseline_p} ({len(results)} case(s))")
    sys.exit(0)

with open(baseline_p) as f:
    base = json.load(f)["scores"]
drifted = []
for cid, s in results.items():
    b = base.get(cid)
    if b is None:
        drifted.append((cid, "no baseline entry", s)); continue
    if s < b - epsilon:
        drifted.append((cid, f"{b:.4f} -> {s:.4f}", s))
for cid in base:
    if cid not in results and cid not in misses:
        drifted.append((cid, "baseline case not replayed", None))

if drifted:
    print("regress: score drift detected:", file=sys.stderr)
    for cid, why, _ in drifted:
        print(f"  {cid}: {why}", file=sys.stderr)
    sys.exit(1)
print(f"regress: PASS — {len(results)} case(s), no drift beyond {epsilon}")
PY
