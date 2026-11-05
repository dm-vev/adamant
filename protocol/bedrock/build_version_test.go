package bedrockadapter

import (
	"slices"
	"testing"

	"github.com/df-mc/dragonfly/protocol/bedrock/internal/versions"
	"github.com/df-mc/dragonfly/server/version"
	"github.com/df-mc/dragonfly/server/world/mcdb/leveldat"
)

// TestBuildVersionIsNewestPrimary checks that the release the server calls
// itself, version.Minecraft, is the version of the newest protocol a config
// may name as the primary, and that the NetworkVersion and the version numbers
// level.dat stores are those of that protocol. server/ may not import
// protocol/, so a new newest protocol is noticed here: the test fails until
// version.Minecraft, leveldat.NetworkVersion and the pinned level.dat are
// bumped with it.
func TestBuildVersionIsNewestPrimary(t *testing.T) {
	newest := versions.NewestPrimary()
	if version.Minecraft != newest.Version {
		t.Errorf("version.Minecraft = %q, the newest primary protocol %d is of %q", version.Minecraft, newest.Number, newest.Version)
	}
	if !slices.Contains(newest.Names, version.Minecraft) {
		t.Errorf("version.Minecraft = %q is not a version of protocol %d (%v)", version.Minecraft, newest.Number, newest.Names)
	}
	if int32(leveldat.NetworkVersion) != newest.Number {
		t.Errorf("leveldat.NetworkVersion = %d, the newest primary protocol is %d", leveldat.NetworkVersion, newest.Number)
	}
	var d leveldat.Data
	d.FillDefault()
	numbers := version.MinecraftNumbers()
	if d.InventoryVersion != version.Minecraft || d.NetworkVersion != newest.Number ||
		!slices.Equal(d.MinimumCompatibleClientVersion, numbers) || !slices.Equal(d.LastOpenedWithVersion, numbers) {
		t.Errorf("default level.dat has InventoryVersion %q, NetworkVersion %d, MinimumCompatibleClientVersion %v, LastOpenedWithVersion %v",
			d.InventoryVersion, d.NetworkVersion, d.MinimumCompatibleClientVersion, d.LastOpenedWithVersion)
	}
}
