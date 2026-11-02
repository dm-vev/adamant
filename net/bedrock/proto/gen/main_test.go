package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGeneratedCodecsAreCurrent generates the codecs of protocol 2193 from
// its schema: they must be the ones go generate wrote into the package.
func TestGeneratedCodecsAreCurrent(t *testing.T) {
	const out = "../v2193/packets.go"
	s, err := parse("../v2193/schema.txt")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.findHand(out); err != nil {
		t.Fatal(err)
	}
	s.resolveRaw()
	src, err := s.generate("v2193")
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(src, want) {
		t.Fatal("packets.go is not what schema.txt generates: run go generate")
	}
	clients := 0
	for _, d := range s.decls {
		if d.client {
			clients++
		}
	}
	if clients == 0 {
		t.Fatal("no packet a client sends")
	}
}

// TestSchemaErrors parses schemas of lines the format has no place for.
func TestSchemaErrors(t *testing.T) {
	for name, schema := range map[string]string{
		"packet of no number":    "packet x Name",
		"packet of another mark": "packet 1 Name server",
		"enum named twice":       "enum e\nenum e",
		"enum value of no name":  "enum e\n\t1",
		"enum value of no value": "enum e\n\tx name",
		"unknown declaration":    "struct Name",
		"field of no type":       "\tField uint8",
		"field too deep":         "type T\n\t\tField uint8",
		"else without if":        "type T\n\telse",
	} {
		path := filepath.Join(t.TempDir(), "schema.txt")
		if err := os.WriteFile(path, []byte(schema), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := parse(path); err == nil || !strings.HasPrefix(err.Error(), "line ") {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := parse(filepath.Join(t.TempDir(), "none.txt")); err == nil {
		t.Error("parsed a schema that is not there")
	}
}
