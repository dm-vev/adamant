package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestHelpers checks the messages of the helpers and the values they take.
func TestHelpers(t *testing.T) {
	var p Problems
	checks := []struct {
		ok   bool
		want string
	}{
		{Range(&p, "A.Int", 5, 1, 5), ""},
		{Range(&p, "A.Int", 0, 1, 5), "A.Int = 0: want 1 to 5"},
		{AtLeast(&p, "A.Count", -1, 0), "A.Count = -1: want 0 or more"},
		{AtMost(&p, "A.Count", 9, 8), "A.Count = 9: want 8 or less"},
		{Range(&p, "A.Wait", 500*time.Millisecond, time.Second, time.Minute), "A.Wait = 500ms: want 1s to 1m0s"},
		{OneOf(&p, "A.Mode", " Fast ", "fast", "safe"), ""},
		{OneOf(&p, "A.Mode", "", "fast", "safe"), ""},
		{OneOf(&p, "A.Mode", "quick", "fast", "safe", "off"), `A.Mode = "quick": want "fast", "safe" or "off"`},
		{OneOf(&p, "A.Mode", "quick", "fast"), `A.Mode = "quick": want "fast"`},
		{Address(&p, "A.Addr", "udp", "[::]:19132"), ""},
		{Address(&p, "A.Addr", "tcp", "host:port"), `A.Addr = "host:port": want a host and a port of 0 to 65535, such as ":19132" or "127.0.0.1:19132"`},
	}
	if d, ok := Duration(&p, "A.Every", " 90s "); !ok || d != 90*time.Second {
		t.Errorf("Duration of \" 90s \" = %v, %v", d, ok)
	}
	if d, ok := Duration(&p, "A.Every", ""); !ok || d != 0 {
		t.Errorf("Duration of \"\" = %v, %v", d, ok)
	}
	_, ok := Duration(&p, "A.Every", "soon")
	checks = append(checks, struct {
		ok   bool
		want string
	}{ok, `A.Every = "soon": want a duration such as "90s" or "5m"`})

	var want []string
	for i, c := range checks {
		if c.ok != (c.want == "") {
			t.Errorf("check %d: passed %v, want %v", i, c.ok, c.want == "")
		}
		if c.want != "" {
			want = append(want, c.want)
		}
	}
	got := p.List()
	if len(got) != len(want) {
		t.Fatalf("%d problems %v, want %d", len(got), got, len(want))
	}
	for i := range got {
		if got[i].Message != want[i] || got[i].Warning {
			t.Errorf("problem %d: %+v, want the error %q", i, got[i], want[i])
		}
	}
}

// TestErr checks that warnings keep the server from starting only if
// strict, and how the error reads of one problem and of several.
func TestErr(t *testing.T) {
	var p Problems
	p.Warnf("A.Old", "A.Old is gone")
	if err := p.Err(false); err != nil {
		t.Errorf("a warning: error %v", err)
	}
	if err := p.Err(true); err == nil || err.Error() != "A.Old is gone" {
		t.Errorf("a warning, strict: error %v", err)
	}
	p.Errorf("B.New", "B.New is bad")
	p.Add("C.Parsed", nil)
	p.Add("C.Parsed", os.ErrInvalid)
	p.AddProblem(Problem{Message: "the file is odd"})
	p.Locate(func(key string) int { return map[string]int{"A.Old": 7, "B.New": 3}[key] })
	err := p.Err(false).(*Error)
	err.File = "config.toml"
	const want = "config.toml has 3 problems:\n\tC.Parsed: invalid argument\n\tthe file is odd\n\tline 3: B.New is bad"
	if err.Error() != want {
		t.Errorf("error\n%v\nwant\n%s", err, want)
	}
}

// TestPaths checks Folder, ReadFolder and File on what exists and what does
// not.
func TestPaths(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, nil, 0644); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name string
		ok   bool
	}{
		{"folder", Folder(new(Problems), "K", dir)},
		{"new folder", Folder(new(Problems), "K", filepath.Join(dir, "a", "b"))},
		{"read folder", ReadFolder(new(Problems), "K", dir)},
		{"file as folder", !Folder(new(Problems), "K", file)},
		{"folder in file", !Folder(new(Problems), "K", filepath.Join(file, "a"))},
		{"file", File(new(Problems), "K", file)},
		{"new file", File(new(Problems), "K", filepath.Join(dir, "new"))},
		{"folder as file", !File(new(Problems), "K", dir)},
		{"file in no folder", !File(new(Problems), "K", filepath.Join(dir, "a", "new"))},
	} {
		if !c.ok {
			t.Errorf("%s: wrong result", c.name)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Errorf("the checks left %v in the folder, %v", entries, err)
	}
}
