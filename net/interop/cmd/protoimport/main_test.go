package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSchemaIsCurrent imports the schema from gophertunnel: it must be the
// one the codecs of package bedrock/proto are generated from, packets a
// client sends marked so.
func TestSchemaIsCurrent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "schema.txt")
	if err := run(path); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile("../../../bedrock/proto/v2193/schema.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("schema.txt is not what protoimport imports: run it")
	}
	if !strings.Contains(string(got), "\npacket 56 BlockActorData client\n") || !strings.Contains(string(got), "\npacket 174 SubChunk\n") {
		t.Fatal("packets are not marked by who sends them")
	}
	if err := run(filepath.Join(path, "x")); err == nil {
		t.Fatal("wrote a schema under a file")
	}
}
