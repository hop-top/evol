#!/bin/sh
# Seed the corpus-fs store with the e2e golden cases.
# Usage: EVOL_CORPUS_ROOT=e2e/.corpus e2e/bin/seed-corpus.sh <artifact_ref>
set -eu
REF="${1:?usage: seed-corpus.sh <artifact_ref>}"
ROOT="${EVOL_CORPUS_ROOT:?EVOL_CORPUS_ROOT not set}"
HASH=$(printf '%s' "$REF" | shasum -a 256 | cut -c1-12)
DIR="$ROOT/$HASH"
mkdir -p "$DIR"
printf '%s' "$REF" > "$DIR/ref.txt"
cp "$(dirname "$0")/../cases/cases.jsonl" "$DIR/cases.jsonl"
echo "seeded $DIR ($(wc -l < "$DIR/cases.jsonl" | tr -d ' ') cases)"
