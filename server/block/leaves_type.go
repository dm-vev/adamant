package block

// LeavesType represents a type of leaves block, including wood leaves and azalea leaves.
type LeavesType struct {
	leavesType
}

// OakLeaves returns oak leaves.
func OakLeaves() LeavesType {
	return LeavesType{0}
}

// SpruceLeaves returns spruce leaves.
func SpruceLeaves() LeavesType {
	return LeavesType{1}
}

// BirchLeaves returns birch leaves.
func BirchLeaves() LeavesType {
	return LeavesType{2}
}

// JungleLeaves returns jungle leaves.
func JungleLeaves() LeavesType {
	return LeavesType{3}
}

// AcaciaLeaves returns acacia leaves.
func AcaciaLeaves() LeavesType {
	return LeavesType{4}
}

// DarkOakLeaves returns dark oak leaves.
func DarkOakLeaves() LeavesType {
	return LeavesType{5}
}

// MangroveLeaves returns mangrove leaves.
func MangroveLeaves() LeavesType {
	return LeavesType{6}
}

// CherryLeaves returns cherry leaves.
func CherryLeaves() LeavesType {
	return LeavesType{7}
}

// PaleOakLeaves returns pale oak leaves.
func PaleOakLeaves() LeavesType {
	return LeavesType{8}
}

// AzaleaLeaves returns azalea leaves.
func AzaleaLeaves() LeavesType {
	return LeavesType{9}
}

// FloweringAzaleaLeaves returns flowering azalea leaves.
func FloweringAzaleaLeaves() LeavesType {
	return LeavesType{10}
}

// YellowPoplarLeaves returns yellow poplar leaves.
func YellowPoplarLeaves() LeavesType {
	return LeavesType{11}
}

// OrangePoplarLeaves returns orange poplar leaves.
func OrangePoplarLeaves() LeavesType {
	return LeavesType{12}
}

// RedPoplarLeaves returns red poplar leaves.
func RedPoplarLeaves() LeavesType {
	return LeavesType{13}
}

// LeavesTypes returns all supported leaves types.
func LeavesTypes() []LeavesType {
	return []LeavesType{
		OakLeaves(),
		SpruceLeaves(),
		BirchLeaves(),
		JungleLeaves(),
		AcaciaLeaves(),
		DarkOakLeaves(),
		MangroveLeaves(),
		CherryLeaves(),
		PaleOakLeaves(),
		AzaleaLeaves(),
		FloweringAzaleaLeaves(),
		YellowPoplarLeaves(),
		OrangePoplarLeaves(),
		RedPoplarLeaves(),
	}
}

// WoodLeavesTypes returns all supported leaves types that have an underlying wood type.
func WoodLeavesTypes() []LeavesType {
	return []LeavesType{
		OakLeaves(),
		SpruceLeaves(),
		BirchLeaves(),
		JungleLeaves(),
		AcaciaLeaves(),
		DarkOakLeaves(),
		MangroveLeaves(),
		CherryLeaves(),
		PaleOakLeaves(),
		YellowPoplarLeaves(),
		OrangePoplarLeaves(),
		RedPoplarLeaves(),
	}
}

type leavesType uint8

// Uint8 returns the leaves type as a uint8.
func (t leavesType) Uint8() uint8 {
	return uint8(t)
}

// String returns the Bedrock identifier suffix for the leaves type.
func (t leavesType) String() string {
	switch t {
	case 9:
		return "azalea_leaves"
	case 10:
		return "azalea_leaves_flowered"
	case 11:
		return "yellow_poplar_leaves"
	case 12:
		return "orange_poplar_leaves"
	case 13:
		return "red_poplar_leaves"
	}
	if wood, ok := t.Wood(); ok {
		return wood.String() + "_leaves"
	}
	panic("unknown leaves type")
}

// Wood returns the underlying wood type of the leaves if there is one.
func (t leavesType) Wood() (WoodType, bool) {
	switch t {
	case 0:
		return OakWood(), true
	case 1:
		return SpruceWood(), true
	case 2:
		return BirchWood(), true
	case 3:
		return JungleWood(), true
	case 4:
		return AcaciaWood(), true
	case 5:
		return DarkOakWood(), true
	case 6:
		return MangroveWood(), true
	case 7:
		return CherryWood(), true
	case 8:
		return PaleOakWood(), true
	case 11, 12, 13:
		return PoplarWood(), true
	default:
		return WoodType{}, false
	}
}
