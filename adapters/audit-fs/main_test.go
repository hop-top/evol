package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func call(t *testing.T, root, request string) (map[string]any, error) {
	t.Helper()
	var out bytes.Buffer
	getenv := func(k string) string {
		if k == "EVOL_AUDIT_ROOT" {
			return root
		}
		return ""
	}
	err := run(strings.NewReader(request), &out, getenv)
	if err != nil {
		return nil, err
	}
	var resp map[string]any
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("bad response %q: %v", out.String(), err)
	}
	return resp, nil
}

func recordReq(id, subject, started string) string {
	rec := map[string]any{
		"evol": "1", "port": "audit", "action": "record",
		"run": map[string]any{
			"tool": "evol", "run_id": id, "subject": subject,
			"started_at": started, "finished_at": started,
			"outcome": "no-improvement",
			"metrics": map[string]any{"best_score": 0.7},
			"steps": []map[string]any{
				{"seq": 0, "name": "baseline", "status": "ok"},
			},
		},
	}
	b, _ := json.Marshal(rec)
	return string(b)
}

func TestRecordListShowRoundTrip(t *testing.T) {
	root := t.TempDir()
	if _, err := call(t, root, recordReq("r1", "a/SKILL.md", "2026-08-20T01:00:00Z")); err != nil {
		t.Fatalf("record: %v", err)
	}
	if _, err := call(t, root, recordReq("r2", "b/SKILL.md", "2026-08-20T02:00:00Z")); err != nil {
		t.Fatalf("record: %v", err)
	}

	resp, err := call(t, root, `{"evol":"1","port":"audit","action":"list","tool":"evol"}`)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	runs := resp["runs"].([]any)
	if len(runs) != 2 {
		t.Fatalf("want 2 runs, got %d", len(runs))
	}
	first := runs[0].(map[string]any)
	if first["run_id"] != "r2" {
		t.Fatalf("newest first: want r2, got %v", first["run_id"])
	}

	resp, err = call(t, root, `{"evol":"1","port":"audit","action":"show","run_id":"r1"}`)
	if err != nil {
		t.Fatalf("show: %v", err)
	}
	shown := resp["run"].(map[string]any)
	if shown["subject"] != "a/SKILL.md" {
		t.Fatalf("show subject: %v", shown["subject"])
	}
	steps := shown["steps"].([]any)
	if len(steps) != 1 {
		t.Fatalf("show steps: %d", len(steps))
	}
}

func TestRecordUpsertsByRunID(t *testing.T) {
	root := t.TempDir()
	if _, err := call(t, root, recordReq("r1", "a/SKILL.md", "2026-08-20T01:00:00Z")); err != nil {
		t.Fatal(err)
	}
	// Same id, new subject: replaces, does not append.
	if _, err := call(t, root, recordReq("r1", "updated/SKILL.md", "2026-08-20T01:00:00Z")); err != nil {
		t.Fatal(err)
	}
	resp, err := call(t, root, `{"evol":"1","port":"audit","action":"list"}`)
	if err != nil {
		t.Fatal(err)
	}
	runs := resp["runs"].([]any)
	if len(runs) != 1 {
		t.Fatalf("upsert must not duplicate: got %d rows", len(runs))
	}
	if runs[0].(map[string]any)["subject"] != "updated/SKILL.md" {
		t.Fatalf("upsert did not replace: %v", runs[0])
	}
	data, _ := os.ReadFile(filepath.Join(root, ledgerFile)) // #nosec G304 -- test temp dir
	if got := len(bytes.Split(bytes.TrimSpace(data), []byte("\n"))); got != 1 {
		t.Fatalf("ledger file lines: %d", got)
	}
}

func TestListFiltersAndLimit(t *testing.T) {
	root := t.TempDir()
	for i, sub := range []string{"a", "b", "a"} {
		id := string(rune('x' + i))
		started := "2026-08-20T0" + string(rune('1'+i)) + ":00:00Z"
		if _, err := call(t, root, recordReq(id, sub, started)); err != nil {
			t.Fatal(err)
		}
	}
	resp, err := call(t, root, `{"evol":"1","port":"audit","action":"list","subject":"a"}`)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(resp["runs"].([]any)); got != 2 {
		t.Fatalf("subject filter: want 2, got %d", got)
	}
	resp, err = call(t, root, `{"evol":"1","port":"audit","action":"list","limit":1}`)
	if err != nil {
		t.Fatal(err)
	}
	runs := resp["runs"].([]any)
	if len(runs) != 1 {
		t.Fatalf("limit: want 1, got %d", len(runs))
	}
	if runs[0].(map[string]any)["run_id"] != "z" {
		t.Fatalf("limit keeps newest: %v", runs[0])
	}
}

func TestShowMissingIsAdapterError(t *testing.T) {
	root := t.TempDir()
	if _, err := call(t, root, `{"evol":"1","port":"audit","action":"show","run_id":"nope"}`); err == nil {
		t.Fatal("want adapter error for unknown run id")
	}
}

func TestAdapterErrors(t *testing.T) {
	root := t.TempDir()
	cases := []string{
		`not json`,
		`{"evol":"9","port":"audit","action":"list"}`,
		`{"evol":"1","port":"corpus","action":"list"}`,
		`{"evol":"1","port":"audit","action":"bogus"}`,
		`{"evol":"1","port":"audit","action":"record","run":{"tool":"evol"}}`,
		`{"evol":"1","port":"audit","action":"record","run":{"run_id":"r"}}`,
		`{"evol":"1","port":"audit","action":"show"}`,
	}
	for _, req := range cases {
		if _, err := call(t, root, req); err == nil {
			t.Errorf("want error for %q", req)
		}
	}
}

func TestMissingRootIsAdapterError(t *testing.T) {
	var out bytes.Buffer
	err := run(strings.NewReader(`{"evol":"1","port":"audit","action":"list"}`), &out,
		func(string) string { return "" })
	if err == nil {
		t.Fatal("want error when EVOL_AUDIT_ROOT unset")
	}
}
