package ai

import (
	"runtime"
	"sync"
	"sync/atomic"
)

// Scheduler runs submitted jobs on a fixed-size worker pool. Submit never blocks; if the queue is full, the job is
// dropped and Submit returns false.
type Scheduler struct {
	jobs chan func()

	stopOnce sync.Once
	stop     chan struct{}
	wg       sync.WaitGroup
	closed   atomic.Bool
}

// NewScheduler creates a new Scheduler and starts the worker goroutines.
func NewScheduler(workers, queueSize int) *Scheduler {
	if workers <= 0 {
		workers = runtime.NumCPU()
	}
	if workers <= 0 {
		workers = 1
	}
	if queueSize <= 0 {
		queueSize = workers * 256
	}

	s := &Scheduler{
		jobs: make(chan func(), queueSize),
		stop: make(chan struct{}),
	}
	s.wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer s.wg.Done()
			for {
				select {
				case job := <-s.jobs:
					if job != nil {
						job()
					}
				case <-s.stop:
					return
				}
			}
		}()
	}
	return s
}

// Submit attempts to enqueue job for execution. Submit returns false if the scheduler queue is full.
func (s *Scheduler) Submit(job func()) bool {
	if s == nil || job == nil {
		return false
	}
	if s.closed.Load() {
		return false
	}
	select {
	case <-s.stop:
		return false
	case s.jobs <- job:
		return true
	default:
		return false
	}
}

// Close stops the scheduler workers and waits for them to exit. Jobs already queued may be dropped.
func (s *Scheduler) Close() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		// Mark the scheduler closed before stopping workers so Submit can't enqueue new jobs.
		s.closed.Store(true)
		close(s.stop)
	})
	s.wg.Wait()
}

var (
	defaultSchedulerOnce sync.Once
	defaultScheduler     *Scheduler
)

// Default returns the process-wide default Scheduler. It is created lazily.
func Default() *Scheduler {
	defaultSchedulerOnce.Do(func() {
		workers := runtime.NumCPU()
		if workers <= 0 {
			workers = 1
		}
		defaultScheduler = NewScheduler(workers, workers*256)
	})
	return defaultScheduler
}
