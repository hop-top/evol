#!/usr/bin/env python3
"""Obsidian-vault adapter for the evol KnowledgeBase port.

Third-party-style implementation: pure Python 3 stdlib, no evol code,
written against spec/port-knowledgebase.md + spec/README.md alone.

Wire contract: one JSON request on stdin, one JSON response on stdout,
stderr for diagnostics. Exit 0 = valid response (including
{"unavailable": true}); non-zero = adapter error.

Vault root comes from OBSIDIAN_VAULT. A missing/unset vault is treated
as the knowledge base being "simply not configured" -> unavailable, not
an adapter error (see README: the spec leaves this ambiguous).
"""

import json
import math
import os
import re
import sys

DEFAULT_LIMIT = 5
SNIPPET_RADIUS = 260  # chars around the best-matching region

_WIKILINK = re.compile(r"\[\[([^\]|]+)(?:\|([^\]]+))?\]\]")
_TOKEN = re.compile(r"[a-z0-9]+")


def fail(msg):
    sys.stderr.write("obsidian-kb: %s\n" % msg)
    sys.exit(1)


def envelope(action, extra=None):
    resp = {"evol": "1", "port": "knowledgebase", "action": action}
    if extra:
        resp.update(extra)
    return resp


def emit(resp):
    sys.stdout.write(json.dumps(resp, ensure_ascii=False))
    sys.stdout.write("\n")
    sys.exit(0)


def tokenize(text):
    return _TOKEN.findall(text.lower())


def strip_frontmatter(text):
    if text.startswith("---"):
        end = text.find("\n---", 3)
        if end != -1:
            return text[end + 4:]
    return text


def unwikilink(text):
    return _WIKILINK.sub(lambda m: m.group(2) or m.group(1), text)


def vault_notes(root):
    for dirpath, dirnames, filenames in os.walk(root):
        dirnames[:] = [d for d in dirnames if not d.startswith(".")]
        for name in filenames:
            if name.endswith(".md"):
                path = os.path.join(dirpath, name)
                rel = os.path.relpath(path, root)
                try:
                    with open(path, encoding="utf-8", errors="replace") as fh:
                        yield rel, fh.read()
                except OSError as exc:
                    sys.stderr.write("obsidian-kb: skip %s: %s\n" % (rel, exc))


def score_note(tokens, rel, body):
    body_tokens = tokenize(body)
    if not body_tokens:
        return 0.0
    counts = {}
    for t in body_tokens:
        counts[t] = counts.get(t, 0) + 1
    hits = sum(counts.get(t, 0) for t in tokens)
    name_tokens = tokenize(os.path.splitext(os.path.basename(rel))[0])
    name_hits = sum(2 for t in tokens if t in name_tokens)
    if hits == 0 and name_hits == 0:
        return 0.0
    # log-dampened length normalization: raw tf/len lets a 10-token note
    # with one hit outrank the authoritative long note (found by test)
    return hits / math.log2(8 + len(body_tokens)) + name_hits


def snippet(body, tokens):
    lower = body.lower()
    best = -1
    for t in tokens:
        pos = lower.find(t)
        if pos != -1 and (best == -1 or pos < best):
            best = pos
    if best == -1:
        best = 0
    start = max(0, best - SNIPPET_RADIUS // 4)
    end = min(len(body), start + SNIPPET_RADIUS)
    text = body[start:end].strip()
    return unwikilink(text)


def do_search(root, req):
    query = req.get("query")
    if not isinstance(query, str) or not query.strip():
        fail("search: 'query' (non-empty string) is required")
    limit = req.get("limit", DEFAULT_LIMIT)
    if not isinstance(limit, int) or limit <= 0:
        limit = DEFAULT_LIMIT
    tokens = tokenize(query)
    scored = []
    for rel, raw in vault_notes(root):
        body = strip_frontmatter(raw)
        s = score_note(tokens, rel, body)
        if s > 0:
            scored.append((s, rel, body))
    scored.sort(key=lambda item: (-item[0], item[1]))
    top = scored[:limit]
    peak = top[0][0] if top else 1.0
    passages = [
        {"text": snippet(body, tokens),
         "source": rel.replace(os.sep, "/"),
         "score": round(s / peak, 4)}
        for s, rel, body in top
    ]
    return envelope("search", {"passages": passages})


def do_brief(root, req):
    topic = req.get("topic")
    if not isinstance(topic, str) or not topic.strip():
        fail("brief: 'topic' (non-empty string) is required")
    search = do_search(root, {"query": topic, "limit": 3})
    parts = []
    for p in search["passages"]:
        parts.append("%s:\n%s" % (p["source"], p["text"]))
    if not parts:
        text = "No vault notes matched topic: %s" % topic
    else:
        text = "\n\n".join(parts)
    return envelope("brief", {"text": text})


def do_append(root, req):
    text = req.get("text")
    if not isinstance(text, str) or not text.strip():
        fail("append: 'text' (non-empty string) is required")
    tags = req.get("tags", [])
    if not isinstance(tags, list):
        fail("append: 'tags' must be an array of strings")
    hashtags = " ".join(
        "#" + re.sub(r"[^A-Za-z0-9/_-]", "-", str(t)) for t in tags if str(t).strip()
    )
    inbox = os.path.join(root, "Inbox", "evol.md")
    os.makedirs(os.path.dirname(inbox), exist_ok=True)
    entry = "\n---\n%s\n" % text.rstrip()
    if hashtags:
        entry += hashtags + "\n"
    with open(inbox, "a", encoding="utf-8") as fh:
        fh.write(entry)
    return envelope("append")


def main():
    raw = sys.stdin.read()
    try:
        req = json.loads(raw)
    except ValueError as exc:
        fail("malformed request JSON: %s" % exc)
    if not isinstance(req, dict):
        fail("request must be a JSON object")
    if req.get("evol") != "1":
        fail("unsupported contract version: %r" % req.get("evol"))
    if req.get("port") != "knowledgebase":
        fail("wrong port: %r" % req.get("port"))
    action = req.get("action")
    if action not in ("search", "brief", "append"):
        fail("unknown action: %r" % action)

    root = os.environ.get("OBSIDIAN_VAULT", "")
    if not root or not os.path.isdir(root):
        sys.stderr.write("obsidian-kb: vault not configured or missing (%r)\n" % root)
        emit(envelope(action, {"unavailable": True}))

    if action == "search":
        emit(do_search(root, req))
    elif action == "brief":
        emit(do_brief(root, req))
    else:
        emit(do_append(root, req))


if __name__ == "__main__":
    main()
