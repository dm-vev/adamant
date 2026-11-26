package server

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/dm-vev/adamant/server/event"
	"github.com/dm-vev/adamant/server/player"
	"github.com/dm-vev/adamant/server/world"
)

func TestServerEventsJoinAndQuit(t *testing.T) {
	jt := newJoinTest(t, player.NopProvider{}, Config{Name: "Events Test"})
	if jt.srv.Events() == nil {
		t.Fatal("Server.Events() returned nil")
	}

	var mu sync.Mutex
	var trace []string
	var worldJoined bool

	subLow := event.On(jt.srv.Events(), event.PriorityLowest, func(e *player.JoinEvent) {
		mu.Lock()
		trace = append(trace, "srv:low:"+e.Player.Name())
		mu.Unlock()
	})
	defer subLow.Unsubscribe()

	subHigh := event.On(jt.srv.Events(), event.PriorityHighest, func(e *player.JoinEvent) {
		mu.Lock()
		trace = append(trace, "srv:high:"+e.Player.Name())
		mu.Unlock()
	})
	defer subHigh.Unsubscribe()

	subWorldJoin := event.On(jt.srv.World().Events(), event.PriorityNormal, func(e *player.JoinEvent) {
		mu.Lock()
		worldJoined = e.Tx != nil && e.Player != nil && e.Player.Name() == "Alice"
		mu.Unlock()
	})
	defer subWorldJoin.Unsubscribe()

	quitCh := make(chan string, 1)
	subQuit := event.On(jt.srv.Events(), event.PriorityNormal, func(e *player.QuitEvent) {
		quitCh <- e.Player.Name()
	})
	defer subQuit.Unsubscribe()

	jt.start()
	alice := joinLogin("Alice", "2535400000000010")
	handle := jt.join(t, alice)

	mu.Lock()
	gotTrace := slices.Clone(trace)
	gotWorldJoined := worldJoined
	mu.Unlock()
	if !slices.Equal(gotTrace, []string{"srv:low:Alice", "srv:high:Alice"}) {
		t.Fatalf("JoinEvent priority trace = %v, want [srv:low:Alice srv:high:Alice]", gotTrace)
	}
	if !gotWorldJoined {
		t.Fatal("expected world bus to receive player.JoinEvent")
	}

	if err := player.Do(handle, func(_ *world.Tx, p *player.Player) {
		p.Disconnect("bye")
	}).Wait(context.Background()); err != nil {
		t.Fatal(err)
	}

	select {
	case name := <-quitCh:
		if name != "Alice" {
			t.Fatalf("QuitEvent player name = %q, want Alice", name)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for server QuitEvent")
	}
}
