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
