package structure

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestResolvePoolAliasesMatchSource(t *testing.T) {
	data, err := os.ReadFile("testdata/java263_pool_aliases.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name      string            `json:"name"`
		Bindings  []any             `json:"bindings"`
		Seed      int64             `json:"seed"`
		Pos       [3]int32          `json:"pos"`
		Resolved  map[string]string `json:"resolved"`
		Duplicate bool              `json:"duplicate"`
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%s/%d/%v", tc.Name, tc.Seed, tc.Pos), func(t *testing.T) {
			aliases, err := ResolvePoolAliases(tc.Bindings, tc.Pos, tc.Seed)
			if tc.Duplicate {
				if err == nil {
					t.Fatal("selected duplicate alias was accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			for query, target := range tc.Resolved {
				want := strings.TrimPrefix(target, "minecraft:")
				if got := lookupPoolAlias(query, aliases); got != want {
					t.Errorf("lookup %q = %q, source %q", query, got, want)
				}
			}
		})
	}
}

func TestResolvePoolAliasesRejectDuplicateSelectedAliases(t *testing.T) {
	bindings := []any{
		map[string]any{"type": "direct", "alias": "minecraft:duplicate", "target": "first"},
		map[string]any{"type": "direct", "alias": "duplicate", "target": "second"},
	}
	if _, err := ResolvePoolAliases(bindings, [3]int32{}, 0); err == nil {
		t.Fatal("expected duplicate alias error after namespace normalization")
	}
}

func TestLookupPoolAliasIsOneHopAndNormalizesNamespace(t *testing.T) {
	aliases := map[string]string{"first": "minecraft:second", "second": "third"}
	if got := lookupPoolAlias("minecraft:first", aliases); got != "minecraft:second" {
		t.Fatalf("lookup recursively followed alias: %q", got)
	}
	if got := lookupPoolAlias("minecraft:unmapped", aliases); got != "unmapped" {
		t.Fatalf("unmapped pool = %q", got)
	}
}

func TestResolvePoolAliasesRejectsZeroWeightNonEmptyLists(t *testing.T) {
	for _, raw := range []any{
		[]any{map[string]any{"type": "random", "alias": "a", "targets": []any{map[string]any{"data": "b", "weight": float64(0)}}}},
		[]any{map[string]any{"type": "random_group", "groups": []any{map[string]any{"data": []any{}, "weight": float64(0)}}}},
	} {
		if _, err := ResolvePoolAliases(raw, [3]int32{}, 0); err == nil {
			t.Fatalf("expected codec-compatible zero-total validation error: %#v", raw)
		}
	}
}
