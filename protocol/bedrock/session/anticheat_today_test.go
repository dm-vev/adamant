package session

import (
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/df-mc/dragonfly/server/client"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/go-gl/mathgl/mgl64"
)

// TestAntiCheatTodayAdapterAppliesEveryInput pins that the session hands
// the game every PlayerAuthInput it handles, however many a client sends in
// a tick. Since "Hold clients to real time" the game defers the inputs of a
// client ahead of time (TestTimerDefersInputsAhead), and the session holds
// them, unchanged, for a later tick.
func TestAntiCheatTodayAdapterAppliesEveryInput(t *testing.T) {
	t.Parallel()
	c := newUnchangedControllable()
	moves := 0
	c.move = func(d mgl64.Vec3) {
		moves++
		c.position = c.position.Add(d)
	}
	s := &Session{conf: Config{Log: slog.New(slog.NewTextHandler(io.Discard, nil))}}
	for i := 1; i <= 5; i++ {
		to := mgl64.Vec3{0.2 * float64(i), 0, 0}
		pk := &v2193.PlayerAuthInputPacket{Position: vec64To32(entityNetworkPosition(c, to)), Tick: uint64(i)}
		if err := (PlayerAuthInputHandler{}).handleMovement(pk, s, nil, c); err != nil {
			t.Fatal(err)
		}
	}
	if moves != 5 {
		t.Fatalf("five inputs in one tick moved the player %d times, want 5", moves)
	}
	c.result = client.MoveDeferred
	pk := &v2193.PlayerAuthInputPacket{Position: vec64To32(entityNetworkPosition(c, mgl64.Vec3{2, 0, 0})), Tick: 6}
	before := *pk
	if err := (PlayerAuthInputHandler{}).Handle(pk, s, nil, c); !errors.Is(err, errInputDeferred) {
		t.Fatalf("a deferred input: %v, want errInputDeferred", err)
	}
	if pk.Position != before.Position || pk.Yaw != before.Yaw {
		t.Fatal("the session changed a deferred input, which it handles again")
	}
}
