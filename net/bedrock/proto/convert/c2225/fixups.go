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

// The filtered name of a structure is optional in 2225: an empty name of
// 2193 is none.

func fixUpStructureBlockUpdatePacket(in *v2193.StructureBlockUpdatePacket, out *v2225.StructureBlockUpdatePacket) {
	if in.FilteredStructureName != "" {
		out.FilteredStructureName = proto.Some(in.FilteredStructureName)
	}
}

func fixDownStructureBlockUpdatePacket(in *v2225.StructureBlockUpdatePacket, out *v2193.StructureBlockUpdatePacket) {
	out.FilteredStructureName = in.FilteredStructureName.Val
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

// The type, identifier and loading from JSON of a spline of 2225 always go;
// 2193 marks each present. A key frame of a spline definition has an
// optional easing in 2225, which 2193 always gives.

func fixUpCameraSplineInstruction(in *v2193.CameraSplineInstruction, out *v2225.CameraSplineInstruction) {
	out.SplineType, out.SplineIdentifier, out.LoadFromJson = in.SplineType.Val, in.SplineIdentifier.Val, in.LoadFromJson.Val
}

func fixDownCameraSplineInstruction(in *v2225.CameraSplineInstruction, out *v2193.CameraSplineInstruction) {
	out.SplineType, out.SplineIdentifier, out.LoadFromJson = proto.Some(in.SplineType), proto.Some(in.SplineIdentifier), proto.Some(in.LoadFromJson)
}

func fixUpCameraSplineDefinition(in *v2193.CameraSplineDefinition, out *v2225.CameraSplineDefinition) {
	out.SplineType = in.SplineType.Val
	if in.ProgressKeyFrames != nil {
		out.ProgressKeyFrames = make([]v2225.CameraSplineProgressKeyFrame, len(in.ProgressKeyFrames))
		for i, f := range in.ProgressKeyFrames {
			out.ProgressKeyFrames[i] = v2225.CameraSplineProgressKeyFrame{Value: f.Value, Time: f.Time, EaseType: v2225.Optional(proto.Some(f.EaseType))}
		}
	}
	if in.RotationKeyFrames != nil {
		out.RotationKeyFrames = make([]v2225.CameraSplineRotationKeyFrame, len(in.RotationKeyFrames))
		for i, f := range in.RotationKeyFrames {
			out.RotationKeyFrames[i] = v2225.CameraSplineRotationKeyFrame{Value: f.Value, Time: f.Time, EaseType: v2225.Optional(proto.Some(f.EaseType))}
		}
	}
}

// fixDownCameraSplineDefinition gives a key frame of no easing the first
// easing type, linear.
func fixDownCameraSplineDefinition(in *v2225.CameraSplineDefinition, out *v2193.CameraSplineDefinition) {
	out.SplineType = proto.Some(in.SplineType)
	if in.ProgressKeyFrames != nil {
		out.ProgressKeyFrames = make([]v2193.CameraProgressOption, len(in.ProgressKeyFrames))
		for i, f := range in.ProgressKeyFrames {
			out.ProgressKeyFrames[i] = v2193.CameraProgressOption{Value: f.Value, Time: f.Time, EaseType: f.EaseType.Val}
		}
	}
	if in.RotationKeyFrames != nil {
		out.RotationKeyFrames = make([]v2193.CameraRotationOption, len(in.RotationKeyFrames))
		for i, f := range in.RotationKeyFrames {
			out.RotationKeyFrames[i] = v2193.CameraRotationOption{Value: f.Value, Time: f.Time, EaseType: f.EaseType.Val}
		}
	}
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
