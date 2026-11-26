package admin_test

import (
	"io"
	"log/slog"
	"path/filepath"
	"slices"
	"testing"

	"github.com/dm-vev/adamant/sdk/admin"
	"github.com/dm-vev/adamant/server"
	"github.com/dm-vev/adamant/server/player"
	"github.com/dm-vev/adamant/server/player/playerdb"
	"github.com/dm-vev/adamant/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

func TestAdminModerationAndOfflinePlayerData(t *testing.T) {
	dir := t.TempDir()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	prov, err := playerdb.NewProvider(filepath.Join(dir, "players"))
	if err != nil {
		t.Fatalf("playerdb.NewProvider: %v", err)
	}
	uc := server.DefaultConfig()
	uc.Whitelist.File = filepath.Join(dir, "white-list.txt")
	uc.World.Folder = filepath.Join(dir, "world")
	uc.Players.SaveData = false
	cfg, err := uc.Config(log)
	if err != nil {
		t.Fatalf("uc.Config: %v", err)
	}
	if cfg.WorldProvider != nil {
		_ = cfg.WorldProvider.Close()
	}
	cfg.WorldProvider = world.NopProvider{}
	cfg.PlayerProvider = prov
	cfg.Generator = func(world.Dimension, int64) world.Generator { return world.NopGenerator{} }
	cfg.Listeners = nil
	cfg.RandomTickSpeed = -1
	cfg.SaveInterval = -1
	srv := cfg.New()
	srv.Listen()
	t.Cleanup(func() { _ = srv.Close() })

	// Operators.
	if added, err := admin.Op(srv, "ModOne"); err != nil || !added {
		t.Fatalf("Op = (%v, %v)", added, err)
	}
	if !admin.IsOperator(srv, "ModOne") {
		t.Fatal("IsOperator(ModOne) = false")
	}
	if removed, err := admin.Deop(srv, "ModOne"); err != nil || !removed || admin.IsOperator(srv, "ModOne") {
		t.Fatalf("Deop = (%v, %v)", removed, err)
	}

	// Bans & IP bans.
	if added, err := admin.BanPlayer(srv, "Griefer", "griefing"); err != nil || !added {
		t.Fatalf("BanPlayer = (%v, %v)", added, err)
	}
	if reason, ok := admin.Banned(srv, "", "Griefer"); !ok || reason == "" {
		t.Fatalf("Banned(Griefer) = (%q, %v)", reason, ok)
	}
	if list, err := admin.BanList(srv, false); err != nil || len(list) == 0 {
		t.Fatalf("BanList = (%d, %v)", len(list), err)
	}
	if ok, err := admin.PardonPlayer(srv, "Griefer"); err != nil || !ok {
		t.Fatalf("PardonPlayer = (%v, %v)", ok, err)
	}

	if added, err := admin.BanIP(srv, "203.0.113.5", "spam"); err != nil || !added {
		t.Fatalf("BanIP = (%v, %v)", added, err)
	}
	if _, ok := admin.Banned(srv, "203.0.113.5:19132", ""); !ok {
		t.Fatal("expected IP 203.0.113.5 banned")
	}
	if ok, err := admin.PardonIP(srv, "203.0.113.5"); err != nil || !ok {
		t.Fatalf("PardonIP = (%v, %v)", ok, err)
	}

	// Whitelist.
	if err := admin.SetWhitelist(srv, true); err != nil || !admin.WhitelistEnabled(srv) {
		t.Fatalf("SetWhitelist = %v enabled=%v", err, admin.WhitelistEnabled(srv))
	}
	if added, err := admin.WhitelistAdd(srv, "VIPPlayer"); err != nil || !added {
		t.Fatalf("WhitelistAdd = (%v, %v)", added, err)
	}
	entries, err := admin.WhitelistEntries(srv)
	if err != nil || !slices.Contains(entries, "vipplayer") {
		t.Fatalf("WhitelistEntries = (%v, %v)", entries, err)
	}
	if removed, err := admin.WhitelistRemove(srv, "VIPPlayer"); err != nil || !removed {
		t.Fatalf("WhitelistRemove = (%v, %v)", removed, err)
	}

	// Offline player data save, edit, and reload.
	id := uuid.New()
	initial := player.Config{
		UUID:     id,
		Name:     "OfflineHero",
		Position: mgl64.Vec3{1, 64, 1},
		Health:   20,
	}
	if err := admin.SaveOffline(srv, id, initial, srv.World()); err != nil {
		t.Fatalf("SaveOffline: %v", err)
	}
	if err := admin.EditOffline(srv, id, func(data *player.Config, _ **world.World) error {
		data.Position = mgl64.Vec3{50, 80, 50}
		return nil
	}); err != nil {
		t.Fatalf("EditOffline: %v", err)
	}
	loaded, _, err := admin.LoadOffline(srv, id)
	if err != nil || loaded.Position != (mgl64.Vec3{50, 80, 50}) {
		t.Fatalf("LoadOffline = (%v, %v)", loaded.Position, err)
	}
}
