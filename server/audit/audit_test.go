package audit

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// readRecords returns the records of the file at path.
func readRecords(t *testing.T, path string) []Record {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out []Record
	s := bufio.NewScanner(f)
	for s.Scan() {
		var r Record
		if err := json.Unmarshal(s.Bytes(), &r); err != nil {
			t.Fatalf("%q: %v", s.Text(), err)
		}
		out = append(out, r)
	}
	return out
}

// fixedNow is the time of the records of the tests.
var fixedNow = time.Date(2026, 11, 4, 14, 20, 31, 123456789, time.UTC)

// TestAuditRecordLine checks the line of a record: every field of a ban
// by a player, the time in UTC to the millisecond, the file of mode 0600,
// and the address left out without RecordAddresses.
func TestAuditRecordLine(t *testing.T) {
	for _, addresses := range []bool{true, false} {
		path := filepath.Join(t.TempDir(), "audit.log")
		l, err := Open(Config{File: path, RecordAddresses: addresses, Now: func() time.Time { return fixedNow }})
		if err != nil {
			t.Fatal(err)
		}
		l.Record(Record{
			Actor:   Actor{Kind: KindPlayer, Name: "Steve", Key: "xuid:2535498765432109", Level: 4, Address: "203.0.113.9"},
			Where:   &Where{Dimension: "overworld", Pos: [3]float64{12.5, 64, -3.2}},
			Action:  "ban",
			Command: "/ban Griefer griefing",
			Target:  "Griefer (xuid:2535412345678901)",
			Result:  ResultOK,
			Output:  []string{"Banned Griefer (xuid:2535412345678901): griefing"},
		})
		if err := l.Close(); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		address := `,"address":"203.0.113.9"`
		if !addresses {
			address = ""
		}
		want := `{"time":"2026-11-04T14:20:31.123Z","actor":{"kind":"player","name":"Steve","key":"xuid:2535498765432109","level":4` + address + `},` +
			`"where":{"dimension":"overworld","pos":[12.5,64,-3.2]},"action":"ban","command":"/ban Griefer griefing",` +
			`"target":"Griefer (xuid:2535412345678901)","result":"ok","output":["Banned Griefer (xuid:2535412345678901): griefing"]}` + "\n"
		if string(data) != want {
			t.Errorf("RecordAddresses %v:\n%s\nwant\n%s", addresses, data, want)
		}
		if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
			t.Errorf("audit.log: mode %v, %v, want 0600", info.Mode().Perm(), err)
		}
	}
}

// TestAuditActors checks how the source of a ban shows each actor.
func TestAuditActors(t *testing.T) {
	for _, tc := range []struct {
		a    Actor
		want string
	}{
		{Actor{Kind: KindConsole}, "Console"},
		{Actor{Kind: KindPlayer, Name: "Steve", Key: "xuid:2535498765432109"}, "Steve (xuid:2535498765432109)"},
		{Actor{Kind: KindPlayer, Name: "steve", Key: "steve"}, "steve"},
		{Actor{Kind: KindRemote, Token: "panel"}, "remote:panel"},
		{Actor{Kind: KindServer}, "Server"},
		{Actor{Kind: KindAPI}, "API"},
		{Actor{Kind: KindCommandBlock, Name: "@"}, "@"},
	} {
		if got := tc.a.String(); got != tc.want {
			t.Errorf("%+v: %q, want %q", tc.a, got, tc.want)
		}
	}
}

// TestAuditRotates checks that the file is rotated before it would pass
// MaxSize, as audit-<time>.log beside it, and that Keep rotated files are
// kept.
func TestAuditRotates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.log")
	clock := fixedNow
	l, err := Open(Config{File: path, MaxSize: 200, Keep: 2, Now: func() time.Time { clock = clock.Add(time.Second); return clock }})
	if err != nil {
		t.Fatal(err)
	}
	for range 10 {
		l.Record(Record{Actor: Actor{Kind: KindConsole, Level: 4}, Action: "say", Command: "/say " + strings.Repeat("x", 60), Result: ResultOK})
	}
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	rotated, _ := filepath.Glob(filepath.Join(dir, "audit-*.log"))
	if len(rotated) != 2 {
		t.Errorf("rotated files %v, want the 2 newest", rotated)
	}
	total := len(readRecords(t, path))
	for _, f := range rotated {
		total += len(readRecords(t, f))
		if info, _ := os.Stat(f); info.Size() > 200 {
			t.Errorf("%s holds %d bytes, past MaxSize", f, info.Size())
		}
	}
	if total == 0 || total > 10 {
		t.Errorf("%d records kept", total)
	}
}

// TestAuditDropsRatherThanBlocks checks that Record never waits for a
// writer that is behind: with the writer blocked and the buffer full,
// records are dropped and counted, Record returns at once, and the next
// record written is preceded by a record of the drops.
func TestAuditDropsRatherThanBlocks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.log")
	l, err := Open(Config{File: path, Buffer: 4, Now: func() time.Time { return fixedNow }})
	if err != nil {
		t.Fatal(err)
	}
	gate := make(chan struct{})
	l.gate = gate
	start := time.Now()
	for range 100 {
		l.Record(Record{Actor: Actor{Kind: KindConsole}, Action: "say", Result: ResultOK})
	}
	// A Record that waited for the writer would never return: the bound
	// is loose for a loaded machine.
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("100 records with the writer blocked took %v", d)
	}
	if n := l.Dropped(); n < 90 {
		t.Errorf("dropped %d of 100 records with room for 4 and one in hand, want 95", n)
	}
	close(gate)
	l.Record(Record{Actor: Actor{Kind: KindConsole}, Action: "last", Result: ResultOK})
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	records := readRecords(t, path)
	var dropped uint64
	for _, r := range records {
		if r.Action == "audit-dropped" {
			dropped += r.Count
		}
	}
	if dropped != l.Dropped() {
		t.Errorf("records of drops count %d, want %d", dropped, l.Dropped())
	}
	if len(records) == 0 || records[0].Action != "audit-dropped" {
		t.Errorf("records %+v: want the drops first", records)
	}
	var nilLog *Log
	nilLog.Record(Record{Action: "nothing"})
	if err := nilLog.Close(); err != nil || nilLog.Dropped() != 0 {
		t.Error("a nil Log is not a log of nothing")
	}
}

// TestAuditCapsOutput checks that a record keeps MaxOutput bytes of
// output at most.
func TestAuditCapsOutput(t *testing.T) {
	out := capOutput([]string{strings.Repeat("a", 3000), strings.Repeat("b", 3000), "c"})
	if len(out) != 2 || len(out[0])+len(out[1]) != MaxOutput+len("…") || !strings.HasSuffix(out[1], "…") {
		t.Errorf("capped output: %d lines, %d bytes", len(out), len(strings.Join(out, "")))
	}
}
