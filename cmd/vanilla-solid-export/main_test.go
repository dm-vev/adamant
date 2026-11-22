package main

import (
	"bytes"
	"go/format"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteDefaultsEmitsGoFormattedMapLiterals(t *testing.T) {
	path := filepath.Join(t.TempDir(), "block_defaults_generated.go")
	defaults := [][]defaultProperty{
		{},
		{
			{name: "axis", kind: "s", value: "y"},
		},
		{
			{name: "facing", kind: "s", value: "north"},
			{name: "level", kind: "i", value: "0"},
			{name: "waterlogged", kind: "b", value: "false"},
		},
	}
	if err := writeDefaults(path, defaults); err != nil {
		t.Fatalf("writeDefaults() error = %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read generated defaults: %v", err)
	}
	if strings.Contains(string(got), ", }") {
		t.Fatalf("generated defaults contain trailing comma-space in single-line map literal:\n%s", got)
	}
	wantFormatted, err := format.Source(got)
	if err != nil {
		t.Fatalf("format.Source() error = %v", err)
	}
	if !bytes.Equal(got, wantFormatted) {
		t.Fatalf("generated defaults are not gofmt-clean:\ngot:\n%s\nwant:\n%s", got, wantFormatted)
	}
	for _, wantLine := range []string{
		"\t{},",
		"\t{\"axis\": \"y\"},",
		"\t{\"facing\": \"north\", \"level\": int32(0), \"waterlogged\": false},",
	} {
		if !strings.Contains(string(got), wantLine) {
			t.Fatalf("generated defaults missing expected line %q:\n%s", wantLine, got)
		}
	}
}
