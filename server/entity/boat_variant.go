package entity

import "fmt"

// BoatVariant represents the variant of a boat. Different variants map to the
// various wood types that can be used to craft boats. The order of these
// variants is important as it matches the Bedrock network encoding for
// boat/chest boat entities.
type BoatVariant struct {
	name    string
	variant int32
}

// Name returns the short name of the boat variant, matching vanilla resource
// identifiers such as "oak", "spruce" or "bamboo".
func (v BoatVariant) Name() string {
	return v.name
}

// Variant returns the numerical identifier used for the boat metadata.
func (v BoatVariant) Variant() int32 {
	return v.variant
}

// String implements fmt.Stringer.
func (v BoatVariant) String() string {
	if v == (BoatVariant{}) {
		return "unknown"
	}
	return fmt.Sprintf("boat_variant(%s)", v.name)
}

// BoatVariants returns all boat variants supported by the server.
func BoatVariants() []BoatVariant {
	return []BoatVariant{
		BoatVariant{name: "oak", variant: 0},
		BoatVariant{name: "spruce", variant: 1},
		BoatVariant{name: "birch", variant: 2},
		BoatVariant{name: "jungle", variant: 3},
		BoatVariant{name: "acacia", variant: 4},
		BoatVariant{name: "dark_oak", variant: 5},
		BoatVariant{name: "mangrove", variant: 6},
		BoatVariant{name: "cherry", variant: 7},
		BoatVariant{name: "bamboo", variant: 8},
		BoatVariant{name: "pale_oak", variant: 9},
	}
}

// BoatVariantByName looks up a boat variant by its short name.
func BoatVariantByName(name string) (BoatVariant, bool) {
	for _, v := range BoatVariants() {
		if v.name == name {
			return v, true
		}
	}
	return BoatVariant{}, false
}
