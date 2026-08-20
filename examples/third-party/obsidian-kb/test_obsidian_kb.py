#!/usr/bin/env python3
"""Wire-level tests for the Obsidian KnowledgeBase adapter (stdlib only)."""

import json
import os
import shutil
import subprocess
import sys
import tempfile
import unittest

HERE = os.path.dirname(os.path.abspath(__file__))
ADAPTER = os.path.join(HERE, "obsidian_kb.py")
VAULT = os.path.join(HERE, "testdata", "vault")


def call(request, vault=VAULT, raw=None):
    env = dict(os.environ)
    if vault is None:
        env.pop("OBSIDIAN_VAULT", None)
    else:
        env["OBSIDIAN_VAULT"] = vault
    data = raw if raw is not None else json.dumps(request)
    proc = subprocess.run(
        [sys.executable, ADAPTER], input=data, capture_output=True,
        text=True, env=env, timeout=30,
    )
    return proc


def req(action, **fields):
    base = {"evol": "1", "port": "knowledgebase", "action": action}
    base.update(fields)
    return base


class SearchTests(unittest.TestCase):
    def test_engine_exact_query_finds_house_rules_first(self):
        # the engine sends the artifact ref verbatim as the query
        proc = call(req("search", query="commit-messages/SKILL.md", limit=5))
        self.assertEqual(proc.returncode, 0, proc.stderr)
        resp = json.loads(proc.stdout)
        self.assertEqual(resp["port"], "knowledgebase")
        self.assertTrue(resp["passages"], "expected passages")
        self.assertEqual(resp["passages"][0]["source"],
                         "Conventions/commit-messages.md")
        self.assertIn("backtick", resp["passages"][0]["text"])
        scores = [p["score"] for p in resp["passages"]]
        self.assertEqual(scores, sorted(scores, reverse=True))

    def test_limit_respected(self):
        proc = call(req("search", query="conventions commit", limit=1))
        resp = json.loads(proc.stdout)
        self.assertEqual(len(resp["passages"]), 1)

    def test_wikilinks_unwrapped_in_snippets(self):
        proc = call(req("search", query="scopes kebab", limit=3))
        resp = json.loads(proc.stdout)
        joined = " ".join(p["text"] for p in resp["passages"])
        self.assertNotIn("[[", joined)

    def test_unknown_fields_ignored(self):
        proc = call(req("search", query="commit", limit=2, future_field="x"))
        self.assertEqual(proc.returncode, 0, proc.stderr)


class BriefTests(unittest.TestCase):
    def test_brief_composes_with_sources(self):
        proc = call(req("brief", topic="commit style"))
        self.assertEqual(proc.returncode, 0, proc.stderr)
        resp = json.loads(proc.stdout)
        self.assertIn("Conventions/commit-messages.md", resp["text"])


class AppendTests(unittest.TestCase):
    def test_append_creates_inbox_note(self):
        with tempfile.TemporaryDirectory() as tmp:
            vault = os.path.join(tmp, "vault")
            shutil.copytree(VAULT, vault)
            proc = call(req("append", text="add-example beat reorder twice",
                            tags=["evolution", "commit-style"]), vault=vault)
            self.assertEqual(proc.returncode, 0, proc.stderr)
            inbox = os.path.join(vault, "Inbox", "evol.md")
            self.assertTrue(os.path.exists(inbox))
            body = open(inbox, encoding="utf-8").read()
            self.assertIn("add-example beat reorder twice", body)
            self.assertIn("#evolution", body)


class UnavailabilityTests(unittest.TestCase):
    def test_missing_vault_is_unavailable_exit_zero(self):
        proc = call(req("search", query="anything"), vault="/nonexistent/vault")
        self.assertEqual(proc.returncode, 0, proc.stderr)
        resp = json.loads(proc.stdout)
        self.assertTrue(resp.get("unavailable"))

    def test_unset_env_is_unavailable_exit_zero(self):
        proc = call(req("brief", topic="anything"), vault=None)
        self.assertEqual(proc.returncode, 0, proc.stderr)
        self.assertTrue(json.loads(proc.stdout).get("unavailable"))


class AdapterErrorTests(unittest.TestCase):
    def test_malformed_json_exits_nonzero(self):
        proc = call(None, raw="{not json")
        self.assertNotEqual(proc.returncode, 0)
        self.assertIn("malformed", proc.stderr)

    def test_wrong_port_exits_nonzero(self):
        proc = call({"evol": "1", "port": "corpus", "action": "search"})
        self.assertNotEqual(proc.returncode, 0)

    def test_unknown_action_exits_nonzero(self):
        proc = call(req("summon"))
        self.assertNotEqual(proc.returncode, 0)

    def test_missing_query_exits_nonzero(self):
        proc = call(req("search"))
        self.assertNotEqual(proc.returncode, 0)


if __name__ == "__main__":
    unittest.main(verbosity=2)
