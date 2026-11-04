package session_test

import (
	"io"
	"log/slog"
	"slices"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/protocol/bedrock/session"
	"github.com/df-mc/dragonfly/server/world/sound"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
	"github.com/go-gl/mathgl/mgl64"
)

func TestJavaSoundsPlayTheirBedrockDefinitions(t *testing.T) {
	conn := &packetConn{packets: make(chan any, 64)}
	viewer := session.Config{Log: slog.New(slog.NewTextHandler(io.Discard, nil)), MaxChunkRadius: 1}.New(conn)
	t.Cleanup(viewer.CloseConnection)
	for _, name := range []string{
		"entity.cat.purr",
		// Bedrock parrots cannot imitate hoglins.
		"entity.parrot.imitate.hoglin",
		"item.spear.lunge_2",
		// Bedrock has no long jump sound of goats.
		"entity.goat.long_jump",
		"entity.goat.screaming.long_jump",
		"entity.panda.eat",
		"entity.strider.happy",
		// Bedrock definitions pass through as they are.
		"random.glass",
	} {
		viewer.ViewSound(mgl64.Vec3{}, sound.Custom{Name: name, Volume: 1, Pitch: 1})
	}
	want := []string{"mob.cat.purr", "item.enchant.lunge2", "mob.panda.eat", "mob.strider.tempt", "random.glass"}
	var got []string
	timeout := time.After(time.Second)
	for len(got) < len(want)+1 {
		select {
		case pk := <-conn.packets:
			if p, ok := pk.(*mc.PlaySoundPacket); ok {
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
