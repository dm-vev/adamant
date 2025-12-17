package biome

// This package defines Minecraft Java Edition 1.12 biome IDs and helpers used by the GenLayer pipeline.

type ID int

const (
	Ocean                ID = 0
	Plains               ID = 1
	Desert               ID = 2
	Mountains            ID = 3 // "Extreme Hills" in 1.12
	Forest               ID = 4
	Taiga                ID = 5
	Swamp                ID = 6
	River                ID = 7
	Nether               ID = 8
	End                  ID = 9
	FrozenOcean          ID = 10
	FrozenRiver          ID = 11
	SnowyTundra          ID = 12 // "Ice Plains" in 1.12
	SnowyMountains       ID = 13 // "Ice Mountains" in 1.12
	MushroomFields       ID = 14 // "Mushroom Island" in 1.12
	MushroomFieldShore   ID = 15
	Beach                ID = 16
	DesertHills          ID = 17
	WoodedHills          ID = 18 // "Forest Hills" in 1.12
	TaigaHills           ID = 19
	MountainEdge         ID = 20 // "Extreme Hills Edge" in 1.12
	Jungle               ID = 21
	JungleHills          ID = 22
	JungleEdge           ID = 23
	DeepOcean            ID = 24
	StoneShore           ID = 25 // "Stone Beach" in 1.12
	SnowyBeach           ID = 26 // "Cold Beach" in 1.12
	BirchForest          ID = 27
	BirchForestHills     ID = 28
	DarkForest           ID = 29 // "Roofed Forest" in 1.12
	SnowyTaiga           ID = 30 // "Cold Taiga" in 1.12
	SnowyTaigaHills      ID = 31
	GiantTreeTaiga       ID = 32 // "Mega Taiga" in 1.12
	GiantTreeTaigaHills  ID = 33
	WoodedMountains      ID = 34 // "Extreme Hills+" in 1.12
	Savanna              ID = 35
	SavannaPlateau       ID = 36
	Badlands             ID = 37 // "Mesa" in 1.12
	WoodedBadlandsPlateau ID = 38 // "Mesa Plateau F" in 1.12
	BadlandsPlateau      ID = 39 // "Mesa Plateau" in 1.12
	TheVoid              ID = 40
)

const MutatedOffset = 128

const (
	SunflowerPlains          ID = Plains + MutatedOffset
	DesertM                  ID = Desert + MutatedOffset
	GravellyMountains        ID = Mountains + MutatedOffset
	FlowerForest             ID = Forest + MutatedOffset
	TaigaMountains           ID = Taiga + MutatedOffset
	SwampHills               ID = Swamp + MutatedOffset
	IceSpikes                ID = SnowyTundra + MutatedOffset
	ModifiedJungle           ID = Jungle + MutatedOffset
	ModifiedJungleEdge       ID = JungleEdge + MutatedOffset
	TallBirchForest          ID = BirchForest + MutatedOffset
	TallBirchHills           ID = BirchForestHills + MutatedOffset
	DarkForestHills          ID = DarkForest + MutatedOffset
	SnowyTaigaMountains      ID = SnowyTaiga + MutatedOffset
	GiantSpruceTaiga         ID = GiantTreeTaiga + MutatedOffset
	GiantSpruceTaigaHills    ID = GiantTreeTaigaHills + MutatedOffset
	ModifiedGravellyMountains ID = WoodedMountains + MutatedOffset
	ShatteredSavanna         ID = Savanna + MutatedOffset
	ShatteredSavannaPlateau  ID = SavannaPlateau + MutatedOffset
	ErodedBadlands           ID = Badlands + MutatedOffset
	ModifiedWoodedBadlandsPlateau ID = WoodedBadlandsPlateau + MutatedOffset
	ModifiedBadlandsPlateau  ID = BadlandsPlateau + MutatedOffset
)

type Category int

const (
	CategoryNone Category = iota
	CategoryBeach
	CategoryDesert
	CategoryMountains
	CategoryForest
	CategorySnowy
	CategoryJungle
	CategoryMesa
	CategoryMushroom
	CategoryStoneShore
	CategoryOcean
	CategoryPlains
	CategoryRiver
	CategorySavanna
	CategorySwamp
	CategoryTaiga
)

func IsOceanic(id int) bool {
	switch ID(id) {
	case Ocean, FrozenOcean, DeepOcean:
		return true
	default:
		return false
	}
}

func IsShallowOcean(id int) bool {
	switch ID(id) {
	case Ocean, FrozenOcean:
		return true
	default:
		return false
	}
}

func IsDeepOcean(id int) bool {
	return ID(id) == DeepOcean
}

func IsMesa(id int) bool {
	switch ID(id) {
	case Badlands, WoodedBadlandsPlateau, BadlandsPlateau, ErodedBadlands, ModifiedWoodedBadlandsPlateau, ModifiedBadlandsPlateau:
		return true
	default:
		return false
	}
}

func IsSnowy(id int) bool {
	switch ID(id) {
	case FrozenOcean, FrozenRiver, SnowyTundra, SnowyMountains, SnowyTaiga, SnowyTaigaHills, SnowyTaigaMountains, IceSpikes, SnowyBeach:
		return true
	default:
		return false
	}
}

func CategoryOf(id int) Category {
	switch ID(id) {
	case Beach, SnowyBeach:
		return CategoryBeach
	case Desert, DesertHills, DesertM:
		return CategoryDesert
	case Mountains, MountainEdge, WoodedMountains, GravellyMountains, ModifiedGravellyMountains:
		return CategoryMountains
	case Forest, WoodedHills, BirchForest, BirchForestHills, DarkForest, FlowerForest, TallBirchForest, TallBirchHills, DarkForestHills:
		return CategoryForest
	case SnowyTundra, SnowyMountains, IceSpikes:
		return CategorySnowy
	case Jungle, JungleHills, JungleEdge, ModifiedJungle, ModifiedJungleEdge:
		return CategoryJungle
	case Badlands, ErodedBadlands, ModifiedWoodedBadlandsPlateau, ModifiedBadlandsPlateau:
		return CategoryMesa
	case WoodedBadlandsPlateau, BadlandsPlateau:
		return CategoryMesa
	case MushroomFields, MushroomFieldShore:
		return CategoryMushroom
	case StoneShore:
		return CategoryStoneShore
	case Ocean, FrozenOcean, DeepOcean:
		return CategoryOcean
	case Plains, SunflowerPlains:
		return CategoryPlains
	case River, FrozenRiver:
		return CategoryRiver
	case Savanna, SavannaPlateau, ShatteredSavanna, ShatteredSavannaPlateau:
		return CategorySavanna
	case Swamp, SwampHills:
		return CategorySwamp
	case Taiga, TaigaHills, SnowyTaiga, SnowyTaigaHills, GiantTreeTaiga, GiantTreeTaigaHills, TaigaMountains, SnowyTaigaMountains, GiantSpruceTaiga, GiantSpruceTaigaHills:
		return CategoryTaiga
	default:
		return CategoryNone
	}
}

func AreSimilar(id1, id2 int) bool {
	if id1 == id2 {
		return true
	}
	if ID(id1) == WoodedBadlandsPlateau || ID(id1) == BadlandsPlateau {
		return ID(id2) == WoodedBadlandsPlateau || ID(id2) == BadlandsPlateau
	}
	return CategoryOf(id1) == CategoryOf(id2)
}

func Mutated(id int) int {
	switch ID(id) {
	case Plains:
		return int(SunflowerPlains)
	case Desert:
		return int(DesertM)
	case Mountains:
		return int(GravellyMountains)
	case Forest:
		return int(FlowerForest)
	case Taiga:
		return int(TaigaMountains)
	case Swamp:
		return int(SwampHills)
	case SnowyTundra:
		return int(IceSpikes)
	case Jungle:
		return int(ModifiedJungle)
	case JungleEdge:
		return int(ModifiedJungleEdge)
	case BirchForest:
		return int(TallBirchForest)
	case BirchForestHills:
		return int(TallBirchHills)
	case DarkForest:
		return int(DarkForestHills)
	case SnowyTaiga:
		return int(SnowyTaigaMountains)
	case GiantTreeTaiga:
		return int(GiantSpruceTaiga)
	case GiantTreeTaigaHills:
		return int(GiantSpruceTaigaHills)
	case WoodedMountains:
		return int(ModifiedGravellyMountains)
	case Savanna:
		return int(ShatteredSavanna)
	case SavannaPlateau:
		return int(ShatteredSavannaPlateau)
	case Badlands:
		return int(ErodedBadlands)
	case WoodedBadlandsPlateau:
		return int(ModifiedWoodedBadlandsPlateau)
	case BadlandsPlateau:
		return int(ModifiedBadlandsPlateau)
	default:
		return -1
	}
}

