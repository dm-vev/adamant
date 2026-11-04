package c2225

import (
	"bytes"
	"math/rand/v2"
	"reflect"
	"slices"
	"testing"

	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2225"
)

// one converts a packet with a step function and returns the one packet it
// gives, decoded again from its bytes in the codec of its protocol.
func one(t *testing.T, f func(proto.Packet) []proto.Packet, c codec, pk proto.Packet) proto.Packet {
	t.Helper()
	pks := f(pk)
	if len(pks) != 1 {
		t.Fatalf("%T converts to %d packets", pk, len(pks))
	}
	b, ok := encode(pks[0])
	if !ok {
		t.Fatalf("%T converts to %+v, which does not encode", pk, pks[0])
	}
	back, ok := decode(c, pks[0].ID(), b)
	if !ok {
		t.Fatalf("%T converts to bytes % x that do not decode with %s", pk, head(b), c.name)
	}
	return back
}

// roundTrip converts a packet of 2193 up and back and checks it comes back
// to its bytes; it returns the packet of 2225.
func roundTrip(t *testing.T, pk proto.Packet) proto.Packet {
	t.Helper()
	up := one(t, Up, to, pk)
	down := one(t, Down, from, up)
	want, _ := encode(pk)
	if got, _ := encode(down); !bytes.Equal(got, want) {
		t.Fatalf("%T comes back as %+v, not %+v", pk, down, pk)
	}
	return up
}

func TestPlayFabIDMoves(t *testing.T) {
	pk := &v2193.PlayerListPacket{Entries: []v2193.PlayerListEntry{{
		UUID: [16]byte{1}, EntityUniqueID: 7, Username: "Steve", XUID: "2535400000000042", PlatformChatID: "chat",
		Skin: v2193.Skin{SkinID: "skin", PlayFabID: "pf00", SkinResourcePatch: []byte("{}"), FullID: "full"},
	}}}
	up := roundTrip(t, pk).(*v2225.PlayerListPacket)
	if e := up.Entries[0]; e.PlayFabID != "pf00" || e.Skin.SkinID != "skin" || e.Skin.FullID != "full" || e.Username != "Steve" {
		t.Fatalf("entry %+v", e)
	}
	skin := one(t, Up, to, &v2193.PlayerSkinPacket{Skin: v2193.Skin{SkinID: "s", PlayFabID: "pf"}}).(*v2225.PlayerSkinPacket)
	if skin.Skin.SkinID != "s" {
		t.Fatalf("skin %+v", skin.Skin)
	}
	if down := one(t, Down, from, skin).(*v2193.PlayerSkinPacket); down.Skin.PlayFabID != "" || down.Skin.SkinID != "s" {
		t.Fatalf("skin of 2225 back %+v", down.Skin)
	}
}

func TestFilteredStructureName(t *testing.T) {
	for name, want := range map[string]proto.Optional[string]{"": {}, "x": proto.Some("x")} {
		up := roundTrip(t, &v2193.StructureBlockUpdatePacket{FilteredStructureName: name}).(*v2225.StructureBlockUpdatePacket)
		if up.FilteredStructureName != want {
			t.Errorf("%q goes up as %+v", name, up.FilteredStructureName)
		}
	}
}

func TestSoundDataUpdate(t *testing.T) {
	u := v2193.SoundDataUpdate{Type: v2193.SoundDataUpdateSetVolume, Volume: 0.5}
	up := roundTrip(t, &v2193.ClientboundUpdateSoundDataPacket{ServerSoundHandle: 3, Stop: u, SetVolume: u, SetPitch: u, Fade: u, SeekTo: u, Pause: u, Resume: u}).(*v2225.ClientboundUpdateSoundDataPacket)
	if up.ServerSoundHandle != 3 || up.Update != (v2225.SoundDataUpdate{Type: v2225.SoundDataUpdateSetVolume, Volume: 0.5}) {
		t.Fatalf("up %+v", up)
	}
	// The client of 2193 takes the last of the seven.
	last := one(t, Up, to, &v2193.ClientboundUpdateSoundDataPacket{Stop: u, Resume: v2193.SoundDataUpdate{Type: v2193.SoundDataUpdatePause}}).(*v2225.ClientboundUpdateSoundDataPacket)
	if last.Update.Type != v2225.SoundDataUpdatePause {
		t.Fatalf("up takes %+v, not the last update", last.Update)
	}
}

func TestDebugMarker(t *testing.T) {
	marker := &v2193.ClientBoundDebugRendererPacket{Type: debugMarker, Text: "here", Position: [3]float32{1, 2, 3}, Red: 1, Green: 0, Blue: 0.2, Alpha: 1, Duration: 50}
	up := one(t, Up, to, marker).(*v2225.ClientBoundDebugRendererPacket)
	if d, ok := up.Data.Value(); !ok || d.Text != "here" || d.Colour != (proto.Color{255, 0, 51, 255}) || d.Duration != 50 || d.Position != marker.Position {
		t.Fatalf("marker %+v", up.Data)
	}
	down := one(t, Down, from, up).(*v2193.ClientBoundDebugRendererPacket)
	if down.Red != 1 || down.Blue != 0.2 || down.Text != "here" {
		t.Fatalf("marker back %+v", down)
	}
	if clear := roundTrip(t, &v2193.ClientBoundDebugRendererPacket{Type: 0}).(*v2225.ClientBoundDebugRendererPacket); clear.Data.Set {
		t.Fatalf("a clear carries a marker: %+v", clear)
	}
}

func TestAgentCapabilities(t *testing.T) {
	up := roundTrip(t, &v2193.EducationSettingsPacket{CanModifyBlocks: proto.Some(true), CodeBuilderTitle: "c"}).(*v2225.EducationSettingsPacket)
	if c, ok := up.AgentCapabilities.Value(); !ok || c.CanModifyBlocks != proto.Some(true) || up.CodeBuilderTitle != "c" {
		t.Fatalf("up %+v", up)
	}
	if up := roundTrip(t, &v2193.EducationSettingsPacket{}).(*v2225.EducationSettingsPacket); up.AgentCapabilities.Set {
		t.Fatalf("capabilities of none: %+v", up)
	}
}

func TestEnvironmentAttributes(t *testing.T) {
	attr := func(v float32) v2193.AttributeData {
		return v2193.AttributeData{Type: 1, FloatValue: v}
	}
	layer := func(a v2193.EnvironmentAttributeData, noise string) *v2193.ClientBoundAttributeLayerSyncPacket {
		l := v2193.AttributeLayerData{Name: "l", Settings: v2193.AttributeLayerSettings{Enabled: true}, EnvironmentAttributes: []v2193.EnvironmentAttributeData{a}}
		if noise != "" {
			l.NoiseName = proto.Some(noise)
		}
		return &v2193.ClientBoundAttributeLayerSyncPacket{PayloadType: 0, Layers: []v2193.AttributeLayerData{l}}
	}
	sync := func(pk proto.Packet) v2225.EnvironmentAttributeData {
		up := one(t, Up, to, pk).(*v2225.ClientBoundAttributeLayerSyncPacket)
		return up.Layers[0].EnvironmentAttributes[0]
	}
	if c := sync(layer(v2193.EnvironmentAttributeData{AttributeName: "fog", Attribute: attr(2)}, "")); c.PayloadType != attributeConstant || c.Attribute.FloatValue != 2 {
		t.Errorf("constant %+v", c)
	}
	tr := sync(layer(v2193.EnvironmentAttributeData{AttributeName: "fog", FromAttribute: proto.Some(attr(1)), Attribute: attr(2), ToAttribute: proto.Some(attr(3)), TotalTransitionTicks: 20, CurrentTransitionTicks: 5, EaseType: 2}, ""))
	if tr.PayloadType != attributeTransition || tr.FromAttribute.FloatValue != 1 || tr.ToAttribute.FloatValue != 3 ||
		tr.TransitionSettings != (v2225.AttributeTransitionSettings{TotalTransitionTicks: 20, CurrentTransitionTicks: 5, EaseType: 2}) {
		t.Errorf("transition %+v", tr)
	}
	noise := sync(layer(v2193.EnvironmentAttributeData{AttributeName: "fog", Attribute: attr(2), NoiseTransition: true, LocalTransitionTicks: 4, TotalTransitionTicks: 8}, "perlin"))
	if s := noise.NoiseTransitionSettings; noise.PayloadType != attributeNoiseTransition || s.NoiseName != "perlin" || s.LocalTransitionTicks != 4 || s.TotalTransitionTicks != 8 {
		t.Errorf("noise transition %+v", noise)
	}
	down := one(t, Down, from, one(t, Up, to, layer(v2193.EnvironmentAttributeData{AttributeName: "fog", Attribute: attr(2), NoiseTransition: true}, "perlin"))).(*v2193.ClientBoundAttributeLayerSyncPacket)
	if l := down.Layers[0]; l.NoiseName != proto.Some("perlin") || !l.EnvironmentAttributes[0].NoiseTransition {
		t.Errorf("noise transition back %+v", l)
	}
}

func TestCameraSplines(t *testing.T) {
	def := v2193.CameraSplineDefinition{Name: "s", SplineType: proto.Some("catmull_rom"),
		ProgressKeyFrames: []v2193.CameraProgressOption{{Value: 0.5, Time: 1, EaseType: 2}},
		RotationKeyFrames: []v2193.CameraRotationOption{{Value: [3]float32{1, 2, 3}, Time: 2, EaseType: 1}}}
	up := roundTrip(t, &v2193.CameraSplinePacket{Splines: []v2193.CameraSplineDefinition{def}}).(*v2225.CameraSplinePacket)
	s := up.Splines[0]
	if s.SplineType != "catmull_rom" || s.ProgressKeyFrames[0].EaseType != v2225.Optional(proto.Some[int32](2)) || s.RotationKeyFrames[0].Value != [3]float32{1, 2, 3} {
		t.Fatalf("spline %+v", s)
	}
	frames := v2225.CameraSplinePacket{Splines: []v2225.CameraSplineDefinition{{Name: "s", ProgressKeyFrames: []v2225.CameraSplineProgressKeyFrame{{Value: 1}}}}}
	if down := one(t, Down, from, &frames).(*v2193.CameraSplinePacket); down.Splines[0].ProgressKeyFrames[0].EaseType != 0 {
		t.Fatalf("a key frame of no easing goes down as %+v", down.Splines[0].ProgressKeyFrames[0])
	}
}

func TestDiagnostics(t *testing.T) {
	pk := &v2193.ServerBoundDiagnosticsPacket{
		SystemCategories:  []v2193.SystemCategory{{CategoryName: "c", SystemIndex: 2}},
		EntityDiagnostics: []v2193.EntityDiagnosticTimingInfo{{DisplayName: "pig", Position: [3]float32{1, 2, 3}, Dimension: "overworld"}},
		// A memory category means the same in both: Mojang numbers them
		// alike, 110 categories and Executable (110) added in 2225.
		MemoryCategoryValues: []v2193.MemoryCategoryCounter{{Category: 43, Bytes: 9}},
	}
	up := roundTrip(t, pk).(*v2225.ServerBoundDiagnosticsPacket)
	if e := up.EntityDiagnostics[0]; e.Position != proto.Some([3]float32{1, 2, 3}) || e.Dimension != proto.Some("overworld") ||
		len(up.SystemCategories.Val) != 1 || up.MemoryCategoryValues[0].Category != 43 {
		t.Fatalf("up %+v", up)
	}
	down := one(t, Down, from, &v2225.ServerBoundDiagnosticsPacket{EntityDiagnostics: []v2225.EntityDiagnosticTimingInfo{{DisplayName: "pig"}}}).(*v2193.ServerBoundDiagnosticsPacket)
	if e := down.EntityDiagnostics[0]; e.Dimension != "" || down.SystemCategories == nil {
		t.Fatalf("down %+v", down)
	}
}

func TestStackRequests(t *testing.T) {
	results := &v2193.CraftResultsDeprecatedStackRequestAction{ResultItems: []v2193.StackRequestItem{{Identifier: "minecraft:stick", Count: 4}}, TimesCrafted: 1}
	pk := &v2193.ItemStackRequestPacket{Requests: []v2193.ItemStackRequest{{RequestID: -5, Actions: []v2193.StackRequestAction{
		&v2193.CraftRecipeStackRequestAction{RecipeNetworkID: 9, NumberOfCrafts: 1}, results,
	}}}}
	up := roundTrip(t, pk).(*v2225.ItemStackRequestPacket)
	b, _ := encode(up)
	// The results go as variant 18 and action 20 in 2225, 17 and 19 in 2193.
	if !bytes.Contains(b, []byte{18, 20}) {
		t.Fatalf("the results of 2225 are not action 20: % x", b)
	}
	reserved := &v2225.ItemStackRequestPacket{Requests: []v2225.ItemStackRequest{{Actions: []v2225.StackRequestAction{
		&v2225.CraftReservedStackRequestAction{ReservedID: "r", NumberOfCrafts: 1}, &v2225.TakeStackRequestAction{Count: 1},
	}}}}
	down := one(t, Down, from, reserved).(*v2193.ItemStackRequestPacket)
	if a := down.Requests[0].Actions; len(a) != 1 || !reflect.DeepEqual(a[0], &v2193.TakeStackRequestAction{Count: 1}) {
		t.Fatalf("the reserved craft goes down as %+v", a)
	}
	input := &v2193.PlayerAuthInputPacket{InputData: v2193.InputFlags{v2193.InputFlagPerformItemStackRequest}, ItemStackRequest: proto.Some(v2193.ItemStackRequest{Actions: []v2193.StackRequestAction{results}})}
	if up := roundTrip(t, input).(*v2225.PlayerAuthInputPacket); len(up.ItemStackRequest.Val.Actions) != 1 {
		t.Fatalf("the stack request of an input %+v", up.ItemStackRequest)
	}
}

func TestTransactionHands(t *testing.T) {
	use := &v2193.InventoryTransactionPacket{TransactionData: &v2193.UseItemOnEntityTransactionData{TargetEntityRuntimeID: 4, ActionType: 1, HotBarSlot: 2}}
	up := roundTrip(t, use).(*v2225.InventoryTransactionPacket)
	if d := up.TransactionData.(*v2225.UseItemOnEntityTransactionData); d.Hand != v2225.HandSlotMainHand || d.TargetEntityRuntimeID != 4 || d.HotBarSlot != 2 {
		t.Fatalf("use on an entity %+v", d)
	}
	off := &v2225.InventoryTransactionPacket{TransactionData: &v2225.ReleaseItemTransactionData{ActionType: 1, Hand: v2225.HandSlotOffHand}}
	if d := one(t, Down, from, off).(*v2193.InventoryTransactionPacket).TransactionData.(*v2193.ReleaseItemTransactionData); d.ActionType != 1 {
		t.Fatalf("release %+v", d)
	}
	roundTrip(t, &v2193.InventoryTransactionPacket{LegacyRequestID: -2, LegacySetItemSlots: []v2193.LegacySetItemSlot{{ContainerID: 1, Slots: []byte{3}}},
		Actions:         []v2193.InventoryAction{{SourceType: 2, InventorySlot: 1, NewItem: v2193.ItemInstance{StackNetworkID: 3, Stack: v2193.ItemStack{NetworkID: 5, Count: 1}}}},
		TransactionData: &v2193.NormalTransactionData{}})
}

// TestNewFields checks the fields 2225 adds take the zero value going up,
// which means what 2193 meant, and are dropped going down.
func TestNewFields(t *testing.T) {
	if a := roundTrip(t, &v2193.AnimatePacket{ActionType: 1, EntityRuntimeID: 3}).(*v2225.AnimatePacket); a.Hand != v2225.HandSlotMainHand {
		t.Errorf("animate %+v", a)
	}
	if c := roundTrip(t, &v2193.LevelChunkPacket{Position: [2]int32{1, 2}, RawPayload: []byte{1}}).(*v2225.LevelChunkPacket); c.ClientBiomeUpdate {
		t.Errorf("chunk %+v", c)
	}
	game := roundTrip(t, &v2193.StartGamePacket{WorldName: "w", GameVersion: "1.26.50", ExportedFromEditor: true}).(*v2225.StartGamePacket)
	if game.EditorLevelMigrationVersion != v2225.EditorLevelMigrationVersionLegacy || game.WorldName != "w" || !game.ExportedFromEditor {
		t.Errorf("start game %+v", game)
	}
	dim := roundTrip(t, &v2193.DimensionDataPacket{Definitions: []v2193.DimensionDefinition{{Name: "d", MinimumY: -64, HeightRange: 384}}}).(*v2225.DimensionDataPacket)
	if d := dim.Definitions[0]; d.CloudHeight != 0 || d.RenderClouds || d.Name != "d" {
		t.Errorf("dimension %+v", d)
	}
	seated := &v2225.AddActorPacket{EntityUniqueID: 9, PassengerOfBlock: proto.Some(v2225.PassengerOfBlockData{BlockPosition: [3]int32{1, 2, 3}})}
	if a := one(t, Down, from, seated).(*v2193.AddActorPacket); a.EntityUniqueID != 9 {
		t.Errorf("actor %+v", a)
	}
	roundTrip(t, &v2193.AddPlayerPacket{Username: "p", HeldItem: v2193.ItemInstance{Stack: v2193.ItemStack{NetworkID: 3, Count: 1}}, EntityMetadata: v2193.Metadata{0: int64(1)}})
}

func TestLevelEventsOfNoForm(t *testing.T) {
	for _, e := range []int32{9817, 9818, 9819, 9820} {
		if pks := Up(&v2193.LevelEventPacket{EventType: e}); len(pks) != 0 {
			t.Errorf("level event %d of 2193 goes up", e)
		}
	}
	if pks := Up(&v2193.LevelEventPacket{EventType: 9816}); len(pks) != 1 {
		t.Error("level event 9816 does not go up")
	}
	if pks := Down(&v2225.LevelEventPacket{EventType: levelEventIceBallBreak}); len(pks) != 0 {
		t.Error("the particles of an ice ball go down")
	}
	if pks := Down(&v2225.LevelEventPacket{EventType: v2225.LevelEventParticleLegacyEvent | 104}); len(pks) != 1 {
		t.Error("particle 104 does not go down")
	}
}

// TestCoverage checks every packet that changed is reviewed, and the
// packets of no form are left out.
func TestCoverage(t *testing.T) {
	for _, id := range changed {
		if reviewed[id] == "" {
			t.Errorf("packet %d changed and is not reviewed in coverage.go", id)
		}
		if _, ok := upPacket(from.new(id)); !ok {
			t.Errorf("packet %d has no converter up", id)
		}
		if _, ok := downPacket(to.new(id)); !ok {
			t.Errorf("packet %d has no converter down", id)
		}
	}
	for id := range reviewed {
		if !slices.Contains(changed, id) {
			t.Errorf("packet %d is reviewed and did not change", id)
		}
	}
	for _, id := range added {
		if noForm[id] == "" {
			t.Errorf("packet %d of 2225 has no form in 2193 and is not listed", id)
		}
		if pks := Down(to.new(id)); len(pks) != 0 {
			t.Errorf("packet %d of 2225 goes down", id)
		}
	}
	for _, id := range removed {
		if noForm[id] == "" || len(Up(from.new(id))) != 0 {
			t.Errorf("packet %d of 2193 has no form in 2225 and is not left out", id)
		}
	}
}

// FuzzUpDown converts what decodes of hostile bytes up and down: the step
// never panics, and gives packets that encode.
func FuzzUpDown(f *testing.F) {
	rng := rand.New(rand.NewPCG(3, 4))
	for _, id := range changed {
		for _, b := range samples(rng, id)[:2] {
			f.Add(uint16(id), b)
		}
	}
	f.Fuzz(func(t *testing.T, id uint16, b []byte) {
		for _, c := range []struct {
			src, dst codec
			step     func(proto.Packet) []proto.Packet
		}{{from, to, Up}, {to, from, Down}} {
			pk, ok := decode(c.src, uint32(id)&0x3ff, b)
			if !ok {
				continue
			}
			for _, out := range c.step(pk) {
				if _, ok := encode(out); !ok {
					t.Fatalf("packet %d of %s converts to %+v, which does not encode", id, c.src.name, out)
				}
			}
		}
	})
}
