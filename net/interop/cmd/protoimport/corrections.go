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
