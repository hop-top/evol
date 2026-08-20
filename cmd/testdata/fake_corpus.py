#!/usr/bin/env python3
"""Fake corpus adapter for cmd review-verb tests.

Reads one request from stdin, appends it to $FAKE_REQ_LOG (JSONL),
answers canned responses per action.
"""
import json, os, sys

req = json.load(sys.stdin)
log = os.environ.get("FAKE_REQ_LOG")
if log:
    with open(log, "a", encoding="utf-8") as f:
        f.write(json.dumps(req) + "\n")

action = req.get("action")
env = {"evol": "1", "port": "corpus", "action": action}

if action == "cases":
    cases = [{"id": "c1", "input": "golden input", "expected": "x",
              "split": "train", "source": "golden"}]
    if req.get("include_quarantined"):
        cases.append({"id": "q1", "input": "synthetic intake", "expected": "y",
                      "split": "train", "source": "synthetic", "quarantined": True})
    print(json.dumps({**env, "cases": cases}))
elif action == "add-corrections":
    ids = [c["id"] for c in req.get("corrections", [])]
    print(json.dumps({**env, "added": len(ids), "duplicates": 0, "ids": ids}))
else:
    print(json.dumps({**env, "error": "unsupported"}))
    sys.exit(1)
