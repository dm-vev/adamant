package block

import "testing"

func TestBlastResistanceScale(t *testing.T) {
	tests := []struct {
		name string
		got  float64
		want float64
	}{
		{"default to hardness", Cobweb{}.BreakInfo().BlastResistance, 4},
		{"stone", Stone{}.BreakInfo().BlastResistance, 6},
		{"copper chain", CopperChain{}.BreakInfo().BlastResistance, 6},
		{"obsidian", Obsidian{}.BreakInfo().BlastResistance, 1200},
		{"heavy core", HeavyCore{}.BreakInfo().BlastResistance, 1200},
		{"nether reactor", NetherReactor{}.BreakInfo().BlastResistance, 3},
		{"water", Water{}.BlastResistance(), 100},
	}
	for _, test := range tests {
		if test.got != test.want {
			t.Errorf("%s blast resistance: got %v, want %v", test.name, test.got, test.want)
		}
	}
}
