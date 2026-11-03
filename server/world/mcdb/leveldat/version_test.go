package leveldat

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"testing"
)

// TestDefaultVersionsRoundTrip checks the versions a new level.dat stores, that
// they survive writing and reading the file, and that the default file keeps
// the bytes it had when the versions came from the protocol package.
func TestDefaultVersionsRoundTrip(t *testing.T) {
	var d Data
	d.FillDefault()
	var ld LevelDat
	if err := ld.Marshal(d); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := ld.Write(&buf); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(buf.Bytes())
	if got, want := hex.EncodeToString(sum[:]), defaultLevelDatSHA256; got != want {
		t.Errorf("default level.dat has SHA-256 %s, want %s", got, want)
	}

	read, err := Read(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	var got Data
	if err := read.Unmarshal(&got); err != nil {
		t.Fatal(err)
	}
	numbers := []int32{1, 26, 50, 0, 0}
	if got.InventoryVersion != "1.26.50" || got.NetworkVersion != 2193 ||
		!slices.Equal(got.MinimumCompatibleClientVersion, numbers) || !slices.Equal(got.LastOpenedWithVersion, numbers) {
		t.Errorf("read back InventoryVersion %q, NetworkVersion %d, MinimumCompatibleClientVersion %v, LastOpenedWithVersion %v",
			got.InventoryVersion, got.NetworkVersion, got.MinimumCompatibleClientVersion, got.LastOpenedWithVersion)
	}
}

// defaultLevelDatSHA256 is the SHA-256 of the level.dat written for the data of
// FillDefault.
const defaultLevelDatSHA256 = "e4abbf5b652fc3f4566092aa96f051d3231f1ec39a47adbad3a07054bef112a2"
