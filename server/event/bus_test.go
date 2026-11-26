package event_test

import (
	"slices"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/dm-vev/adamant/server/event"
)

type sampleEvent struct {
	Value int
}

type cancelEvent struct {
	event.Cancellable
	Log []string
}

type otherEvent struct {
	Flag bool
}

func TestBusPriorityOrderAndTieBreak(t *testing.T) {
	bus := event.NewBus()
	var order []string

	event.On(bus, event.PriorityMonitor, func(*sampleEvent) { order = append(order, "monitor") })
	event.On(bus, event.PriorityNormal, func(*sampleEvent) { order = append(order, "normal-1") })
	event.On(bus, event.PriorityLowest, func(*sampleEvent) { order = append(order, "lowest") })
	event.On(bus, event.PriorityHighest, func(*sampleEvent) { order = append(order, "highest") })
	event.On(bus, event.PriorityNormal, func(*sampleEvent) { order = append(order, "normal-2") })
	event.On(bus, event.PriorityLow, func(*sampleEvent) { order = append(order, "low") })
	event.On(bus, event.PriorityHigh, func(*sampleEvent) { order = append(order, "high") })

	ev := sampleEvent{Value: 7}
	if ok := event.Emit(bus, &ev); !ok {
		t.Fatal("non-cancellable event returned false")
	}
	want := []string{"lowest", "low", "normal-1", "normal-2", "high", "highest", "monitor"}
	if !slices.Equal(order, want) {
		t.Fatalf("listener order = %v, want %v", order, want)
	}
}

func TestBusCancellationAndMonitor(t *testing.T) {
	bus := event.NewBus()

	event.On(bus, event.PriorityLowest, func(e *cancelEvent) {
		e.Log = append(e.Log, "lowest")
	})
	event.On(bus, event.PriorityLow, func(e *cancelEvent) {
		e.Log = append(e.Log, "low-cancel")
		e.Cancel()
	})
	event.On(bus, event.PriorityNormal, func(e *cancelEvent) {
		e.Log = append(e.Log, "normal-should-skip")
	})
	event.On(bus, event.PriorityHighest, func(e *cancelEvent) {
		e.Log = append(e.Log, "highest-should-skip")
	})
	event.On(bus, event.PriorityMonitor, func(e *cancelEvent) {
		if !e.Cancelled() {
			t.Error("expected Cancelled() == true in PriorityMonitor")
		}
		e.Log = append(e.Log, "monitor")
	})

	ev := cancelEvent{}
	if allowed := event.Emit(bus, &ev); allowed {
		t.Fatal("cancelled event returned true")
	}
	want := []string{"lowest", "low-cancel", "monitor"}
	if !slices.Equal(ev.Log, want) {
		t.Fatalf("cancelled dispatch log = %v, want %v", ev.Log, want)
	}
}

func TestBusUnsubscribeAndHasListeners(t *testing.T) {
	var bus event.Bus
	if event.HasListeners[sampleEvent](&bus) {
		t.Fatal("fresh bus should have no listeners")
	}
	if event.HasListeners[sampleEvent](nil) {
		t.Fatal("nil bus should have no listeners")
	}

	var calls int
	sub1 := event.On(&bus, event.PriorityNormal, func(*sampleEvent) { calls++ })
	var sub2 event.Subscription
	sub2 = event.On(&bus, event.PriorityHigh, func(*sampleEvent) {
		calls++
		// Self-unsubscribe during Emit.
		sub2.Unsubscribe()
	})
	if !event.HasListeners[sampleEvent](&bus) {
		t.Fatal("expected HasListeners[sampleEvent] == true")
	}
	if event.HasListeners[otherEvent](&bus) {
		t.Fatal("expected HasListeners[otherEvent] == false")
	}

	// Unsubscribing sub2 during first Emit prevents it on second Emit.
	event.Emit(&bus, &sampleEvent{})
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
	event.Emit(&bus, &sampleEvent{})
	if calls != 3 {
		t.Fatalf("calls = %d, want 3", calls)
	}

	// Idempotent unsubscribe & zero Subscription safety.
	sub1.Unsubscribe()
	sub1.Unsubscribe()
	var zeroSub event.Subscription
	zeroSub.Unsubscribe()
	if event.HasListeners[sampleEvent](&bus) {
		t.Fatal("expected HasListeners[sampleEvent] == false after all unsubscribed")
	}
}

func TestBusUnsubscribeSiblingDuringEmit(t *testing.T) {
	bus := event.NewBus()
	var called []string
	var subHigh event.Subscription

	event.On(bus, event.PriorityLow, func(*sampleEvent) {
		called = append(called, "low")
		subHigh.Unsubscribe()
	})
	subHigh = event.On(bus, event.PriorityHigh, func(*sampleEvent) {
		called = append(called, "high")
	})

	event.Emit(bus, &sampleEvent{})
	if !slices.Equal(called, []string{"low"}) {
		t.Fatalf("called = %v, want [low]", called)
	}
}

func TestBusConcurrentRace(t *testing.T) {
	bus := event.NewBus()
	var wg sync.WaitGroup
	var emits atomic.Int64

	// Register different event types out of order concurrently.
	for i := range 8 {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for range 100 {
				var sub event.Subscription
				if id%2 == 0 {
					sub = event.On(bus, event.PriorityNormal, func(*sampleEvent) {})
				} else {
					sub = event.On(bus, event.PriorityNormal, func(*otherEvent) {})
				}
				_ = event.HasListeners[sampleEvent](bus)
				_ = event.HasListeners[otherEvent](bus)
				if id%2 == 0 {
					ev := sampleEvent{Value: id}
					event.Emit(bus, &ev)
				} else {
					ev := otherEvent{Flag: true}
					event.Emit(bus, &ev)
				}
				emits.Add(1)
				sub.Unsubscribe()
			}
		}(i)
	}
	wg.Wait()
	if emits.Load() != 800 {
		t.Fatalf("emits = %d, want 800", emits.Load())
	}
}

func TestBusZeroAllocHotPaths(t *testing.T) {
	emptyBus := event.NewBus()
	ev := sampleEvent{Value: 1}
	cev := cancelEvent{}

	if n := testing.AllocsPerRun(100, func() {
		if event.HasListeners[sampleEvent](emptyBus) {
			t.Fatal("unexpected listener")
		}
		if !event.Emit(emptyBus, &ev) {
			t.Fatal("expected true")
		}
	}); n != 0 {
		t.Fatalf("empty bus HasListeners/Emit allocs = %v, want 0", n)
	}

	bus := event.NewBus()
	// Register a listener for a different type so bus.listenerCount > 0.
	event.On(bus, event.PriorityNormal, func(*otherEvent) {})

	if n := testing.AllocsPerRun(100, func() {
		if event.HasListeners[sampleEvent](bus) {
			t.Fatal("unexpected listener for sampleEvent")
		}
		if !event.Emit(bus, &ev) {
			t.Fatal("expected true")
		}
	}); n != 0 {
		t.Fatalf("unlistened type HasListeners/Emit allocs = %v, want 0", n)
	}

	var count int
	event.On(bus, event.PriorityNormal, func(e *sampleEvent) {
		count += e.Value
	})
	event.On(bus, event.PriorityNormal, func(e *cancelEvent) {
		if e.Cancelled() {
			t.Fatal("should not be cancelled")
		}
	})

	if n := testing.AllocsPerRun(100, func() {
		if !event.HasListeners[sampleEvent](bus) {
			t.Fatal("expected listener")
		}
		if !event.Emit(bus, &ev) {
			t.Fatal("expected true")
		}
		if !event.Emit(bus, &cev) {
			t.Fatal("expected true")
		}
	}); n != 0 {
		t.Fatalf("steady-state Emit allocs = %v, want 0", n)
	}
}

func BenchmarkBus(b *testing.B) {
	b.Run("HasListenersEmpty", func(b *testing.B) {
		bus := event.NewBus()
		b.ReportAllocs()
		for range b.N {
			_ = event.HasListeners[sampleEvent](bus)
		}
	})
	b.Run("HasListenersUnlistenedType", func(b *testing.B) {
		bus := event.NewBus()
		event.On(bus, event.PriorityNormal, func(*otherEvent) {})
		b.ReportAllocs()
		for range b.N {
			_ = event.HasListeners[sampleEvent](bus)
		}
	})
	b.Run("EmitNoListeners", func(b *testing.B) {
		bus := event.NewBus()
		ev := sampleEvent{Value: 1}
		b.ReportAllocs()
		for range b.N {
			event.Emit(bus, &ev)
		}
	})
	b.Run("EmitOneListener", func(b *testing.B) {
		bus := event.NewBus()
		var sum int
		event.On(bus, event.PriorityNormal, func(e *sampleEvent) { sum += e.Value })
		ev := sampleEvent{Value: 1}
		b.ReportAllocs()
		for range b.N {
			event.Emit(bus, &ev)
		}
		_ = sum
	})
	b.Run("EmitFourListenersCancellable", func(b *testing.B) {
		bus := event.NewBus()
		for range 4 {
			event.On(bus, event.PriorityNormal, func(*cancelEvent) {})
		}
		ev := cancelEvent{}
		b.ReportAllocs()
		for range b.N {
			event.Emit(bus, &ev)
		}
	})
}
