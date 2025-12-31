package packbuilder

import (
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/resource"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// buildManifest creates a JSON manifest file for the client to be able to read the resource pack. It creates
// basic information and writes it to the pack.
func buildManifest(dir string, headerUUID, moduleUUID uuid.UUID) error {
	version, err := parseVersion(protocol.CurrentVersion)
	if err != nil {
		return fmt.Errorf("parse game version: %w", err)
	}
	m, err := json.Marshal(resource.Manifest{
		FormatVersion: 2,
		Header: resource.Header{
			Name:               "dragonfly auto-generated resource pack",
			Description:        "This resource pack contains auto-generated content from dragonfly",
			UUID:               headerUUID,
			Version:            [3]int{0, 0, 1},
			MinimumGameVersion: version,
		},
		Modules: []resource.Module{
			{
				UUID:        moduleUUID.String(),
				Description: "This resource pack contains auto-generated content from dragonfly",
				Type:        "resources",
				Version:     [3]int{0, 0, 1},
			},
		},
	})
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), m, 0666); err != nil {
		return err
	}
	return nil
}

// parseVersion parses the version passed in the format of a.b.c as a [3]int.
func parseVersion(ver string) ([3]int, error) {
	frag := strings.Split(ver, ".")
	if len(frag) != 3 {
		return [3]int{}, fmt.Errorf("invalid version number %q", ver)
	}
	a, err := strconv.ParseInt(frag[0], 10, 64)
	if err != nil {
		return [3]int{}, fmt.Errorf("invalid major version %q: %w", frag[0], err)
	}
	b, err := strconv.ParseInt(frag[1], 10, 64)
	if err != nil {
		return [3]int{}, fmt.Errorf("invalid minor version %q: %w", frag[1], err)
	}
	c, err := strconv.ParseInt(frag[2], 10, 64)
	if err != nil {
		return [3]int{}, fmt.Errorf("invalid patch version %q: %w", frag[2], err)
	}
	return [3]int{int(a), int(b), int(c)}, nil
}
