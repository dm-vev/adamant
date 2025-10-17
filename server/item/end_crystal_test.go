package item_test

import (
	"context"
	"io"
	"net"
	"runtime"
	"sync"
	"testing"
	"time"
	_ "unsafe"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/player/skin"
	"github.com/df-mc/dragonfly/server/session"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
	"github.com/sandertv/gophertunnel/minecraft"
	"github.com/sandertv/gophertunnel/minecraft/protocol/login"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

func init() {
	worldFinaliseBlockRegistry()
}

//go:linkname worldFinaliseBlockRegistry github.com/df-mc/dragonfly/server/world.finaliseBlockRegistry
func worldFinaliseBlockRegistry()

func TestEndCrystalUseOnBlockReturns(t *testing.T) {
	w := world.Config{Entities: entity.DefaultRegistry}.New()
	defer w.Close()

	loader := world.NewLoader(2, w, world.NopViewer{})
	defer func() {
		<-w.Exec(func(tx *world.Tx) {
			loader.Close(tx)
		})
	}()

	done := make(chan struct{})
	go func() {
		<-w.Exec(func(tx *world.Tx) {
			base := cube.Pos{0, 64, 0}
			tx.SetBlock(base, block.Obsidian{}, nil)

			loader.Move(tx, base.Vec3Centre())
			loader.Load(tx, 1)

			ctx := item.UseContext{}
			if !(item.EndCrystal{}).UseOnBlock(base, cube.FaceUp, mgl64.Vec3{}, tx, nil, &ctx) {
				t.Errorf("use on block failed")
			}
		})
		close(done)
	}()

	timer := time.AfterFunc(2*time.Second, func() {
		buf := make([]byte, 1<<16)
		n := runtime.Stack(buf, true)
		panic("end crystal placement transaction timed out:\n" + string(buf[:n]))
	})

	<-done
	timer.Stop()
}

func TestEndCrystalPlacementByPlayer(t *testing.T) {
	w := world.Config{Entities: entity.DefaultRegistry}.New()
	defer w.Close()

	conn := newFakeConn()
	sess := (session.Config{MaxChunkRadius: 8}).New(conn)

	playerSkin := skin.New(64, 64)
	playerConf := player.Config{
		Session:  sess,
		Name:     "TestPlayer",
		UUID:     uuid.New(),
		Skin:     playerSkin,
		Position: mgl64.Vec3{0, 64, 0},
	}

	handle := world.NewEntity(player.Type, playerConf)

	done := make(chan struct{})
	go func() {
		<-w.Exec(func(tx *world.Tx) {
			tx.SetBlock(cube.Pos{0, 63, 0}, block.Obsidian{}, nil)

			ent := tx.AddEntity(handle)
			pl := ent.(*player.Player)

			sess.SetHandle(handle, playerSkin)
			sess.Spawn(pl, tx)

			pl.Inventory().SetItem(0, item.NewStack(item.EndCrystal{}, 1))
			_ = pl.SetHeldSlot(0)

			pl.UseItemOnBlock(cube.Pos{0, 63, 0}, cube.FaceUp, mgl64.Vec3{})
		})
		close(done)
	}()

	timer := time.AfterFunc(2*time.Second, func() {
		buf := make([]byte, 1<<16)
		n := runtime.Stack(buf, true)
		panic("player end crystal placement timed out:\n" + string(buf[:n]))
	})

	<-done
	timer.Stop()
}

type fakeConn struct {
	packetsMu sync.Mutex
	packets   []packet.Packet
	readBlock chan struct{}
}

func newFakeConn() *fakeConn {
	return &fakeConn{readBlock: make(chan struct{})}
}

func (f *fakeConn) Close() error { return nil }

func (f *fakeConn) IdentityData() login.IdentityData {
	return login.IdentityData{
		DisplayName: "TestPlayer",
		Identity:    uuid.New().String(),
		XUID:        "0",
	}
}

func (f *fakeConn) ClientData() login.ClientData { return login.ClientData{} }

func (f *fakeConn) ClientCacheEnabled() bool { return false }

func (f *fakeConn) ChunkRadius() int { return 8 }

func (f *fakeConn) Latency() time.Duration { return 0 }

func (f *fakeConn) Flush() error { return nil }

func (f *fakeConn) RemoteAddr() net.Addr { return fakeAddr("test") }

func (f *fakeConn) ReadPacket() (packet.Packet, error) {
	<-f.readBlock
	return nil, io.EOF
}

func (f *fakeConn) WritePacket(pk packet.Packet) error {
	f.packetsMu.Lock()
	f.packets = append(f.packets, pk)
	f.packetsMu.Unlock()
	return nil
}

func (f *fakeConn) StartGameContext(context.Context, minecraft.GameData) error { return nil }

type fakeAddr string

func (f fakeAddr) Network() string { return "fake" }
func (f fakeAddr) String() string  { return string(f) }
