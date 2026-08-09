package player

import (
	"context"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"log/slog"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/session"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/google/uuid"
	"github.com/sandertv/gophertunnel/minecraft"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/login"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

func TestReleaseItemResyncsOnlyWhenNothingWasConsumed(t *testing.T) {
	tests := []struct {
		name                  string
		arrows                int
		useDuration           time.Duration
		handler               Handler
		wantInventoryContents int
		wantOffHandContents   int
		wantArrowConsumed     bool
	}{
		{name: "cancelled", arrows: 1, useDuration: time.Second, handler: cancelReleaseHandler{}, wantInventoryContents: 1, wantOffHandContents: 1},
		{name: "too short", arrows: 1, wantInventoryContents: 1, wantOffHandContents: 2},
		{name: "missing ammo", useDuration: time.Second, wantInventoryContents: 1, wantOffHandContents: 1},
		{name: "consumed", arrows: 1, useDuration: time.Second, wantOffHandContents: 1, wantArrowConsumed: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			conn := &releaseTestConn{}
			s := session.Config{Log: slog.New(slog.NewTextHandler(io.Discard, nil)), MaxChunkRadius: 1}.New(conn)
			t.Cleanup(s.CloseConnection)

			w := world.Config{Entities: entity.DefaultRegistry}.New()
			t.Cleanup(func() { _ = w.Close() })
			inv := inventory.New(36, nil)
			_ = inv.SetItem(0, item.NewStack(item.Bow{}, 1))
			if test.arrows > 0 {
				_ = inv.SetItem(1, item.NewStack(item.Arrow{}, test.arrows))
			}
			handle := world.EntitySpawnOpts{ID: uuid.New()}.New(Type, Config{Session: s, Inventory: inv})

			if err := w.Do(func(tx *world.Tx) {
				tx.AddEntity(handle)
			}).Wait(context.Background()); err != nil {
				t.Fatalf("add player: %v", err)
			}
			s.SendMessage("release-item-test-ready")
			conn.waitForMessage(t, "release-item-test-ready")
			conn.reset()

			if err := w.Do(func(tx *world.Tx) {
				entity, ok := handle.Entity(tx)
				if !ok {
					t.Fatal("player is not in world")
				}
				p := entity.(*Player)
				held, _ := p.HeldItems()
				p.startItemUse(held)
				p.usingSince = time.Now().Add(-test.useDuration)
				if test.handler != nil {
					p.Handle(test.handler)
				}
				p.ReleaseItem()
			}).Wait(context.Background()); err != nil {
				t.Fatalf("release item: %v", err)
			}

			const marker = "release-item-test-marker"
			s.SendMessage(marker)
			conn.waitForMessage(t, marker)
			if got := conn.inventoryContents(protocol.WindowIDInventory); got != test.wantInventoryContents {
				t.Errorf("main inventory packets = %d, want %d", got, test.wantInventoryContents)
			}
			if got := conn.inventoryContents(protocol.WindowIDOffHand); got != test.wantOffHandContents {
				t.Errorf("off-hand inventory packets = %d, want %d", got, test.wantOffHandContents)
			}
			if test.wantArrowConsumed {
				arrow, _ := inv.Item(1)
				if !arrow.Empty() {
					t.Errorf("arrow was not consumed: %v", arrow)
				}
			}
		})
	}
}

type cancelReleaseHandler struct{ NopHandler }

func (cancelReleaseHandler) HandleItemRelease(ctx *Context, _ item.Stack, _ time.Duration) {
	ctx.Cancel()
}

type releaseTestConn struct {
	mu      sync.Mutex
	packets []packet.Packet
}

func (*releaseTestConn) Close() error { return nil }

func (*releaseTestConn) IdentityData() login.IdentityData {
	return login.IdentityData{Identity: uuid.NewString(), DisplayName: "Test"}
}

func (*releaseTestConn) ClientData() login.ClientData { return login.ClientData{} }
func (*releaseTestConn) ClientCacheEnabled() bool     { return false }
func (*releaseTestConn) ChunkRadius() int             { return 1 }
func (*releaseTestConn) Latency() time.Duration       { return 0 }
func (*releaseTestConn) Flush() error                 { return nil }
func (*releaseTestConn) RemoteAddr() net.Addr         { return &net.TCPAddr{} }
func (*releaseTestConn) ReadPacket() (packet.Packet, error) {
	return nil, io.EOF
}
func (c *releaseTestConn) WritePacket(pk packet.Packet) error {
	c.mu.Lock()
	c.packets = append(c.packets, pk)
	c.mu.Unlock()
	return nil
}
func (*releaseTestConn) StartGameContext(context.Context, minecraft.GameData) error { return nil }

func (c *releaseTestConn) waitForMessage(t *testing.T, marker string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		for _, pk := range c.packets {
			if text, ok := pk.(*packet.Text); ok && text.Message == marker {
				c.mu.Unlock()
				return
			}
		}
		c.mu.Unlock()
		time.Sleep(time.Millisecond)
	}
	t.Fatal("timed out waiting for packet writer")
}

func (c *releaseTestConn) inventoryContents(windowID uint32) int {
	c.mu.Lock()
	defer c.mu.Unlock()

	count := 0
	for _, pk := range c.packets {
		if content, ok := pk.(*packet.InventoryContent); ok && content.WindowID == windowID {
			count++
		}
	}
	return count
}

func (c *releaseTestConn) reset() {
	c.mu.Lock()
	c.packets = nil
	c.mu.Unlock()
}
