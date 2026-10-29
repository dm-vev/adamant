package server

import (
	"context"
	"testing"
	"time"

	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/sandertv/gophertunnel/minecraft"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/login"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// TestRakNetConnCarriesCodecPackets runs a session connection over
// RakNet: a gophertunnel client reads what the codecs of protocol 2193
// encode, a shield with the blocking tick only a shield has among them,
// and the server decodes what the client sends.
func TestRakNetConnCarriesCodecPackets(t *testing.T) {
	ml, err := minecraft.ListenConfig{AuthenticationDisabled: true}.Listen("raknet", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	l := listener{ml}
	defer l.Close()
	const shield = 355
	echoed := make(chan string, 1)
	go func() {
		c, err := l.Accept()
		if err != nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		data := minecraft.GameData{EntityRuntimeID: 1, EntityUniqueID: 1, Items: []protocol.ItemEntry{{Name: "minecraft:shield", RuntimeID: shield}}}
		if err := c.StartGameContext(ctx, data); err != nil {
			t.Errorf("start game: %v", err)
			return
		}
		_ = c.WritePacket(&v2193.InventorySlotPacket{WindowID: 0, Slot: 3, NewItem: v2193.ItemInstance{
			StackNetworkID: 9,
			Stack:          v2193.ItemStack{NetworkID: shield, Count: 1, BlockingTick: 7},
		}})
		_ = c.Flush()
		for {
			pk, err := c.ReadPacket()
			if err != nil {
				return
			}
			if text, ok := pk.(*v2193.TextPacket); ok {
				echoed <- text.Message
				return
			}
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	conn, err := minecraft.Dialer{IdentityData: login.IdentityData{DisplayName: "Bot"}}.DialContext(ctx, "raknet", ml.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.DoSpawnContext(ctx); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	for {
		pk, err := conn.ReadPacket()
		if err != nil {
			t.Fatal(err)
		}
		if slot, ok := pk.(*packet.InventorySlot); ok {
			if s := slot.NewItem.Stack; slot.Slot != 3 || s.NetworkID != shield || s.Count != 1 || s.BlockingTick != 7 {
				t.Fatalf("slot %d holds %+v", slot.Slot, s)
			}
			break
		}
	}
	if err := conn.WritePacket(&packet.Text{TextType: packet.TextTypeChat, SourceName: "Bot", Message: "hello"}); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-echoed:
		if got != "hello" {
			t.Fatalf("server read %q", got)
		}
	case <-ctx.Done():
		t.Fatal("server did not read the text")
	}
}
