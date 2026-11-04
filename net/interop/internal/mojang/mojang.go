// Package mojang holds the constants gophertunnel gives values other than
// Mojang's protocol documentation (Mojang/bedrock-protocol-docs,
// json/*.json) does, for the protocol of each codec package: constimport
// writes Mojang's in their place, and the test of the constants against
// gophertunnel leaves them out.
package mojang

import "strings"

// Enum is the constants of an enum by Mojang's documentation: those whose
// names begin with Prefix, each the prefix and a name of Values, of its
// value. gophertunnel's constants of the prefix are replaced, those of no
// name of Values dropped.
type Enum struct {
	Prefix string
	Values map[string]int64
}

// Enums are the enums corrected, by codec package.
var Enums = map[string][]Enum{
	"v2193": {
		// Mojang's json/PlayerVideoCapturePacketPayload.json of v1.26.50
		// is a variant of Start Video Capture (x-ordinal-index 0) and
		// Stop Video Capture (1). gophertunnel v1.62.0 numbered them the
		// other way, stop 0 and start 1.
		{Prefix: "PlayerVideoCaptureAction", Values: map[string]int64{"Start": 0, "Stop": 1}},
	},
}

// Corrected returns whether a constant of a codec package is one of an
// enum corrected.
func Corrected(pkg, name string) bool {
	for _, e := range Enums[pkg] {
		if strings.HasPrefix(name, e.Prefix) {
			return true
		}
	}
	return false
}
