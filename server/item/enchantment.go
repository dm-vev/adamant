package item

import (
	"cmp"
	"slices"
	"strings"
	"sync"

	"github.com/df-mc/dragonfly/server/world"
)

// Enchantment is an enchantment that can be applied to a Stack. It holds an EnchantmentType and level that influences
// the power of the enchantment.
type Enchantment struct {
	t   EnchantmentType
	lvl int
}

// NewEnchantment creates and returns an Enchantment with a specific EnchantmentType and level. If the level passed
// exceeds EnchantmentType.MaxLevel, NewEnchantment panics.
func NewEnchantment(t EnchantmentType, lvl int) Enchantment {
	if lvl < 1 {
		panic("enchantment level must never be below 1")
	}
	return Enchantment{t: t, lvl: lvl}
}

// Level returns the current level of the Enchantment as passed to NewEnchantment upon construction.
func (e Enchantment) Level() int {
	return e.lvl
}

// Type returns the EnchantmentType of the Enchantment as passed to NewEnchantment upon construction.
func (e Enchantment) Type() EnchantmentType {
	return e.t
}

// EnchantmentType represents an enchantment type that can be applied to a Stack, with specific behaviour that modifies
// the Stack's behaviour.
// An instance of an EnchantmentType may be created using NewEnchantment.
type EnchantmentType interface {
	// Name returns the name of the enchantment.
	Name() string
	// MaxLevel returns the maximum level the enchantment should be able to have.
	MaxLevel() int
	// Cost returns the minimum and maximum cost the enchantment may inhibit. The higher this range is, the more likely
	// better enchantments are to be selected.
	Cost(level int) (int, int)
	// Rarity returns the enchantment's rarity.
	Rarity() EnchantmentRarity
	// CompatibleWithEnchantment is called when an enchantment is added to an item. It can be used to check if
	// the enchantment is compatible with other enchantments.
	CompatibleWithEnchantment(t EnchantmentType) bool
	// CompatibleWithItem is also called when an enchantment is added to an item. It can be used to check if
	// the enchantment is compatible with the item type.
	CompatibleWithItem(i world.Item) bool
}

// PrimaryItemEnchantment is implemented by an EnchantmentType that an
// enchanting table offers for only some of the items CompatibleWithItem
// takes, as the primary_items of a Java enchantment narrow its
// supported_items. An anvil and /enchant check CompatibleWithItem alone: they
// put Smite on an axe or a mace, which a table offers it for only on a sword.
type PrimaryItemEnchantment interface {
	EnchantmentType
	// PrimaryItem reports if an enchanting table offers the enchantment for
	// i. It is asked only of an item that CompatibleWithItem holds for.
	PrimaryItem(i world.Item) bool
}

// IsPrimaryItem reports if an enchanting table offers t for i, as Java's
// Enchantment.isPrimaryItem: t goes on i, and, if t is a
// PrimaryItemEnchantment, i is one of its primary items.
func IsPrimaryItem(t EnchantmentType, i world.Item) bool {
	if !t.CompatibleWithItem(i) {
		return false
	}
	p, ok := t.(PrimaryItemEnchantment)
	return !ok || p.PrimaryItem(i)
}

// Enchantable is an interface that can be implemented by items that can be enchanted through an enchanting table.
type Enchantable interface {
	// EnchantmentValue returns the value the item may inhibit on possible enchantments.
	EnchantmentValue() int
}

// RegisterEnchantment registers an enchantment with the ID passed. Once registered, enchantments may be received
// by instantiating an EnchantmentType struct (e.g. enchantment.Protection{})
func RegisterEnchantment(id int, enchantment EnchantmentType) {
	enchantmentMu.Lock()
	defer enchantmentMu.Unlock()

	enchantmentsMap[id] = enchantment
	enchantmentIDs[enchantment] = id
	enchantmentsByJavaID[JavaEnchantmentID(enchantment)] = enchantment
	enchantmentsSorted = sortedEnchantments()
}

// RegisterJavaOnlyEnchantment registers an enchantment that Java has and
// Bedrock lacks, such as Sweeping Edge, without an ID: a Bedrock ID it took
// could name another enchantment to Bedrock, then or once Bedrock adds one.
// The item NBT of a stack holds it under adamant:ench, a list of compounds
// of its Java ID (JavaEnchantmentID) and level, instead of under ench, which
// Bedrock reads. Enchantments lists it after the enchantments registered
// with an ID, in the order of registration. An enchantment registered with
// an ID too is not Java-only.
func RegisterJavaOnlyEnchantment(enchantment EnchantmentType) {
	enchantmentMu.Lock()
	defer enchantmentMu.Unlock()

	if !slices.Contains(javaOnlyEnchantments, enchantment) {
		javaOnlyEnchantments = append(javaOnlyEnchantments, enchantment)
	}
	enchantmentsByJavaID[JavaEnchantmentID(enchantment)] = enchantment
	enchantmentsSorted = sortedEnchantments()
}

// IsJavaOnlyEnchantment reports if e was registered with
// RegisterJavaOnlyEnchantment and not with an ID: Bedrock has no ID of it.
func IsJavaOnlyEnchantment(e EnchantmentType) bool {
	enchantmentMu.RLock()
	defer enchantmentMu.RUnlock()
	return javaOnly(e)
}

// javaOnly reports if e is a Java-only enchantment. The caller holds
// enchantmentMu.
func javaOnly(e EnchantmentType) bool {
	_, numbered := enchantmentIDs[e]
	return !numbered && slices.Contains(javaOnlyEnchantments, e)
}

// enchantmentByJavaID returns the registered enchantment of a Java ID, with
// or without the minecraft namespace.
func enchantmentByJavaID(id string) (EnchantmentType, bool) {
	if !strings.Contains(id, ":") {
		id = "minecraft:" + id
	}
	enchantmentMu.RLock()
	defer enchantmentMu.RUnlock()
	e, ok := enchantmentsByJavaID[id]
	return e, ok
}

// enchantmentOrder returns the place of e in the order of Enchantments,
// which Stack.Enchantments sorts by: its ID, or after every ID for a
// Java-only enchantment, in the order those were registered. An
// unregistered enchantment is at 0.
func enchantmentOrder(e EnchantmentType) int {
	enchantmentMu.RLock()
	defer enchantmentMu.RUnlock()
	if id, ok := enchantmentIDs[e]; ok {
		return id
	}
	if i := slices.Index(javaOnlyEnchantments, e); i >= 0 {
		// Item NBT holds an ID as an int16, so an ID is below 1<<15.
		return 1<<16 + i
	}
	return 0
}

var (
	enchantmentMu   sync.RWMutex
	enchantmentsMap = map[int]EnchantmentType{}
	enchantmentIDs  = map[EnchantmentType]int{}
	// javaOnlyEnchantments holds the enchantments of
	// RegisterJavaOnlyEnchantment, in the order they were registered.
	javaOnlyEnchantments []EnchantmentType
	// enchantmentsByJavaID holds every registered enchantment by its Java
	// ID.
	enchantmentsByJavaID = map[string]EnchantmentType{}
	// enchantmentsSorted holds the registered enchantments as Enchantments
	// returns them. RegisterEnchantment and RegisterJavaOnlyEnchantment
	// build it again; it is never changed otherwise, and Enchantments
	// returns copies of it.
	enchantmentsSorted []EnchantmentType
)

// EnchantmentByID attempts to return an enchantment by the ID it was registered with. If found, the enchantment found
// is returned and the bool true.
func EnchantmentByID(id int) (EnchantmentType, bool) {
	enchantmentMu.RLock()
	defer enchantmentMu.RUnlock()

	e, ok := enchantmentsMap[id]
	return e, ok
}

// EnchantmentID attempts to return the ID the enchantment was registered with. If found, the id is returned and
// the bool true.
func EnchantmentID(e EnchantmentType) (int, bool) {
	enchantmentMu.RLock()
	defer enchantmentMu.RUnlock()

	id, ok := enchantmentIDs[e]
	return id, ok
}

// Enchantments returns a slice of all registered enchantments, ordered by
// the IDs they were registered with, then the Java-only enchantments in the
// order they were registered. The slice is the caller's.
func Enchantments() []EnchantmentType {
	enchantmentMu.RLock()
	defer enchantmentMu.RUnlock()
	return slices.Clone(enchantmentsSorted)
}

// sortedEnchantments returns the enchantments of enchantmentsMap ordered by
// the ID enchantmentIDs holds for each, the ID it was registered with last.
// An enchantment registered with more than one ID is in it once for each.
// Enchantments of the same ID, which only one registered with several IDs
// and one registered later with its last can be, are ordered by the IDs
// that hold them in enchantmentsMap. The Java-only enchantments follow, in
// the order they were registered. The caller holds enchantmentMu.
func sortedEnchantments() []EnchantmentType {
	type entry struct {
		e      EnchantmentType
		id, at int
	}
	entries := make([]entry, 0, len(enchantmentsMap))
	for at, e := range enchantmentsMap {
		entries = append(entries, entry{e: e, id: enchantmentIDs[e], at: at})
	}
	slices.SortFunc(entries, func(a, b entry) int {
		return cmp.Or(cmp.Compare(a.id, b.id), cmp.Compare(a.at, b.at))
	})
	e := make([]EnchantmentType, len(entries), len(entries)+len(javaOnlyEnchantments))
	for i, entry := range entries {
		e[i] = entry.e
	}
	for _, j := range javaOnlyEnchantments {
		if javaOnly(j) {
			e = append(e, j)
		}
	}
	return e
}
