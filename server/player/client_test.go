package player

import (
	"errors"
	"log/slog"
	"slices"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/client"
	"github.com/df-mc/dragonfly/server/client/clienttest"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/player/chat"
	"github.com/df-mc/dragonfly/server/player/scoreboard"
	"github.com/df-mc/dragonfly/server/player/skin"
	"github.com/df-mc/dragonfly/server/player/title"
	"github.com/df-mc/dragonfly/server/text"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// clientTestWorld returns a synchronous world with a loader around the
// origin whose viewer, a Recorder, stands for another player that sees the
// player of a test.
func clientTestWorld(t *testing.T) (*world.World, *clienttest.Recorder) {
	t.Helper()
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Log: slog.New(slog.DiscardHandler), Entities: entity.DefaultRegistry}.New()
	viewer := clienttest.New()
	loader := world.NewLoader(2, w, viewer)
	t.Cleanup(func() {
		<-w.Exec(func(tx *world.Tx) { loader.Close(tx) })
		_ = w.Close()
	})
	<-w.Exec(func(tx *world.Tx) {
		loader.Move(tx, mgl64.Vec3{0.5, 100, 0.5})
		loader.Load(tx, 25)
	})
	return w, viewer
}

// withClientPlayer runs f with a player in w that has the client c, none if
// c is nil.
func withClientPlayer(t *testing.T, w *world.World, c client.Client, f func(tx *world.Tx, p *Player)) {
	t.Helper()
	pos := mgl64.Vec3{0.5, 100, 0.5}
	conf := Config{Position: pos, GameMode: world.GameModeSurvival}
	if c != nil {
		conf.Client = c
	}
	handle := world.EntitySpawnOpts{Position: pos, ID: uuid.New()}.New(Type, conf)
	<-w.Exec(func(tx *world.Tx) {
		f(tx, tx.AddEntity(handle).(*Player))
	})
}

// argText returns the text.Text argument i of call.
func argText(t *testing.T, call clienttest.Call, i int) text.Text {
	t.Helper()
	tx, ok := call.Args[i].(text.Text)
	if !ok {
		t.Fatalf("%s argument %d is %T, not text.Text", call.Method, i, call.Args[i])
	}
	return tx
}

func methods(calls []clienttest.Call) []string {
	names := make([]string, len(calls))
	for i, c := range calls {
		names[i] = c.Method
	}
	return names
}

func TestPlayerSendsMessagesAsText(t *testing.T) {
	w, _ := clientTestWorld(t)
	r := clienttest.New()
	withClientPlayer(t, w, r, func(tx *world.Tx, p *Player) {
		r.Reset()
		p.Message("§cHi", 3)
		lone := text.Colourf("<red>lone</red>")
		p.Message(lone)
		p.Messaget(chat.MessageJoin, "Steve")

		calls := r.CallsTo("SendMessage")
		if len(calls) != 3 {
			t.Fatalf("SendMessage calls = %d, want 3", len(calls))
		}
		if got, want := argText(t, calls[0], 0), text.ParseLegacy("§cHi 3"); !text.Equal(got, want) {
			t.Errorf("Message(\"§cHi\", 3) sent %q, want %q", got.Legacy(), want.Legacy())
		}
		if got := argText(t, calls[1], 0); !text.Equal(got, lone) {
			t.Errorf("Message(lone) sent %q, want the text as it is", got.Legacy())
		}
		if got, want := argText(t, calls[2], 0), chat.MessageJoin.F("Steve").Text(p.Locale()); !text.Equal(got, want) {
			t.Errorf("Messaget(MessageJoin, Steve) sent %q, want %q", got.Legacy(), want.Legacy())
		}
	})
}

func TestPlayerSendTitleCalls(t *testing.T) {
	w, _ := clientTestWorld(t)
	r := clienttest.New()
	withClientPlayer(t, w, r, func(tx *world.Tx, p *Player) {
		cases := []struct {
			name  string
			title title.Title
			want  []string
		}{
			{"text", title.New("a"), []string{"SetTitleDurations", "SendTitle"}},
			{"subtitle", title.New().WithSubtitle("b"), []string{"SetTitleDurations", "SendTitle", "SendSubtitle"}},
			{"action text", title.New().WithActionText("c"), []string{"SetTitleDurations", "SendActionBarMessage"}},
		}
		for _, c := range cases {
			r.Reset()
			p.SendTitle(c.title)
			if got := methods(r.Calls()); !slices.Equal(got, c.want) {
				t.Errorf("%s: calls = %v, want %v", c.name, got, c.want)
			}
		}
	})
}

func TestPlayerSendsToastsAndScoreboards(t *testing.T) {
	w, _ := clientTestWorld(t)
	r := clienttest.New()
	withClientPlayer(t, w, r, func(tx *world.Tx, p *Player) {
		r.Reset()
		message := text.Literal("message")
		p.SendToast("§atitle", message)
		calls := r.CallsTo("SendToast")
		if len(calls) != 1 {
			t.Fatalf("SendToast calls = %d, want 1", len(calls))
		}
		if got := argText(t, calls[0], 0); !text.Equal(got, text.ParseLegacy("§atitle")) {
			t.Errorf("toast title = %q, want §atitle", got.Legacy())
		}
		if got := argText(t, calls[0], 1); !text.Equal(got, message) {
			t.Errorf("toast message = %q, want the text as it is", got.Legacy())
		}

		sb := scoreboard.New("board")
		p.SendScoreboard(sb)
		if calls := r.CallsTo("SendScoreboard"); len(calls) != 1 || calls[0].Args[0] != sb {
			t.Errorf("SendScoreboard calls = %v, want one with the board", calls)
		}
	})
}

func TestPlayerDropsCommandMessagesWithoutFeedback(t *testing.T) {
	w, _ := clientTestWorld(t)
	if err := w.SetGameRule("send_command_feedback", false); err != nil {
		t.Fatal(err)
	}
	r := clienttest.New()
	withClientPlayer(t, w, r, func(tx *world.Tx, p *Player) {
		r.Reset()
		var o cmd.Output
		o.Print("done")
		o.Errorf("failed")
		p.SendCommandOutput(&o)
		calls := r.CallsTo("SendCommandOutput")
		if len(calls) != 1 {
			t.Fatalf("SendCommandOutput calls = %d, want 1", len(calls))
		}
		out := calls[0].Args[0].(client.CommandOutput)
		if len(out.Messages) != 1 || out.Messages[0].Success || out.Messages[0].Text.Plain() != "failed" {
			t.Errorf("output = %+v, want only the error", out)
		}
	})
}

// A player with a client has its client show every client the skin; one
// without shows it to its viewers.
func TestPlayerSetSkin(t *testing.T) {
	sk := skin.New(1, 1)
	t.Run("client", func(t *testing.T) {
		w, viewer := clientTestWorld(t)
		r := clienttest.New()
		withClientPlayer(t, w, r, func(tx *world.Tx, p *Player) {
			viewer.Reset()
			p.SetSkin(sk)
			if len(r.CallsTo("ShowSkin")) != 1 {
				t.Error("client was not told to show the skin")
			}
			if len(viewer.CallsTo("ViewSkin")) != 0 {
				t.Error("viewer was shown the skin by the player")
			}
		})
	})
	t.Run("none", func(t *testing.T) {
		w, viewer := clientTestWorld(t)
		withClientPlayer(t, w, nil, func(tx *world.Tx, p *Player) {
			viewer.Reset()
			p.SetSkin(sk)
			if len(viewer.CallsTo("ViewSkin")) != 1 {
				t.Error("viewer was not shown the skin")
			}
		})
	})
}

type cancelMoveHandler struct{ NopHandler }

func (cancelMoveHandler) HandleMove(ctx *Context, _ mgl64.Vec3, _ cube.Rotation) { ctx.Cancel() }

// A cancelled move puts the player back on its client, which moved it; a
// player without a client never moved.
func TestPlayerCancelledMove(t *testing.T) {
	for _, withClient := range []bool{true, false} {
		w, viewer := clientTestWorld(t)
		var c client.Client
		if withClient {
			c = clienttest.New()
		}
		withClientPlayer(t, w, c, func(tx *world.Tx, p *Player) {
			p.Handle(cancelMoveHandler{})
			viewer.Reset()
			p.Move(mgl64.Vec3{1, 0, 0}, 0, 0)
			if got := len(viewer.CallsTo("ViewEntityTeleport")) == 1; got != withClient {
				t.Errorf("client %v: teleported = %v", withClient, got)
			}
		})
	}
}

// The server moves a player without a client: it keeps its velocity and
// falls as it ticks. The client of a player moves it.
func TestPlayerPhysicsOnlyWithoutClient(t *testing.T) {
	for _, withClient := range []bool{true, false} {
		w, viewer := clientTestWorld(t)
		r := clienttest.New()
		var c client.Client
		if withClient {
			c = r
		}
		withClientPlayer(t, w, c, func(tx *world.Tx, p *Player) {
			viewer.Reset()
			vel := mgl64.Vec3{0, 0.5, 0}
			p.SetVelocity(vel)
			if got := p.Velocity() == vel; got == withClient {
				t.Errorf("client %v: velocity kept = %v", withClient, got)
			}
			if withClient && len(r.CallsTo("ViewEntityVelocity")) != 1 {
				t.Error("client was not shown the velocity")
			}
			p.SetVelocity(mgl64.Vec3{})

			start := p.Position()
			p.Tick(tx, 1)
			if moved := p.Position() != start; moved == withClient {
				t.Errorf("client %v: moved by the server = %v", withClient, moved)
			}
		})
	}
}

// Only a client predicts blocks that the player must correct.
func TestPlayerResendsBlocksOnlyToClient(t *testing.T) {
	w, _ := clientTestWorld(t)
	r := clienttest.New()
	r.Answers.ChunkRadius = 4
	withClientPlayer(t, w, r, func(tx *world.Tx, p *Player) {
		r.Reset()
		p.resendNearbyBlock(cube.PosFromVec3(p.Position()))
		if len(r.CallsTo("ViewBlockUpdate")) == 0 {
			t.Error("client was not shown the block")
		}
	})
	withClientPlayer(t, w, nil, func(tx *world.Tx, p *Player) {
		p.resendNearbyBlocks(cube.PosFromVec3(p.Position()), cube.FaceUp)
	})
}

func TestPlayerHideEntityIgnoresItself(t *testing.T) {
	w, _ := clientTestWorld(t)
	r := clienttest.New()
	withClientPlayer(t, w, r, func(tx *world.Tx, p *Player) {
		r.Reset()
		p.HideEntity(p)
		if calls := r.CallsTo("StopShowingEntity"); len(calls) != 0 {
			t.Errorf("hiding the player from itself reached its client: %v", calls)
		}
		other := tx.AddEntity(world.EntitySpawnOpts{Position: p.Position()}.New(Type, Config{Position: p.Position()}))
		p.HideEntity(other)
		if calls := r.CallsTo("StopShowingEntity"); len(calls) != 1 {
			t.Errorf("StopShowingEntity calls = %d, want 1", len(calls))
		}
	})
}

func TestPlayerNetStats(t *testing.T) {
	w, _ := clientTestWorld(t)
	r := clienttest.New()
	r.Answers.NetStats = client.NetStats{Queued: 3}
	withClientPlayer(t, w, r, func(tx *world.Tx, p *Player) {
		if s, ok := p.NetStats(); !ok || s.Queued != 3 {
			t.Errorf("NetStats() = %+v, %v; want the client's, true", s, ok)
		}
	})
	withClientPlayer(t, w, nil, func(tx *world.Tx, p *Player) {
		if s, ok := p.NetStats(); ok || s.Queued != 0 || s.Addr != nil || s.Transport != nil {
			t.Errorf("NetStats() = %+v, %v; want zero, false", s, ok)
		}
	})
}

// Disconnect tells the client why, then closes its session with the player
// and its connection; the client removes the player from the world.
func TestPlayerDisconnectClosesClient(t *testing.T) {
	w, _ := clientTestWorld(t)
	r := clienttest.New()
	var handle *world.EntityHandle
	withClientPlayer(t, w, r, func(tx *world.Tx, p *Player) {
		handle = p.H()
		r.Reset()
		p.Disconnect("§cbye")
		var lifecycle []clienttest.Call
		for _, c := range r.Calls() {
			switch c.Method {
			case "Disconnect", "Close", "CloseConnection":
				lifecycle = append(lifecycle, c)
			}
		}
		if got, want := methods(lifecycle), []string{"Disconnect", "Close", "CloseConnection"}; !slices.Equal(got, want) {
			t.Fatalf("calls = %v, want %v", got, want)
		}
		if got := argText(t, lifecycle[0], 0); !text.Equal(got, text.ParseLegacy("§cbye")) {
			t.Errorf("Disconnect message = %q, want §cbye", got.Legacy())
		}
		if lifecycle[1].Args[0] != tx || lifecycle[1].Args[1] != client.Controllable(p) {
			t.Errorf("Close args = %v, want the transaction and the player", lifecycle[1].Args)
		}
	})
	if err := handle.Do(func(*world.Tx, world.Entity) {}).Wait(t.Context()); !errors.Is(err, world.ErrEntityClosed) {
		t.Errorf("player handle after Disconnect: err = %v, want %v", err, world.ErrEntityClosed)
	}
}
