package main

import "testing"

// timedFind carries three objects: one with only created_at, one with a
// newer updated_at (the expected answer), and one with no timestamps.
const timedFind = `{"objects":[` +
	`{"id":"obj-1","text_content":"older","created_at":"2026-08-18T10:00:00Z"},` +
	`{"id":"obj-2","text_content":"newer","created_at":"2026-08-19T00:00:00Z","updated_at":"2026-08-20T02:08:22-04:00"},` +
	`{"id":"obj-3","text_content":"undated"}]}`

func TestNewestReturnsMaxTimestamp(t *testing.T) {
	fake := writeFake(t, healthyStatus+`
if [ "$1" = "find" ]; then echo '`+timedFind+`'; exit 0; fi
exit 64`)
	res := invoke(t, fake, `{"evol":"1","port":"knowledgebase","action":"newest","query":"skills/commit-style"}`)
	if res.exitCode != 0 {
		t.Fatalf("exit %d, stderr: %s", res.exitCode, res.stderr)
	}
	m := decode(t, res.stdout)
	if m["unavailable"] == true {
		t.Fatal("unexpected unavailable")
	}
	// updated_at beats every created_at; value is passed through verbatim.
	if m["ts"] != "2026-08-20T02:08:22-04:00" {
		t.Fatalf("ts = %v, want the newest updated_at", m["ts"])
	}
}

func TestNewestWithoutTimestampsOmitsTS(t *testing.T) {
	fake := writeFake(t, healthyStatus+`
if [ "$1" = "find" ]; then echo '{"objects":[{"id":"obj-1","text_content":"undated"}]}'; exit 0; fi
exit 64`)
	res := invoke(t, fake, `{"evol":"1","port":"knowledgebase","action":"newest","query":"anything"}`)
	if res.exitCode != 0 {
		t.Fatalf("exit %d, stderr: %s", res.exitCode, res.stderr)
	}
	m := decode(t, res.stdout)
	if m["unavailable"] == true {
		t.Fatal("no-timestamp knowledge is data, not unavailability")
	}
	if _, present := m["ts"]; present {
		t.Fatalf("ts should be omitted when nothing is timestamped, got %v", m["ts"])
	}
}

func TestNewestDaemonDownIsUnavailable(t *testing.T) {
	fake := writeFake(t, `exit 7`)
	res := invoke(t, fake, `{"evol":"1","port":"knowledgebase","action":"newest","query":"anything"}`)
	if res.exitCode != 0 {
		t.Fatalf("exit %d, want 0 (unavailability is data)", res.exitCode)
	}
	if m := decode(t, res.stdout); m["unavailable"] != true {
		t.Fatalf("want unavailable, got %v", m)
	}
}

func TestNewestRequiresQuery(t *testing.T) {
	fake := writeFake(t, healthyStatus)
	res := invoke(t, fake, `{"evol":"1","port":"knowledgebase","action":"newest"}`)
	if res.exitCode == 0 {
		t.Fatal("missing query must be an adapter fault")
	}
}
