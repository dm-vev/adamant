package resource

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Manifest is the manifest.json of a pack.
type Manifest struct {
	// FormatVersion is the version of the format of the manifest.
	FormatVersion int `json:"format_version"`
	// Header names the pack.
	Header Header `json:"header"`
	// Modules are what the pack holds.
	Modules []Module `json:"modules"`
	// Dependencies are the packs the pack needs.
	Dependencies []Dependency `json:"dependencies,omitempty"`
	// Capabilities are the features the pack uses.
	Capabilities []Capability `json:"capabilities,omitempty"`
	// Metadata describes who made the pack.
	Metadata *Metadata `json:"metadata,omitempty"`

	worldTemplate bool
}

// Capability is a feature a pack uses, such as "chemistry".
type Capability string

// Header names a pack.
type Header struct {
	Name               string  `json:"name"`
	Description        string  `json:"description"`
	UUID               UUID    `json:"uuid"`
	Version            Version `json:"version"`
	MinimumGameVersion Version `json:"min_engine_version"`
}

// Module is a part of a pack: resources, data, client_data or others.
type Module struct {
	UUID        string  `json:"uuid"`
	Description string  `json:"description"`
	Type        string  `json:"type"`
	Version     Version `json:"version"`
}

// Dependency is a pack another needs.
type Dependency struct {
	UUID    string  `json:"uuid"`
	Version Version `json:"version"`
}

// Metadata describes who made a pack.
type Metadata struct {
	Authors []string `json:"authors,omitempty"`
	License string   `json:"license,omitempty"`
	URL     string   `json:"url,omitempty"`
}

// Version is a version of three numbers, which a manifest writes as an
// array or as "x.y.z".
type Version [3]int

// UnmarshalJSON reads a version of either form.
func (v *Version) UnmarshalJSON(b []byte) error {
	var arr [3]int
	if err := json.Unmarshal(b, &arr); err == nil {
		*v = arr
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("invalid version: %s", string(b))
	}
	parts := strings.Split(strings.TrimSpace(s), ".")
	if len(parts) != 3 {
		return fmt.Errorf("invalid version %q (need x.y.z)", s)
	}
	for i := range 3 {
		n, err := strconv.Atoi(parts[i])
		if err != nil {
			return fmt.Errorf("invalid version component %q in %q", parts[i], s)
		}
		v[i] = n
	}
	return nil
}
