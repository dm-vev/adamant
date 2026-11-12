package world

import (
	"runtime"
	"testing"
)

// TestGeneratorWorkersFollowTheScheduler checks that a world sizes its
// generator workers by the processors the scheduler runs goroutines on, not
// by the CPUs of the host: with GOMAXPROCS below them, workers by the host
// would fill every processor and leave none to the owner of the world.
func TestGeneratorWorkersFollowTheScheduler(t *testing.T) {
	if runtime.NumCPU() < 4 {
		t.Skip("needs a host of four logical CPUs to tell the two counts apart")
	}
	prev := runtime.GOMAXPROCS(2)
	defer runtime.GOMAXPROCS(prev)

	if got := schedulerCPUs(); got != 2 {
		t.Fatalf("schedulerCPUs() = %d with GOMAXPROCS 2, want 2", got)
	}
	w := Config{Synchronous: true}.New()
	defer w.Close()
	if got, want := w.conf.GeneratorWorkers, GeneratorProfileFastCores.workers(2); got != want {
		t.Errorf("%d generator workers on 2 processors, want %d", got, want)
	}

	runtime.GOMAXPROCS(prev)
	if got := schedulerCPUs(); got > runtime.NumCPU() {
		t.Errorf("schedulerCPUs() = %d on a host of %d CPUs", got, runtime.NumCPU())
	}
}

// TestLightFillsLeaveProcessorsToTheOwner checks that the columns read from
// the provider have their light filled on at most half the processors, so
// that a player loading many columns does not leave the owner waiting for
// one.
func TestLightFillsLeaveProcessorsToTheOwner(t *testing.T) {
	w := Config{Provider: NopProvider{}}.New()
	defer w.Close()
	if cpus, fills := schedulerCPUs(), cap(w.lightFills); fills > max(1, cpus/2) {
		t.Errorf("%d columns have their light filled at once on %d processors", fills, cpus)
	}
}

// TestGeneratorWorkersLeaveOneCPU checks the default worker counts: every
// logical CPU but one, and at least two.
func TestGeneratorWorkersLeaveOneCPU(t *testing.T) {
	for cpus, want := range map[int]int{1: 2, 2: 2, 3: 2, 4: 3, 8: 7, 16: 15} {
		if got := GeneratorProfileFastCores.workers(cpus); got != want {
			t.Errorf("fast-cores: %d workers on %d CPUs, want %d", got, cpus, want)
		}
	}
	for cpus, want := range map[int]int{2: 2, 24: 22, 64: 62} {
		if got := GeneratorProfileManyCores.workers(cpus); got != want {
			t.Errorf("many-cores: %d workers on %d CPUs, want %d", got, cpus, want)
		}
	}
}
