package world

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
)

func TestDimensionRegistryConcurrentAccess(t *testing.T) {
	reg := newDimensionRegistry(map[int]Dimension{0: Overworld, 1: Nether, 2: End})
	dimensions := make([]testDimension, 32)
	for i := range dimensions {
		dimensions[i] = testDimension{i: i}
	}

	start := make(chan struct{})
	var wg sync.WaitGroup
	for i, dim := range dimensions {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if err := reg.RegisterDimension(1000+i, fmt.Sprintf("test-%d", i), dim); err != nil {
				t.Error(err)
			}
		}()
	}

	for range dimensions {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for i := range dimensions {
				reg.Lookup(1000 + i)
				reg.LookupID(dimensions[i])
				_ = reg.CustomDimensions()
			}
		}()
	}
	close(start)
	wg.Wait()

	registered := reg.CustomDimensions()
	if len(registered) != len(dimensions) {
		t.Fatalf("unexpected registrations: %#v", registered)
	}
	registered[0].Name = "changed"
	if got := reg.CustomDimensions()[0].Name; got == "changed" {
		t.Fatalf("registry returned an unsafe snapshot: %q", got)
	}
}

func TestDimensionRegistryBuiltInIDs(t *testing.T) {
	reg := newDimensionRegistry(map[int]Dimension{0: Overworld, 1: Nether, 2: End})
	for id, want := range map[int]Dimension{0: Overworld, 1: Nether, 2: End} {
		if got, ok := reg.Lookup(id); !ok || got != want {
			t.Fatalf("Lookup(%d) = %v, %v; want %v, true", id, got, ok, want)
		}
		if got, ok := reg.LookupID(want); !ok || got != id {
			t.Fatalf("LookupID(%v) = %d, %v; want %d, true", want, got, ok, id)
		}
	}
}

type testDimension struct{ i int }

func (testDimension) Range() cube.Range                 { return cube.Range{-64, 319} }
func (testDimension) WaterEvaporates() bool             { return false }
func (testDimension) LavaSpreadDuration() time.Duration { return time.Second }
func (testDimension) WeatherCycle() bool                { return true }
func (testDimension) TimeCycle() bool                   { return true }
