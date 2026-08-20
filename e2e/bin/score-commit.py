#!/usr/bin/env python3
"""Fallback scorer (draft scorer port) for the commit-message e2e.

Mirrors e2e/contracts/commit-subject.yaml with stdlib-only checks so the
loop runs against any environment. Swap evol.yaml's scorer cmd to the
eva-backed adapter once an eva build with standalone contract mode
(`eva run --contract ... --input -`) is installed; the port contract is
identical.
"""
import json, re, sys

def main() -> int:
    try:
        req = json.load(sys.stdin)
    except json.JSONDecodeError as e:
        print(f"bad request: {e}", file=sys.stderr); return 1
    if req.get("evol") != "1" or req.get("port") != "scorer" or req.get("action") != "score":
        print("bad envelope", file=sys.stderr); return 1
    out = (req.get("transcript") or {}).get("output") or ""
    subject = out.strip().splitlines()[0] if out.strip() else ""
    checks = [
        ("subject_shape", bool(re.match(
            r"^(feat|fix|refactor|build|ci|chore|docs|style|perf|test)(\([a-z0-9-]+\))?!?: [a-z]", subject))),
        ("no_trailing_period", bool(subject) and not subject.endswith(".")),
        ("subject_length", 0 < len(subject) <= 72),
        ("word_count", 0 < len(out.split()) <= 120),
        ("no_attribution", not re.search(
            r"\b(generated|co-authored|ai|assistant|claude)\b", out, re.I)),
    ]
    passed = [n for n, ok in checks if ok]
    failed = [n for n, ok in checks if not ok]
    value = len(passed) / len(checks)
    reason = "all checks passed" if not failed else "failed: " + ", ".join(failed)
    json.dump({"evol": "1", "port": "scorer", "action": "score",
               "score": {"value": value, "reason": reason}}, sys.stdout)
    return 0

if __name__ == "__main__":
    sys.exit(main())
