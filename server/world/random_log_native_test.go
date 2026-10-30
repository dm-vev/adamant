package world

import (
	"encoding/json"
	"math"
	"os"
	"testing"
)

func TestGaussianLogMatchesInspectedWindowsCRT(t *testing.T) {
	data, err := os.ReadFile("../block/testdata/bedrock_1_26_51_ejection_velocities.json")
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Complete bool
		Cases    []struct {
			InputBits  uint32 `json:"input_bits"`
			OutputBits uint32 `json:"output_bits"`
		} `json:"gaussian_logs"`
		Library struct {
			RVA      string `json:"logf_rva"`
			Dispatch int
		} `json:"math_library"`
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Cases) != 30727 || native.Library.RVA != "0xa9d40" || native.Library.Dispatch != 3 {
		t.Fatal("incomplete inspected CRT profile")
	}
	for i, c := range native.Cases {
		x := math.Float32frombits(c.InputBits)
		if x <= 0 || x >= 1 {
			t.Fatal("native logarithm outside Gaussian radius domain")
		}
		if got := math.Float32bits(nativeGaussianLog32(x)); got != c.OutputBits {
			t.Fatalf("case %d input=%08x log=%08x, native=%08x", i, c.InputBits, got, c.OutputBits)
		}
	}
}
