#!/usr/bin/env python3
"""Fake adapter serving every port for engine tests.

Behavior knobs (env):
  EVOL_TEST_DIR   observation dir; calls are appended to <dir>/calls.jsonl,
                  corpus records to <dir>/record.jsonl, accepted writes to
                  <dir>/written.json
  EVOL_FAKE_GOOD  "1" -> generator proposes an IMPROVED candidate (scores
                  0.9); otherwise a WORSE candidate (scores 0.4)
  EVOL_FAKE_KB    "unavailable" -> knowledgebase answers unavailable
  EVOL_FAKE_CASES N -> corpus serves N holdout cases (default 1)
  EVOL_FAKE_NOISY "1" -> scorer gives IMPROVED outputs 1.0 on even-numbered
                  cases and 0.2 on odd ones (mean improves, unstable)
  EVOL_FAKE_PENALIZE_PROV <uri> -> scorer scores any transcript produced
                  under that provider 0.1 (provider marker appended by the
                  fake executor)
"""
import json
import os
import sys

req = json.load(sys.stdin)
port, action = req["port"], req["action"]
out_dir = os.environ.get("EVOL_TEST_DIR", ".")

with open(os.path.join(out_dir, "calls.jsonl"), "a", encoding="utf-8") as f:
    f.write(json.dumps({"port": port, "action": action,
                        "env": req.get("env")}) + "\n")


def reply(payload):
    env = {"evol": req["evol"], "port": port, "action": action}
    env.update(payload)
    print(json.dumps(env))
    sys.exit(0)


if port == "artifactstore":
    if action == "list":
        # Skills only; other kinds are empty. Refs chosen so lexical
        # order differs from insertion order (selection tie-breaks).
        refs = ["skills/fake", "skills/other"] if req.get("kind") == "skill" else []
        reply({"refs": refs})
    if action == "load":
        reply({"artifact": {
            "ref": req["ref"], "kind": "skill",
            "frontmatter": "name: fake",
            "body": "baseline body", "version": "v1",
        }})
    if action == "write":
        with open(os.path.join(out_dir, "written.json"), "w", encoding="utf-8") as f:
            json.dump(req, f)
        reply({"version": "v2"})

if port == "corpus":
    if action == "cases":
        n = int(os.environ.get("EVOL_FAKE_CASES", "1"))
        reply({"cases": [{
            "id": f"case-{i}", "input": "do the thing",
            "expected": "the thing, done well",
            "split": req.get("split", "holdout"), "source": "golden",
        } for i in range(1, n + 1)]})
    if action == "tabu":
        reply({"entries": []})
    if action == "corrections":
        mode = os.environ.get("EVOL_FAKE_CORRECTIONS", "")
        if mode == "error":
            print("fake corpus: corrections exploded", file=sys.stderr)
            sys.exit(3)
        if mode == "ok":
            reply({"cases": [
                # duplicate id — engine must dedup against corpus cases
                {"id": "case-1", "input": "do the thing",
                 "expected": "the thing, done well",
                 "split": "holdout", "source": "correction"},
                # genuinely new holdout correction — must join the pool
                {"id": "corr-1", "input": "do the corrected thing",
                 "expected": "the corrected thing, done well",
                 "split": "holdout", "source": "correction"},
                # other-split correction — must be skipped for gating
                {"id": "corr-train", "input": "train-only",
                 "expected": "train-only", "split": "train",
                 "source": "correction"},
            ]})
        reply({"cases": []})
    if action == "add-cases":
        with open(os.path.join(out_dir, "added.jsonl"), "a", encoding="utf-8") as f:
            f.write(json.dumps(req) + "\n")
        dup = 1 if os.environ.get("EVOL_FAKE_DUP") == "1" else 0
        n = len(req.get("cases", [])) - dup
        reply({"added": n, "duplicates": dup,
               "ids": [c.get("id", "") for c in req.get("cases", [])[:n]]})
    if action == "promote-cases":
        reply({"promoted": len(req.get("ids", [])) - 1,
               "missing": [req.get("ids", [""])[-1]]})
    if action == "record":
        with open(os.path.join(out_dir, "record.jsonl"), "a", encoding="utf-8") as f:
            f.write(json.dumps(req) + "\n")
        reply({})
    if action == "history":
        # EVOL_FAKE_HISTORY: "" -> empty for all refs; "mixed" ->
        # skills/fake has history, others none; "error" -> adapter error.
        mode = os.environ.get("EVOL_FAKE_HISTORY", "")
        if mode == "error":
            print("fake corpus: history exploded", file=sys.stderr)
            sys.exit(3)
        if mode == "mixed" and req["artifact_ref"] == "skills/fake":
            reply({"generations": [
                {"generation": 1, "best_score": 0.45, "verdict": "rejected"},
                {"generation": 2, "best_score": 0.55, "verdict": "rejected"},
            ]})
        reply({"generations": []})

if port == "generator" and action == "synth":
    mode = os.environ.get("EVOL_FAKE_SYNTH", "ok")
    if mode == "dry":
        reply({"cases": []})
    reply({"cases": [
        {"input": "synthesized input one", "expected": "expected one",
         "rationale": "exercises rule one"},
        {"input": "synthesized input two", "expected": "expected two",
         "rationale": "exercises rule two"},
    ]})

if port == "generator":
    good = os.environ.get("EVOL_FAKE_GOOD") == "1"
    body = "IMPROVED body with sharper instructions" if good else "WORSE body"
    reply({"candidates": [{
        "id": "cand-1", "strategy": "tighten",
        "frontmatter": "name: fake", "body": body,
        "rationale": "test candidate",
    }]})

if port == "executor":
    if action == "run":
        with open(req["candidate_ref"], encoding="utf-8") as f:
            staged = f.read()
        provider = (req.get("env") or {}).get("provider", "")
        if provider:
            staged += f"\n[prov={provider}]"
        reply({"transcript": {
            "output": staged, "tool_calls": [],
            "duration_ms": 5, "exit_code": 0,
        }})

if port == "scorer":
    output = req["transcript"]["output"]
    value = 0.9 if "IMPROVED" in output else (0.5 if "baseline" in output else 0.4)
    penalized = os.environ.get("EVOL_FAKE_PENALIZE_PROV", "")
    if penalized and f"[prov={penalized}]" in output:
        value = 0.1
    elif os.environ.get("EVOL_FAKE_NOISY") == "1" and "IMPROVED" in output:
        num = int(req["case"]["id"].rsplit("-", 1)[-1])
        value = 1.0 if num % 2 == 0 else 0.2
    reply({"score": {"value": value, "reason": f"fake score for output len {len(output)}"}})

if port == "knowledgebase":
    if os.environ.get("EVOL_FAKE_KB") == "unavailable":
        reply({"unavailable": True})
    reply({"passages": [{"text": "a fact", "source": "notes/x", "score": 0.8}]})

print(f"fake adapter: unhandled {port}/{action}", file=sys.stderr)
sys.exit(3)
