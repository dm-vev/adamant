package c2225

import (
	"slices"

	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2225"
)

// The fixes of the converters of convert_gen.go: each runs after the copy
// of the fields by name, for the fields whose type changed and what the
// copy gets wrong.

// fixDownItemStackRequest leaves the actions 2193 has no form of, the
// reserved craft, out of a request.
func fixDownItemStackRequest(_ *v2225.ItemStackRequest, out *v2193.ItemStackRequest) {
	out.Actions = slices.DeleteFunc(out.Actions, func(a v2193.StackRequestAction) bool { return a == nil })
}

// The categories of systems of diagnostics, and the position and dimension
// of an entity timed, are optional in 2225: a client of 2193 sends them
// all.

func fixUpServerBoundDiagnosticsPacket(in *v2193.ServerBoundDiagnosticsPacket, out *v2225.ServerBoundDiagnosticsPacket) {
	if in.SystemCategories == nil {
		return
	}
	categories := make([]v2225.SystemCategory, len(in.SystemCategories))
	for i := range in.SystemCategories {
		categories[i] = upSystemCategory(&in.SystemCategories[i])
	}
	out.SystemCategories = proto.Some(categories)
}

func fixDownServerBoundDiagnosticsPacket(in *v2225.ServerBoundDiagnosticsPacket, out *v2193.ServerBoundDiagnosticsPacket) {
	categories := in.SystemCategories.Val
	out.SystemCategories = make([]v2193.SystemCategory, len(categories))
	for i := range categories {
		out.SystemCategories[i] = downSystemCategory(&categories[i])
	}
}

func fixUpEntityDiagnosticTimingInfo(in *v2193.EntityDiagnosticTimingInfo, out *v2225.EntityDiagnosticTimingInfo) {
	out.Position, out.Dimension = proto.Some(in.Position), proto.Some(in.Dimension)
}

func fixDownEntityDiagnosticTimingInfo(in *v2225.EntityDiagnosticTimingInfo, out *v2193.EntityDiagnosticTimingInfo) {
	out.Position, out.Dimension = in.Position.Val, in.Dimension.Val
}

// The payload of an environment attribute of 2225 is a variant: a
// constant (0), a transition from one value to another (1) or a noise
// transition (2), the last two with their settings. 2193 sends the values
// and the settings of all three alike, the from and to values optional.
const (
	attributeConstant = iota
	attributeTransition
	attributeNoiseTransition
)

// fixUpEnvironmentAttributeData takes a noise transition of 2193 for a
// noise transition, an attribute with a from or to value or a total of
// ticks for a transition, and any other for a constant. A transition of
// 2225 has a clock, which 2193 has not: none.
func fixUpEnvironmentAttributeData(in *v2193.EnvironmentAttributeData, out *v2225.EnvironmentAttributeData) {
	switch {
	case in.NoiseTransition:
		out.PayloadType = attributeNoiseTransition
		out.NoiseTransitionSettings = v2225.AttributeNoiseTransitionSettings{
			TotalTransitionTicks:   in.TotalTransitionTicks,
			CurrentTransitionTicks: in.CurrentTransitionTicks,
			EaseType:               in.EaseType,
			LocalTransitionTicks:   in.LocalTransitionTicks,
			NoiseAlignment:         upNoiseAlignment(&in.NoiseAlignment),
		}
	case in.FromAttribute.Set || in.ToAttribute.Set || in.TotalTransitionTicks != 0:
		out.PayloadType = attributeTransition
		out.TransitionSettings = v2225.AttributeTransitionSettings{
			TotalTransitionTicks:   in.TotalTransitionTicks,
			CurrentTransitionTicks: in.CurrentTransitionTicks,
			EaseType:               in.EaseType,
		}
	default:
		out.PayloadType = attributeConstant
		return
	}
	// A transition goes from the from value, or the attribute, to the to
	// value, or the attribute.
	from, to := in.Attribute, in.Attribute
	if in.FromAttribute.Set {
		from = in.FromAttribute.Val
	}
	if in.ToAttribute.Set {
		to = in.ToAttribute.Val
	}
	out.FromAttribute, out.ToAttribute = upAttributeData(&from), upAttributeData(&to)
	out.Attribute = v2225.AttributeData{}
}

// fixDownEnvironmentAttributeData gives a transition of 2225 its to value
// as the attribute of 2193; its clock and the name of its noise are lost.
func fixDownEnvironmentAttributeData(in *v2225.EnvironmentAttributeData, out *v2193.EnvironmentAttributeData) {
	switch in.PayloadType {
	case attributeTransition:
		s := in.TransitionSettings
		out.TotalTransitionTicks, out.CurrentTransitionTicks, out.EaseType = s.TotalTransitionTicks, s.CurrentTransitionTicks, s.EaseType
	case attributeNoiseTransition:
		s := in.NoiseTransitionSettings
		out.TotalTransitionTicks, out.CurrentTransitionTicks, out.EaseType = s.TotalTransitionTicks, s.CurrentTransitionTicks, s.EaseType
		out.LocalTransitionTicks, out.NoiseTransition, out.NoiseAlignment = s.LocalTransitionTicks, true, downNoiseAlignment(&s.NoiseAlignment)
	default:
		return
	}
	out.FromAttribute = proto.Some(downAttributeData(&in.FromAttribute))
	out.ToAttribute = proto.Some(downAttributeData(&in.ToAttribute))
	out.Attribute = downAttributeData(&in.ToAttribute)
}

// The fixes of what a copy by name gets wrong with the types alike.

// 2193 writes the update of a sound seven times, once per kind, and the
// client takes the last (Mojang's note
// clientbound_update_sound_data_packet_tagged_variant; gophertunnel). 2225
// writes it once, tagged with its kind.

func fixUpClientboundUpdateSoundDataPacket(in *v2193.ClientboundUpdateSoundDataPacket, out *v2225.ClientboundUpdateSoundDataPacket) {
	out.Update = upSoundDataUpdate(&in.Resume)
}

func fixDownClientboundUpdateSoundDataPacket(in *v2225.ClientboundUpdateSoundDataPacket, out *v2193.ClientboundUpdateSoundDataPacket) {
	u := downSoundDataUpdate(&in.Update)
	out.Stop, out.SetVolume, out.SetPitch, out.Fade, out.SeekTo, out.Pause, out.Resume = u, u, u, u, u, u, u
}

// debugMarker is the type of a debug renderer that adds a marker, which
// 2193 follows with its fields and 2225 with an optional DebugMarkerData,
// its colour as bytes rather than as floats from 0 to 1.
const debugMarker = 1

func fixUpClientBoundDebugRendererPacket(in *v2193.ClientBoundDebugRendererPacket, out *v2225.ClientBoundDebugRendererPacket) {
	if in.Type != debugMarker {
		return
	}
	channel := func(f float32) uint8 { return uint8(min(max(f, 0), 1)*255 + 0.5) }
	out.Data = proto.Some(v2225.DebugMarkerData{
		Text:     in.Text,
		Position: in.Position,
		Colour:   proto.Color{channel(in.Red), channel(in.Green), channel(in.Blue), channel(in.Alpha)},
		Duration: in.Duration,
	})
}

func fixDownClientBoundDebugRendererPacket(in *v2225.ClientBoundDebugRendererPacket, out *v2193.ClientBoundDebugRendererPacket) {
	if !in.Data.Set {
		return
	}
	d := in.Data.Val
	out.Text, out.Position, out.Duration = d.Text, d.Position, d.Duration
	out.Red, out.Green, out.Blue, out.Alpha = float32(d.Colour[0])/255, float32(d.Colour[1])/255, float32(d.Colour[2])/255, float32(d.Colour[3])/255
}

// Whether the agent of Education Edition may modify blocks is one of its
// capabilities in 2225.

func fixUpEducationSettingsPacket(in *v2193.EducationSettingsPacket, out *v2225.EducationSettingsPacket) {
	if in.CanModifyBlocks.Set {
		out.AgentCapabilities = proto.Some(v2225.EducationAgentCapabilities{CanModifyBlocks: in.CanModifyBlocks})
	}
}

func fixDownEducationSettingsPacket(in *v2225.EducationSettingsPacket, out *v2193.EducationSettingsPacket) {
	if in.AgentCapabilities.Set {
		out.CanModifyBlocks = in.AgentCapabilities.Val.CanModifyBlocks
	}
}

// The noise of a layer of attributes is named by the settings of each
// noise transition in 2225, by the layer in 2193.

func fixUpAttributeLayerData(in *v2193.AttributeLayerData, out *v2225.AttributeLayerData) {
	for i := range out.EnvironmentAttributes {
		if a := &out.EnvironmentAttributes[i]; a.PayloadType == attributeNoiseTransition {
			a.NoiseTransitionSettings.NoiseName = in.NoiseName.Val
		}
	}
}

func fixDownAttributeLayerData(in *v2225.AttributeLayerData, out *v2193.AttributeLayerData) {
	for _, a := range in.EnvironmentAttributes {
		if a.PayloadType == attributeNoiseTransition && a.NoiseTransitionSettings.NoiseName != "" {
			out.NoiseName = proto.Some(a.NoiseTransitionSettings.NoiseName)
			return
		}
	}
}

// The values of enums 2225 changed, in packets of the same layout.
const (
	// levelEventRemovedFirst to Last are the level events 2225 removed
	// (Mojang's note bug_1598758): the sounds of placing and breaking a
	// painting and the cleaning and dyeing of a shulker box in a cauldron.
	levelEventRemovedFirst, levelEventRemovedLast = 9817, 9820
	// levelEventIceBallBreak is the level event of the particles of the
	// break of an ice ball, of particle type 105, which 2225 adds: the
	// particles of type t are the event LevelEventParticleLegacyEvent | t.
	levelEventIceBallBreak = v2225.LevelEventParticleLegacyEvent | 105
)

// upValues returns false for a packet of 2193 that holds a value of an
// enum 2225 has not, which it leaves out.
func upValues(pk proto.Packet) bool {
	if e, ok := pk.(*v2193.LevelEventPacket); ok {
		return e.EventType < levelEventRemovedFirst || e.EventType > levelEventRemovedLast
	}
	return true
}

// downValues returns false for a packet of 2225 that holds a value of an
// enum 2193 has not, which it leaves out.
func downValues(pk proto.Packet) bool {
	if e, ok := pk.(*v2225.LevelEventPacket); ok {
		return e.EventType != levelEventIceBallBreak
	}
	return true
}
