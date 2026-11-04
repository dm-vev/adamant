package main

import (
	"fmt"
	"log"
	"slices"
	"strings"
)

// correction is a declaration gophertunnel codes otherwise than Mojang's
// protocol documentation (Mojang/bedrock-protocol-docs, json/*.json)
// describes it. A gophertunnel that still codes it as wrong gets the lines
// right instead, and the declarations those lines need, added before it if
// the schema has none of their names; one that codes it otherwise is left
// as it is. Each correction names its evidence.
type correction struct {
	// name is the name of the declaration, a packet or a type.
	name string
	// wrong are the lines gophertunnel's codec translates to, right those
	// of Mojang's documentation.
	wrong, right []string
	// add are the declarations right refers to.
	add []*decl
}

// corrections are the declarations corrected, in the order of the schema.
var corrections = []correction{
	{
		// Mojang's json/ItemStackRequestActionCraftRecipeAuto.json lists
		// the ingredients as json/RecipeIngredient.json: the variant of
		// the descriptor as a varuint32, its type again as a byte, its
		// data and the count as an uint16, not as the ingredients of
		// CraftingData recipes. A Linux BDS 1.26.52.3 accepted requests of
		// this form and dropped the client for the other (e470b957).
		name:  "AutoCraftRecipeStackRequestAction",
		wrong: []string{"RecipeNetworkID varuint32", "NumberOfCrafts uint8", "Ingredients []ItemDescriptorCount"},
		right: []string{"RecipeNetworkID varuint32", "NumberOfCrafts uint8", "Ingredients []RecipeIngredient"},
		add:   []*decl{{name: "RecipeIngredient", manual: "coded by hand as Mojang's RecipeIngredient"}},
	},
	{
		// Mojang's json/PlayerVideoCapturePacketPayload.json is a variant,
		// its index (x-control-value-type uint32) a varuint32 as that of
		// json/PlayerListPacketPayload.json: Start Video Capture
		// (x-ordinal-index 0) with the frame rate (uint32) and the prefix
		// of the files (json/Start_Video_Capture.json), or Stop Video
		// Capture (1) with nothing. gophertunnel v1.62.0 wrote a byte,
		// stop 0 and start 1; it is the same in 1.26.40 to 1.26.61.
		name:  "PlayerVideoCapture",
		wrong: []string{"Action uint8", "if Action == 1", "\tFrameRate int32", "\tFilePrefix string"},
		right: []string{"Action varuint32", "if Action == 0", "\tFrameRate int32", "\tFilePrefix string"},
	},
	{
		// Mojang's json/CameraInstruction__SplineInstruction.json requires
		// totalTime, type (uint8), curve, progressKeyFrames and
		// rotationOption, and gives splineIdentifier ("") and loadFromJson
		// (false) defaults: a field of a default always goes, as the
		// Message of json/MessageOnly.json of the Text packet and the
		// RequestId of json/CommandOriginData.json do, and only one of
		// neither is optional, as the Redacted name of
		// json/Bedrock__Safety__RedactableString.json, which
		// StackResponseSlotInfo codes so. gophertunnel v1.62.0 made the
		// type, identifier and flag optional; it is the same in 1.26.40 to
		// 1.26.61, and #545 codes it so for 2225.
		name: "CameraSplineInstruction",
		wrong: []string{"TotalTime float32", "SplineType ?uint8", "Curve []vec3", "ProgressKeyFrames []CameraProgressOption",
			"RotationOptions []CameraRotationOption", "SplineIdentifier ?string", "LoadFromJson ?bool"},
		right: []string{"TotalTime float32", "SplineType uint8", "Curve []vec3", "ProgressKeyFrames []CameraProgressOption",
			"RotationOptions []CameraRotationOption", "SplineIdentifier string", "LoadFromJson bool"},
	},
	{
		// Mojang's json/CameraSplineDefinition.json gives spline_type a
		// default ("catmullrom"), so it always goes, and its key frames
		// are json/CameraSplineProgressKeyFrame.json and
		// json/CameraSplineRotationKeyFrame.json: a progress (float) or a
		// rotation (Vec3), a time (float) and an easing neither required
		// nor of a default, optional (as above). gophertunnel v1.62.0 made
		// the type optional and coded the key frames as those of a spline
		// instruction, of an easing always; it is the same in 1.26.40 to
		// 1.26.61, and #545 codes it so for 2225, with these names.
		name: "CameraSplineDefinition",
		wrong: []string{"Name string", "TotalTime float32", "SplineType ?string", "ControlPoints []vec3",
			"ProgressKeyFrames []CameraProgressOption", "RotationKeyFrames []CameraRotationOption"},
		right: []string{"Name string", "TotalTime float32", "SplineType string", "ControlPoints []vec3",
			"ProgressKeyFrames []CameraSplineProgressKeyFrame", "RotationKeyFrames []CameraSplineRotationKeyFrame"},
		add: []*decl{
			{name: "Optional", manual: "coded by hand: the easing of a key frame of a camera spline, if any, by its name"},
			{name: "CameraSplineProgressKeyFrame", lines: []string{"Value float32", "Time float32", "EaseType Optional"}},
			{name: "CameraSplineRotationKeyFrame", lines: []string{"Value vec3", "Time float32", "EaseType Optional"}},
		},
	},
}

// correct applies the corrections to the declarations imported, and
// returns the names of those it corrected. A correction of a declaration
// the schema has not fails: gophertunnel renamed or removed it.
func (im *importer) correct() ([]string, error) {
	var corrected []string
	for _, c := range corrections {
		i := slices.IndexFunc(im.decls, func(d *decl) bool { return d.name == c.name })
		if i < 0 {
			return nil, fmt.Errorf("correction of %s: no such declaration", c.name)
		}
		d := im.decls[i]
		if d.manual != "" || !slices.Equal(d.lines, c.wrong) {
			log.Printf("%s: gophertunnel no longer codes it as corrected: left as it is", c.name)
			continue
		}
		d.lines = slices.Clone(c.right)
		var add []*decl
		for _, a := range c.add {
			if !slices.ContainsFunc(im.decls, func(d *decl) bool { return d.name == a.name }) {
				add = append(add, &decl{name: a.name, lines: slices.Clone(a.lines), manual: a.manual})
			}
		}
		im.decls = slices.Insert(im.decls, i, add...)
		corrected = append(corrected, c.name)
	}
	return corrected, nil
}

// correctedLine is the line of the header of a schema that names the
// declarations corrected, empty for none.
func correctedLine(names []string) string {
	if len(names) == 0 {
		return ""
	}
	return "# Corrected after Mojang's protocol documentation: " + strings.Join(names, ", ") + ".\n"
}
