package painting

type Motive struct {
	motive
}

// Alban returns the Alban motive for a painting.
func Alban() Motive { return Motive{0} }

// Aztec returns the Aztec motive for a painting.
func Aztec() Motive { return Motive{1} }

// Aztec2 returns the Aztec2 motive for a painting.
func Aztec2() Motive { return Motive{2} }

// Bomb returns the Bomb motive for a painting.
func Bomb() Motive { return Motive{3} }

// Kebab returns the Kebab motive for a painting.
func Kebab() Motive { return Motive{4} }

// Plant returns the Plant motive for a painting.
func Plant() Motive { return Motive{5} }

// Wasteland returns the Wasteland motive for a painting.
func Wasteland() Motive { return Motive{6} }

// Courbet returns the Courbet motive for a painting.
func Courbet() Motive { return Motive{7} }

// Pool returns the Pool motive for a painting.
func Pool() Motive { return Motive{8} }

// Sea returns the Sea motive for a painting.
func Sea() Motive { return Motive{9} }

// Creebet returns the Creebet motive for a painting.
func Creebet() Motive { return Motive{10} }

// Sunset returns the Sunset motive for a painting.
func Sunset() Motive { return Motive{11} }

// Graham returns the Graham motive for a painting.
func Graham() Motive { return Motive{12} }

// Wanderer returns the Wanderer motive for a painting.
func Wanderer() Motive { return Motive{13} }

// Bust returns the Bust motive for a painting.
func Bust() Motive { return Motive{14} }

// Match returns the Match motive for a painting.
func Match() Motive { return Motive{15} }

// SkullAndRoses returns the Skull and Roses motive for a painting.
func SkullAndRoses() Motive { return Motive{16} }

// Stage returns the Stage motive for a painting.
func Stage() Motive { return Motive{17} }

// Void returns the Void motive for a painting.
func Void() Motive { return Motive{18} }

// Wither returns the Wither motive for a painting.
func Wither() Motive { return Motive{19} }

// Earth returns the Earth motive for a painting.
func Earth() Motive { return Motive{20} }

// Fire returns the Fire motive for a painting.
func Fire() Motive { return Motive{21} }

// Water returns the Water motive for a painting.
func Water() Motive { return Motive{22} }

// Wind returns the Wind motive for a painting.
func Wind() Motive { return Motive{23} }

// Fighters returns the Fighters motive for a painting.
func Fighters() Motive { return Motive{24} }

// DonkeyKong returns the Donkey Kong motive for a painting.
func DonkeyKong() Motive { return Motive{25} }

// Skeleton returns the Skeleton motive for a painting.
func Skeleton() Motive { return Motive{26} }

// BurningSkull returns the Burning Skull motive for a painting.
func BurningSkull() Motive { return Motive{27} }

// PigScene returns the Pigscene motive for a painting.
func PigScene() Motive { return Motive{28} }

// Pointer returns the Pointer motive for a painting.
func Pointer() Motive { return Motive{29} }

// Motives returns all the possible motives for a painting.
func Motives() []Motive {
	return []Motive{
		Alban(), Aztec(), Aztec2(), Bomb(), Kebab(), Plant(), Wasteland(), Courbet(), Pool(), Sea(),
		Creebet(), Sunset(), Graham(), Wanderer(), Bust(), Match(), SkullAndRoses(), Stage(), Void(),
		Wither(), Earth(), Fire(), Water(), Wind(), Fighters(), DonkeyKong(), Skeleton(), BurningSkull(),
		PigScene(), Pointer(),
	}
}

type motive uint8

// Size returns the size of the motive in the 2D axis.
func (m motive) Size() (float64, float64) {
	switch {
	case m.Uint8() < 7:
		return 1, 1
	case m.Uint8() < 12:
		return 2, 1
	case m.Uint8() < 14:
		return 1, 2
	case m.Uint8() < 24:
		return 2, 2
	case m.Uint8() < 25:
		return 4, 2
	case m.Uint8() < 27:
		return 4, 3
	case m.Uint8() < 30:
		return 4, 4
	}
	panic("unknown painting type")
}

// Size returns the size of the motive in the 2D axis.
func (m Motive) Size() (float64, float64) { return m.motive.Size() }

// Uint8 returns the motive as a uint8.
func (m motive) Uint8() uint8 { return uint8(m) }

// Uint8 returns the motive as a uint8.
func (m Motive) Uint8() uint8 { return m.motive.Uint8() }

// String returns the string representation of the motive.
func (m motive) String() string {
	switch m.Uint8() {
	case 0:
		return "Alban"
	case 1:
		return "Aztec"
	case 2:
		return "Aztec2"
	case 3:
		return "Bomb"
	case 4:
		return "Kebab"
	case 5:
		return "Plant"
	case 6:
		return "Wasteland"
	case 7:
		return "Courbet"
	case 8:
		return "Pool"
	case 9:
		return "Sea"
	case 10:
		return "Creebet"
	case 11:
		return "Sunset"
	case 12:
		return "Graham"
	case 13:
		return "Wanderer"
	case 14:
		return "Bust"
	case 15:
		return "Match"
	case 16:
		return "SkullAndRoses"
	case 17:
		return "Stage"
	case 18:
		return "Void"
	case 19:
		return "Wither"
	case 20:
		return "Earth"
	case 21:
		return "Fire"
	case 22:
		return "Water"
	case 23:
		return "Wind"
	case 24:
		return "Fighters"
	case 25:
		return "DonkeyKong"
	case 26:
		return "Skeleton"
	case 27:
		return "BurningSkull"
	case 28:
		return "Pigscene"
	case 29:
		return "Pointer"
	}
	panic("unknown painting type")
}

// String returns the string representation of the motive.
func (m Motive) String() string { return m.motive.String() }

// FromString converts a motive name to a Motive.
func FromString(name string) Motive {
	switch name {
	case "Alban":
		return Alban()
	case "Aztec":
		return Aztec()
	case "Aztec2":
		return Aztec2()
	case "Bomb":
		return Bomb()
	case "Kebab":
		return Kebab()
	case "Plant":
		return Plant()
	case "Wasteland":
		return Wasteland()
	case "Courbet":
		return Courbet()
	case "Pool":
		return Pool()
	case "Sea":
		return Sea()
	case "Creebet":
		return Creebet()
	case "Sunset":
		return Sunset()
	case "Graham":
		return Graham()
	case "Wanderer":
		return Wanderer()
	case "Bust":
		return Bust()
	case "Match":
		return Match()
	case "SkullAndRoses":
		return SkullAndRoses()
	case "Stage":
		return Stage()
	case "Void":
		return Void()
	case "Wither":
		return Wither()
	case "Earth":
		return Earth()
	case "Fire":
		return Fire()
	case "Water":
		return Water()
	case "Wind":
		return Wind()
	case "Fighters":
		return Fighters()
	case "DonkeyKong":
		return DonkeyKong()
	case "Skeleton":
		return Skeleton()
	case "BurningSkull":
		return BurningSkull()
	case "Pigscene":
		return PigScene()
	case "Pointer":
		return Pointer()
	}
	panic("unknown painting type")
}
