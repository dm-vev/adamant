package main

import (
	"bytes"
	"fmt"
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

// schemaFile writes a schema into a directory of its own and returns its
// path.
func schemaFile(t *testing.T, schema string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "schema.txt")
	if err := os.WriteFile(path, []byte(schema), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestSchemaErrors parses schemas of lines the format has no place for:
// the error names the line.
func TestSchemaErrors(t *testing.T) {
	for name, c := range map[string]struct {
		schema string
		line   int
	}{
		"packet of no number":    {"packet x Name", 1},
		"packet of another mark": {"# a comment\n\npacket 1 Name server", 3},
		"enum named twice":       {"enum e\nenum e", 2},
		"enum value of no name":  {"enum e\n\t1", 2},
		"enum value of no value": {"enum e\n\tx name", 2},
		"unknown declaration":    {"struct Name", 1},
		"field of no type":       {"\tField uint8", 1},
		"field too deep":         {"type T\n\t\tField uint8", 2},
		"field under a field":    {"type T\n\tA uint8\n\t\tB uint8", 3},
		"else without if":        {"type T\n\telse", 2},
	} {
		_, err := parse(schemaFile(t, c.schema))
		if err == nil || !strings.HasPrefix(err.Error(), fmt.Sprintf("line %d: ", c.line)) {
			t.Errorf("%s: %v, want an error of line %d", name, err, c.line)
		}
	}
	if _, err := parse(filepath.Join(t.TempDir(), "none.txt")); err == nil {
		t.Error("parsed a schema that is not there")
	}
}

// TestGenerateMarks generates the codecs of a schema of one packet a
// client sends and one that holds a type of no codec: FromClient and Raw
// each report the one, and the type of no codec is not written.
func TestGenerateMarks(t *testing.T) {
	path := schemaFile(t, "packet 1 A client\n\tX uint8\npacket 2 B\n\tM M\ntype M\n\tmanual no codec\n")
	s, err := parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.findHand(filepath.Join(filepath.Dir(path), "packets.go")); err != nil {
		t.Fatal(err)
	}
	s.resolveRaw()
	b, err := s.generate("p")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	for _, want := range []string{
		"func FromClient(id uint32) bool {\n\tswitch id {\n\tcase 1:\n\t\treturn true",
		"func Raw(id uint32) bool {\n\tswitch id {\n\tcase 2:\n\t\treturn true",
		"type APacket struct {\n\tX uint8\n}",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("no %q in\n%s", want, src)
		}
	}
	if strings.Contains(src, "type M ") {
		t.Errorf("wrote a type of no codec:\n%s", src)
	}
}
