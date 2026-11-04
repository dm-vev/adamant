package entity

import (
	"bufio"
	"crypto/sha256"
	"flag"
	"fmt"
	"maps"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// updateSpawnMobs rewrites the golden file of TestSpawnTableMatches to what spawnMob makes of each identifier
// now.
var updateSpawnMobs = flag.Bool("update-spawn-mobs", false, "rewrite testdata/spawn_mobs.txt to what spawnMob makes of each identifier now")

// spawnMobsGolden holds, for every Java identifier spawnMob knows and a sample of others, the entity type it
// spawns and a hash of the NBT the entity is saved with.
const spawnMobsGolden = "testdata/spawn_mobs.txt"

// unknownSpawnIDs are identifiers that spawnMob has no mob for: no namespace, other cases, padding, and entities
// that are not mobs.
var unknownSpawnIDs = []string{
	"", "minecraft:", "allay", "Minecraft:allay", "minecraft:Allay", "minecraft:allay ", " minecraft:allay",
	"minecraft:player", "minecraft:item", "minecraft:arrow", "minecraft:boat", "minecraft:minecart",
	"minecraft:painting", "minecraft:tnt", "minecraft:pigman", "minecraft:unknown", "custom:zombie",
}

// spawnMobLine spawns the mob of an identifier with fixed identity and randomness, and writes the identifier,
// the type of the entity and a hash of its saved NBT, or "-" twice if there is no mob.
func spawnMobLine(w *world.World, id string) string {
	r := vanilla.NewLegacyRandom(7)
	opts := world.EntitySpawnOpts{
		Position:     mgl64.Vec3{0.5, 100, 0.5},
		ID:           uuid.UUID{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15},
		SpawnSeed:    7,
		EntityRandom: &r,
	}
	h := spawnMob(id, opts)
	if h == nil {
		return strconv.Quote(id) + " - -"
	}
	return fmt.Sprintf("%q %s %x", id, h.Type().EncodeEntity(), nbtHash(w.EntityNBT(h)))
}

// randomSpawnNBT are the NBT paths that a mob draws from the global random when it is made, not from the
// randomness of its spawn options: the time to the next egg of a chicken and to the next scute of an armadillo.
// nbtHash leaves them out.
var randomSpawnNBT = map[string]bool{"EggLayTime": true, "adamant:java/scute_time": true}

// nbtHash hashes NBT data with its map keys sorted, so that equal data hashes equally. The paths of
// randomSpawnNBT are left out.
func nbtHash(data map[string]any) []byte {
	var b strings.Builder
	writeNBTValue(&b, "", reflect.ValueOf(data))
	h := sha256.Sum256([]byte(b.String()))
	return h[:12]
}

// writeNBTValue writes the value at path p of NBT data with the keys of its maps sorted.
func writeNBTValue(b *strings.Builder, p string, v reflect.Value) {
	for v.Kind() == reflect.Interface && !v.IsNil() {
		v = v.Elem()
	}
	switch v.Kind() {
	case reflect.Map:
		keys := v.MapKeys()
		slices.SortFunc(keys, func(a, b reflect.Value) int { return strings.Compare(fmt.Sprint(a), fmt.Sprint(b)) })
		fmt.Fprintf(b, "%s{", v.Type())
		for _, k := range keys {
			kp := strings.TrimPrefix(p+"/"+fmt.Sprint(k), "/")
			if randomSpawnNBT[kp] {
				continue
			}
			fmt.Fprintf(b, "%q:", fmt.Sprint(k))
			writeNBTValue(b, kp, v.MapIndex(k))
			b.WriteString(",")
		}
		b.WriteString("}")
	case reflect.Slice, reflect.Array:
		fmt.Fprintf(b, "%s[", v.Type())
		for i := range v.Len() {
			writeNBTValue(b, p+"/"+strconv.Itoa(i), v.Index(i))
			b.WriteString(",")
		}
		b.WriteString("]")
	case reflect.Invalid:
		b.WriteString("nil")
	default:
		fmt.Fprintf(b, "%s(%v)", v.Type(), v)
	}
}

// readSpawnMobsGolden returns the identifiers of the golden file and its lines by identifier.
func readSpawnMobsGolden(t *testing.T) (ids []string, lines map[string]string) {
	t.Helper()
	f, err := os.Open(spawnMobsGolden)
	if err != nil {
		t.Fatalf("read the golden file: %v (generate it with -update-spawn-mobs)", err)
	}
	defer f.Close()
	lines = map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		l := sc.Text()
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		q, err := strconv.QuotedPrefix(l)
		if err != nil {
			t.Fatalf("%s: malformed line %q", spawnMobsGolden, l)
		}
		id, _ := strconv.Unquote(q)
		ids = append(ids, id)
		lines[id] = l
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return ids, lines
}

// TestSpawnTableMatches spawns the mob of every identifier of the spawn table, of the golden file and of a
// sample of unknown identifiers, and requires the entity type and saved NBT of each to be those of the golden
// file. The golden file was written by the switch that the table replaced.
func TestSpawnTableMatches(t *testing.T) {
	w := world.Config{Entities: DefaultRegistry, Synchronous: true, RandomTickSpeed: -1}.New()
	defer func() { _ = w.Close() }()

	goldenIDs, golden := readSpawnMobsGolden(t)
	domain := slices.Concat(goldenIDs, unknownSpawnIDs, spawnTableIDs())
	slices.Sort(domain)
	domain = slices.Compact(domain)

	var current []string
	for _, id := range domain {
		current = append(current, spawnMobLine(w, id))
	}
	if *updateSpawnMobs {
		var b strings.Builder
		b.WriteString("# What spawnMob makes of each Java identifier: \"<id>\" <entity type> <hash of the saved NBT>, or - -.\n")
		b.WriteString("# Spawned at 0.5 100 0.5 with a fixed UUID, spawn seed and entity random (spawn_table_test.go).\n")
		b.WriteString("# Regenerate with: go test -run TestSpawnTableMatches -update-spawn-mobs ./server/entity\n\n")
		for _, l := range current {
			b.WriteString(l + "\n")
		}
		if err := os.WriteFile(spawnMobsGolden, []byte(b.String()), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	known := 0
	for _, l := range current {
		q, _ := strconv.QuotedPrefix(l)
		id, _ := strconv.Unquote(q)
		want, ok := golden[id]
		switch {
		case !ok:
			t.Errorf("%q is not in %s: %s", id, spawnMobsGolden, l)
		case want != l:
			t.Errorf("spawnMob(%q) = %s, want %s", id, l, want)
		}
		if !strings.HasSuffix(l, " - -") {
			known++
		}
	}
	if n := len(spawnTableIDs()); known != n {
		t.Errorf("%d identifiers spawn a mob, but the spawn table has %d", known, n)
	}
}

// TestSpawnTableIsDeterministic spawns every mob of the spawn table twice and requires the same line, so that
// TestSpawnTableMatches compares mobs and not the randomness of their making.
func TestSpawnTableIsDeterministic(t *testing.T) {
	w := world.Config{Entities: DefaultRegistry, Synchronous: true, RandomTickSpeed: -1}.New()
	defer func() { _ = w.Close() }()
	for _, id := range spawnTableIDs() {
		if a, b := spawnMobLine(w, id), spawnMobLine(w, id); a != b {
			t.Errorf("spawning %q twice gave %s and %s", id, a, b)
		}
	}
}

// spawnTableIDs returns the identifiers of the spawn table, sorted.
func spawnTableIDs() []string { return slices.Sorted(maps.Keys(spawnTable)) }
