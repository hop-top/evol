#!/usr/bin/env python3
"""Fallback scorer (draft scorer port) for the commit-message e2e.

Case-aware: each case in e2e/cases/cases.jsonl carries a "checks" field
naming the HOUSE rules that apply to it (schema extension; the engine's
Case struct does not forward unknown fields yet, so this scorer
side-loads the cases file and joins on case id — env EVOL_CASES_FILE,
default e2e/cases/cases.jsonl). Score = fraction of applicable checks
passed: 2 universal checks + the case's house checks.

House rules (what the skill under evolution must teach; a capable model
passes the universal checks without any skill, so only house rules
discriminate):
  backtick_identifiers  code identifier in the subject wrapped in `backticks`
  telegraphese_body     body present; <=3 non-trailer lines; no line opening
                        with an article (The/A/An); no first person
  breaking_bang         '!' before ':' in the subject
  breaking_trailer      'BREAKING CHANGE:' trailer line present
  type_docs|type_ci|type_build  exact type token for edge-case changes
  kebab_scope           scope present and kebab-case

Swap to the eva-backed adapter once an eva build with standalone
contract mode is installed (per-case checks stay scorer-side either way;
a static contract cannot express them).
"""
import json, os, re, sys

ARTICLE_RE = re.compile(r"^(the|a|an)\b", re.I)
FIRST_PERSON_RE = re.compile(r"\b(I|we|our|my|us|me)\b")
SUBJECT_RE = re.compile(
    r"^(feat|fix|refactor|build|ci|chore|docs|style|perf|test)"
    r"(\([a-z0-9-]+\))?!?: [a-z`]")
SCOPE_RE = re.compile(r"^[a-z]+\((?P<scope>[^)]*)\)!?:")
KEBAB_RE = re.compile(r"^[a-z0-9]+(-[a-z0-9]+)*$")
BACKTICK_RE = re.compile(r"`[^`\s][^`]*`")


SUBJECT_FIND_RE = re.compile(
    r"^(feat|fix|refactor|build|ci|chore|docs|style|perf|test)(\([^)]*\))?!?:")


def split_message(out: str):
    """Extract (subject, body, trailers, preamble_free).

    The subject is the FIRST line shaped like a conventional commit;
    narration before it is tolerated by extraction (models sometimes
    preface output despite instructions); discipline is the runner
    prompt's job, skill quality is what's scored."""
    lines = [l for l in out.strip().splitlines()]
    subj_idx = next((i for i, l in enumerate(lines)
                     if SUBJECT_FIND_RE.match(l.strip())), None)
    if subj_idx is None:
        return "", [], [], False
    subject = lines[subj_idx].strip()
    first_nonempty = next((l.strip() for l in lines if l.strip()), "")
    preamble_free = first_nonempty == subject
    rest = lines[subj_idx + 1:]
    trailers = [l for l in rest if l.strip().startswith("BREAKING CHANGE:")]
    body = [l for l in rest if l.strip() and not l.strip().startswith("BREAKING CHANGE:")]
    return subject, body, trailers, preamble_free


def type_token(subject: str) -> str:
    m = re.match(r"^([a-z]+)", subject)
    return m.group(1) if m else ""


def house_checks(name, subject, body, trailers, out):
    if name == "backtick_identifiers":
        return bool(BACKTICK_RE.search(subject))
    if name == "telegraphese_body":
        if not body or len(body) > 3:
            return False
        for l in body:
            t = l.strip()
            if ARTICLE_RE.match(t) or FIRST_PERSON_RE.search(t):
                return False
        return True
    if name == "breaking_bang":
        return bool(re.match(r"^[a-z]+(\([a-z0-9-]+\))?!:", subject))
    if name == "breaking_trailer":
        return bool(trailers)
    if name in ("type_docs", "type_ci", "type_build"):
        return type_token(subject) == name.split("_", 1)[1]
    if name == "kebab_scope":
        m = SCOPE_RE.match(subject)
        return bool(m and KEBAB_RE.match(m.group("scope")))
    return False  # unknown check name counts as failed, loudly below


def load_checks_index():
    path = os.environ.get("EVOL_CASES_FILE", "e2e/cases/cases.jsonl")
    idx = {}
    try:
        with open(path, encoding="utf-8") as f:
            for line in f:
                line = line.strip()
                if not line:
                    continue
                row = json.loads(line)
                idx[row.get("id", "")] = row.get("checks", [])
    except OSError as e:
        print(f"cases file unavailable ({e}); house checks skipped", file=sys.stderr)
    return idx


def main() -> int:
    try:
        req = json.load(sys.stdin)
    except json.JSONDecodeError as e:
        print(f"bad request: {e}", file=sys.stderr)
        return 1
    if req.get("evol") != "1" or req.get("port") != "scorer" or req.get("action") != "score":
        print("bad envelope", file=sys.stderr)
        return 1

    out = (req.get("transcript") or {}).get("output") or ""
    case_id = (req.get("case") or {}).get("id") or ""
    subject, body, trailers, preamble_free = split_message(out)

    checks = [
        ("subject_shape", bool(SUBJECT_RE.match(subject))),
        ("no_attribution", not re.search(
            r"\b(generated|co-authored|ai|assistant|claude)\b", subject + "\n" + "\n".join(body), re.I)),
    ]
    for name in load_checks_index().get(case_id, []):
        checks.append((name, house_checks(name, subject, body, trailers, out)))

    passed = [n for n, ok in checks if ok]
    failed = [n for n, ok in checks if not ok]
    value = len(passed) / len(checks) if checks else 0.0
    reason = "all checks passed" if not failed else "failed: " + ", ".join(failed)
    json.dump({"evol": "1", "port": "scorer", "action": "score",
               "score": {"value": round(value, 4), "reason": reason}}, sys.stdout)
    return 0


if __name__ == "__main__":
    sys.exit(main())
