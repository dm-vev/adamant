package java26

import (
	"encoding/json"
	"math"
	"os"
	"testing"
)

func TestMthLookupAndJavaNarrowing(t *testing.T) {
	// Raw float bits from the pinned Java 26.3 Mth class, not analytic sin/cos.
	for _, test := range []struct {
		name     string
		angle    float64
		sin, cos uint32
	}{
		{"zero", 0, 0, 0x3f800000},
		{"positive fractional index", .1, 0x3dcc7392, 0x3f7eb89e},
		{"negative index truncation", -.1, 0xbdcc7392, 0x3f7eb7fd},
		{"float argument at half period", float64(float32(math.Pi)), 0x250d3132, 0xbf800000},
		{"multiple periods", 1000.1, 0x3f60fdf5, 0x3ef4402a},
		{"positive saturation", math.Inf(1), 0xb8c90fdb, 0xb8c90fdb},
		{"negative saturation", math.Inf(-1), 0, 0},
		{"nan narrowing", math.NaN(), 0, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := math.Float32bits(Sin(test.angle)); got != test.sin {
				t.Fatalf("Sin(%v) bits = %08x, want %08x", test.angle, got, test.sin)
			}
			if got := math.Float32bits(Cos(test.angle)); got != test.cos {
				t.Fatalf("Cos(%v) bits = %08x, want %08x", test.angle, got, test.cos)
			}
		})
	}
}

func TestAtan2MatchesJava263Source(t *testing.T) {
	var fixture struct {
		Cases []struct {
			Name        string
			X, Y, Atan2 float64
		} `json:"cases"`
	}
	data, err := os.ReadFile("testdata/java263_atan2_source.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, tc := range fixture.Cases {
		t.Run(tc.Name, func(t *testing.T) {
			if got := Atan2(tc.Y, tc.X); got != tc.Atan2 {
				t.Fatalf("atan2(%v,%v)=%0.17g Source=%0.17g", tc.Y, tc.X, got, tc.Atan2)
			}
		})
	}
}
