package overworld

import (
 "encoding/json"
 "testing"
 "github.com/df-mc/dragonfly/server/internal/vanilla"
)

// Sequences and following nextLong values were produced by the pinned Java 26.3
// IntProvider implementations at seed 123, independently of this sampler.
func TestIntProviderSequences(t *testing.T) {
 tests := []struct { name, provider string; xoroshiro bool; want [16]int32; next int64 }{
  {name: "legacy/constant", provider: `7`, xoroshiro: false, want: [16]int32{7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7}, next: -5106534569952410475},
  {name: "legacy/uniform", provider: `{"type":"minecraft:uniform","min_inclusive":-4,"max_inclusive":9}`, xoroshiro: false, want: [16]int32{-4, 2, 2, 3, 7, 7, 6, -3, 5, -3, 5, 4, 8, 1, -3, 5}, next: -3758321679654915806},
  {name: "legacy/uniform_single", provider: `{"type":"minecraft:uniform","min_inclusive":4,"max_inclusive":4}`, xoroshiro: false, want: [16]int32{4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4}, next: -3758321679654915806},
  {name: "legacy/biased", provider: `{"type":"minecraft:biased_to_bottom","min_inclusive":0,"max_inclusive":15}`, xoroshiro: false, want: [16]int32{2, 4, 2, 7, 0, 6, 5, 1, 1, 6, 12, 2, 7, 0, 0, 0}, next: 7830028867000074426},
  {name: "legacy/very_biased", provider: `{"type":"minecraft:very_biased_to_bottom","min_inclusive":0,"max_inclusive":15}`, xoroshiro: false, want: [16]int32{2, 0, 6, 6, 1, 0, 0, 3, 0, 0, 0, 1, 0, 2, 0, 0}, next: 8099979915178793733},
  {name: "legacy/trapezoid", provider: `{"type":"minecraft:trapezoid","min":-7,"max":12,"plateau":5}`, xoroshiro: false, want: [16]int32{-2, 7, 3, 6, 5, 8, 1, -2, -3, -6, 7, 2, 0, 0, -2, -2}, next: 7830028867000074426},
  {name: "legacy/triangle", provider: `{"type":"minecraft:trapezoid","min":-7,"max":7,"plateau":0}`, xoroshiro: false, want: [16]int32{4, 5, -2, 2, 2, 3, 4, -5, 4, 3, 3, 0, 4, 0, 0, -2}, next: 7830028867000074426},
  {name: "legacy/clamped", provider: `{"type":"minecraft:clamped","source":{"type":"minecraft:uniform","min_inclusive":-20,"max_inclusive":20},"min_inclusive":-3,"max_inclusive":4}`, xoroshiro: false, want: [16]int32{-3, -3, 1, 3, -2, 4, 2, 4, 4, 4, -3, 4, -3, -3, 4, 4}, next: -3758321679654915806},
  {name: "legacy/normal", provider: `{"type":"minecraft:clamped_normal","mean":4.25,"deviation":2.5,"min_inclusive":-2,"max_inclusive":9}`, xoroshiro: false, want: [16]int32{0, 5, 4, 4, 4, 3, 7, 5, 3, 6, 7, 0, 3, 2, 0, 3}, next: 8099979915178793733},
  {name: "legacy/weighted", provider: `{"type":"minecraft:weighted_list","distribution":[{"data":2,"weight":3},{"data":{"type":"minecraft:uniform","min_inclusive":5,"max_inclusive":12},"weight":2}]}`, xoroshiro: false, want: [16]int32{2, 2, 2, 7, 2, 7, 2, 12, 2, 2, 2, 2, 11, 2, 6, 2}, next: -7336704456267971543},
  {name: "legacy/weighted_zero", provider: `{"type":"minecraft:weighted_list","distribution":[{"data":99,"weight":0},{"data":7,"weight":1}]}`, xoroshiro: false, want: [16]int32{7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7}, next: -3758321679654915806},
  {name: "xoroshiro/constant", provider: `7`, xoroshiro: true, want: [16]int32{7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7}, next: 1248535248033503029},
  {name: "xoroshiro/uniform", provider: `{"type":"minecraft:uniform","min_inclusive":-4,"max_inclusive":9}`, xoroshiro: true, want: [16]int32{1, 6, 3, 9, -2, -3, 8, 3, -3, 7, 8, 1, 9, 4, 1, 8}, next: 1164814799954847057},
  {name: "xoroshiro/uniform_single", provider: `{"type":"minecraft:uniform","min_inclusive":4,"max_inclusive":4}`, xoroshiro: true, want: [16]int32{4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4, 4}, next: 1164814799954847057},
  {name: "xoroshiro/biased", provider: `{"type":"minecraft:biased_to_bottom","min_inclusive":0,"max_inclusive":15}`, xoroshiro: true, want: [16]int32{5, 8, 0, 8, 1, 5, 9, 6, 1, 1, 4, 0, 1, 7, 0, 3}, next: -6485696380778721421},
  {name: "xoroshiro/very_biased", provider: `{"type":"minecraft:very_biased_to_bottom","min_inclusive":0,"max_inclusive":15}`, xoroshiro: true, want: [16]int32{3, 0, 0, 4, 4, 1, 0, 0, 1, 0, 0, 1, 3, 1, 0, 5}, next: -675352210926151768},
  {name: "xoroshiro/trapezoid", provider: `{"type":"minecraft:trapezoid","min":-7,"max":12,"plateau":5}`, xoroshiro: true, want: [16]int32{4, 6, -5, 8, 0, 7, 9, 5, 1, -3, 3, -7, 0, 7, -5, 4}, next: -6485696380778721421},
  {name: "xoroshiro/triangle", provider: `{"type":"minecraft:trapezoid","min":-7,"max":7,"plateau":0}`, xoroshiro: true, want: [16]int32{-3, -3, 1, 3, -6, 4, 3, -4, 3, -2, -4, 0, -6, 2, -2, 4}, next: -6485696380778721421},
  {name: "xoroshiro/clamped", provider: `{"type":"minecraft:clamped","source":{"type":"minecraft:uniform","min_inclusive":-20,"max_inclusive":20},"min_inclusive":-3,"max_inclusive":4}`, xoroshiro: true, want: [16]int32{-3, 4, 0, 4, -3, -3, 4, 3, -3, 4, 4, -3, 4, 4, -3, 4}, next: 1164814799954847057},
  {name: "xoroshiro/normal", provider: `{"type":"minecraft:clamped_normal","mean":4.25,"deviation":2.5,"min_inclusive":-2,"max_inclusive":9}`, xoroshiro: true, want: [16]int32{4, 8, 1, 4, 0, 0, 2, 8, 4, 3, -1, 0, 2, 0, 6, 4}, next: -4086616512585911239},
  {name: "xoroshiro/weighted", provider: `{"type":"minecraft:weighted_list","distribution":[{"data":2,"weight":3},{"data":{"type":"minecraft:uniform","min_inclusive":5,"max_inclusive":12},"weight":2}]}`, xoroshiro: true, want: [16]int32{2, 9, 6, 2, 9, 2, 12, 2, 9, 2, 9, 2, 2, 2, 2, 5}, next: 1734467640216790734},
  {name: "xoroshiro/weighted_zero", provider: `{"type":"minecraft:weighted_list","distribution":[{"data":99,"weight":0},{"data":7,"weight":1}]}`, xoroshiro: true, want: [16]int32{7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7, 7}, next: 1164814799954847057},
 }
 for _, test := range tests {
  t.Run(test.name, func(t *testing.T) {
   var provider any
   if err := json.Unmarshal([]byte(test.provider), &provider); err != nil { t.Fatal(err) }
   random := vanilla.NewLegacyRandom(123)
   if test.xoroshiro { random = vanilla.NewXoroshiroRandom(123) }
   for i, want := range test.want {
    if got := sampleInt(provider, &random); got != want { t.Fatalf("sample %d = %d, want Java %d", i, got, want) }
   }
   if next := random.NextLong(); next != test.next { t.Fatalf("following nextLong = %d, want Java %d", next, test.next) }
  })
 }
}
