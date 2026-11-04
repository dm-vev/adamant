package server

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/identity"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/google/uuid"
)

// TestOfflineRecordMovesFromTheClientUUID checks that on a server keyed by
// name a player whose record the server kept by the UUID of its client,
// before it kept records by name, gets it at its first login, and that the
// server saves it by the name as the player leaves; the client keeps its
// UUID.
func TestOfflineRecordMovesFromTheClientUUID(t *testing.T) {
	client := uuid.MustParse("00000000-0000-4000-8000-00000000057e")
	provider := &recordProvider{records: map[uuid.UUID]player.Config{client: {Name: "Steve", UUID: client, Experience: 1234}}}
	saves := &joinSaveProvider{}
	jt := newJoinTest(t, struct {
		player.Provider
	}{movingProvider{recordProvider: provider, saves: saves}}, Config{})
	var logged bytes.Buffer
	jt.srv.conf.Log = slog.New(slog.NewTextHandler(&logged, &slog.HandlerOptions{ReplaceAttr: noTime}))
	jt.start()
	j := joinLogin("steve", "")
	j.L.Identity.UUID = client
	h := jt.join(t, j)
	if h.UUID() != client {
		t.Errorf("the player has UUID %v, want the one its client sent", h.UUID())
	}
	conf, _ := player.Call(context.Background(), h, func(_ *world.Tx, p *player.Player) (player.Config, error) {
		return p.Data(), nil
	})
	if conf.Experience != 1234 || conf.DataUUID != identity.OfflineUUID("steve") {
		t.Errorf("the player has %d experience and data UUID %v", conf.Experience, conf.DataUUID)
	}
	if want := `msg="Moved the data of steve from the UUID its client sent."`; !strings.Contains(logged.String(), want) {
		t.Errorf("logged\n%s\nwant %s", logged.String(), want)
	}
	if err := player.Do(h, func(_ *world.Tx, p *player.Player) { p.Disconnect("bye") }).Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := saves.saved(); !slices.Equal(got, []uuid.UUID{identity.OfflineUUID("Steve")}) {
		t.Errorf("saved %v, want the record by the name", got)
	}
}

// TestOfflineSpawnPointMoves checks that the spawn point the world kept by
// the UUID of the client moves to the name with the record, unless the
// name has one, and not for a client that sends the UUID of another
// player (Mallory sends the UUID of Carol); the player then sets its spawn
// point by the name.
func TestOfflineSpawnPointMoves(t *testing.T) {
	steve := uuid.MustParse("00000000-0000-4000-8000-00000000057e")
	alex := uuid.MustParse("00000000-0000-4000-8000-0000000000a1")
	carol := uuid.MustParse("00000000-0000-4000-8000-00000000ca01")
	worlds := &spawnProvider{spawns: map[uuid.UUID]cube.Pos{steve: {10, 64, 10}, alex: {20, 64, 20}, identity.OfflineUUID("alex"): {30, 64, 30}, carol: {40, 64, 40}}}
	provider := &recordProvider{records: map[uuid.UUID]player.Config{
		steve: {Name: "Steve", UUID: steve}, alex: {Name: "Alex", UUID: alex}, carol: {Name: "Carol", UUID: carol},
	}}
	jt := newJoinTest(t, provider, Config{WorldProvider: worlds})
	jt.start()
	for _, c := range []struct {
		name string
		id   uuid.UUID
	}{{"Steve", steve}, {"Alex", alex}, {"Mallory", carol}} {
		j := joinLogin(c.name, "")
		j.L.Identity.UUID = c.id
		h := jt.join(t, j)
		if c.name == "Steve" {
			if err := player.Do(h, func(_ *world.Tx, p *player.Player) { p.SetRespawnPosition(cube.Pos{11, 65, 11}, world.Overworld) }).Wait(context.Background()); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, c := range []struct {
		key  uuid.UUID
		want cube.Pos
		ok   bool
	}{
		{identity.OfflineUUID("steve"), cube.Pos{11, 65, 11}, true},
		{steve, cube.Pos{10, 64, 10}, true},
		{identity.OfflineUUID("alex"), cube.Pos{30, 64, 30}, true},
		{identity.OfflineUUID("mallory"), cube.Pos{}, false},
	} {
		if pos, ok, _ := worlds.LoadPlayerSpawnPosition(c.key); ok != c.ok || pos != c.want {
			t.Errorf("spawn point of %v: %v %t, want %v %t", c.key, pos, ok, c.want, c.ok)
		}
	}
}

// movingProvider loads as its recordProvider and saves as saves.
type movingProvider struct {
	*recordProvider
	saves *joinSaveProvider
}

func (p movingProvider) Save(id uuid.UUID, d player.Config, w *world.World) error {
	return p.saves.Save(id, d, w)
}

// spawnProvider is a world.Provider that keeps the spawn points of players
// in memory.
type spawnProvider struct {
	world.NopProvider
	mu     sync.Mutex
	spawns map[uuid.UUID]cube.Pos
}

func (p *spawnProvider) LoadPlayerSpawnPosition(id uuid.UUID) (cube.Pos, bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	pos, ok := p.spawns[id]
	return pos, ok, nil
}

func (p *spawnProvider) SavePlayerSpawnPosition(id uuid.UUID, pos cube.Pos) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.spawns[id] = pos
	return nil
}

var _ io.Closer = (*spawnProvider)(nil)

// TestXUIDSameAccountIsAReconnect checks that on a server keyed by XUID a
// login of an account online, under another UUID or name, is held to the
// reconnect policy, which refuses it by default.
func TestXUIDSameAccountIsAReconnect(t *testing.T) {
	jt := newJoinTest(t, player.NopProvider{}, Config{IdentityKey: identity.XUID})
	jt.start()
	first, second := joinLogin("Steve", steveXUID), joinLogin("Stephen", steveXUID)
	first.L.Identity.Authenticated, second.L.Identity.Authenticated = true, true
	jt.join(t, first)
	jt.l.Send(second)
	awaitRejected(t, second, "Already logged in.")
	if n := jt.srv.PlayerCount(); n != 1 {
		t.Errorf("%d players online, want one", n)
	}
}
