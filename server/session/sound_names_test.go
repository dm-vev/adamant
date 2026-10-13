package session_test

import (
	"io"
	"log/slog"
	"slices"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/session"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

func TestJavaSoundsPlayTheirBedrockDefinitions(t *testing.T) {
	conn := &packetConn{packets: make(chan packet.Packet, 64)}
	viewer := session.Config{Log: slog.New(slog.NewTextHandler(io.Discard, nil)), MaxChunkRadius: 1}.New(conn)
	t.Cleanup(viewer.CloseConnection)
	for _, name := range []string{
		"entity.cat.purr",
		// Bedrock wolves make no sound when eating.
		"entity.wolf.eat",
		"item.spear.lunge_2",
		// Bedrock definitions pass through as they are.
		"random.glass",
	} {
		viewer.ViewSound(mgl64.Vec3{}, sound.Custom{Name: name, Volume: 1, Pitch: 1})
	}
	want := []string{"mob.cat.purr", "item.enchant.lunge2", "random.glass"}
	var got []string
	timeout := time.After(time.Second)
	for len(got) < len(want)+1 {
		select {
		case pk := <-conn.packets:
			if p, ok := pk.(*packet.PlaySound); ok {
				got = append(got, p.SoundName)
			}
		case <-timeout:
			if !slices.Equal(got, want) {
				t.Fatalf("played %v, want %v", got, want)
			}
			return
		}
	}
	t.Fatalf("played %v, want %v", got, want)
}
