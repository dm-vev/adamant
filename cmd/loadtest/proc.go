package main

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// clockTicks is the unit of the CPU times of /proc/<pid>/stat, USER_HZ,
// which is 100 on Linux on every architecture Go supports.
const clockTicks = 100

// procSample is one sample of the server process.
type procSample struct {
	// T is the time since the sampling began, in seconds.
	T float64 `json:"t"`
	// CPUPercent is the CPU time used since the sample before, as a
	// percentage of one core.
	CPUPercent float64 `json:"cpu_percent"`
	RSSMiB     float64 `json:"rss_mib"`
	Threads    int     `json:"threads"`
}

// serverResult is what the sampler gives of a run.
type serverResult struct {
	PID     int          `json:"pid"`
	Samples []procSample `json:"samples"`
	// Window aggregates the samples within the measured window.
	Window procWindow `json:"window"`
	// RSSMiBEnd is the resident memory at the end of the window.
	RSSMiBEnd float64 `json:"rss_mib_end"`
	Err       string  `json:"error,omitempty"`
}

type procWindow struct {
	CPUPercent dist `json:"cpu_percent"`
	RSSMiB     dist `json:"rss_mib"`
	Threads    dist `json:"threads"`
	// CPUSeconds is the CPU time the process used in the window, user and
	// system together.
	CPUSeconds float64 `json:"cpu_seconds"`
}

// procSampler samples a process from /proc once a second.
type procSampler struct {
	pid   int
	begin time.Time
	done  chan struct{}
	wg    sync.WaitGroup

	mu      sync.Mutex
	samples []procSample
	marks   []time.Time
	// cpuAt holds the CPU ticks at each mark.
	cpuAt []uint64
	err   error
}

func newProcSampler(pid int) *procSampler {
	return &procSampler{pid: pid, begin: time.Now(), done: make(chan struct{})}
}

func (p *procSampler) run() {
	p.wg.Add(1)
	defer p.wg.Done()
	ticks, _ := readCPU(p.pid)
	prevT, prev := time.Now(), ticks
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-p.done:
			return
		case now := <-t.C:
			ticks, err := readCPU(p.pid)
			rss, threads, err2 := readStatus(p.pid)
			p.mu.Lock()
			if err != nil || err2 != nil {
				if p.err == nil {
					p.err = fmt.Errorf("sample /proc/%d: %v %v", p.pid, err, err2)
				}
				p.mu.Unlock()
				continue
			}
			cpu := float64(ticks-prev) / clockTicks / now.Sub(prevT).Seconds() * 100
			p.samples = append(p.samples, procSample{T: now.Sub(p.begin).Seconds(), CPUPercent: cpu, RSSMiB: rss, Threads: threads})
			p.mu.Unlock()
			prevT, prev = now, ticks
		}
	}
}

// mark records an edge of the window, with the CPU time at it.
func (p *procSampler) mark(t time.Time) {
	ticks, _ := readCPU(p.pid)
	p.mu.Lock()
	p.marks = append(p.marks, t)
	p.cpuAt = append(p.cpuAt, ticks)
	p.mu.Unlock()
}

func (p *procSampler) stop() {
	close(p.done)
	p.wg.Wait()
}

func (p *procSampler) result() serverResult {
	p.mu.Lock()
	defer p.mu.Unlock()
	r := serverResult{PID: p.pid, Samples: p.samples}
	if p.err != nil {
		r.Err = p.err.Error()
	}
	if len(p.marks) < 2 {
		return r
	}
	from, to := p.marks[0].Sub(p.begin).Seconds(), p.marks[1].Sub(p.begin).Seconds()
	var cpu, rss, threads []float64
	for _, s := range p.samples {
		// A sample covers the second before it: those wholly in the window.
		if s.T-1 >= from-0.05 && s.T <= to+0.05 {
			cpu = append(cpu, s.CPUPercent)
			rss = append(rss, s.RSSMiB)
			threads = append(threads, float64(s.Threads))
			r.RSSMiBEnd = s.RSSMiB
		}
	}
	r.Window = procWindow{CPUPercent: newDist(cpu), RSSMiB: newDist(rss), Threads: newDist(threads), CPUSeconds: float64(p.cpuAt[1]-p.cpuAt[0]) / clockTicks}
	return r
}

// readCPU returns the user and system CPU ticks of a process.
func readCPU(pid int) (uint64, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0, err
	}
	// The name of the command, in parentheses, may hold spaces: the fields
	// are counted from the last parenthesis.
	i := bytes.LastIndexByte(data, ')')
	if i < 0 {
		return 0, fmt.Errorf("malformed /proc/%d/stat", pid)
	}
	fields := strings.Fields(string(data[i+1:]))
	// utime and stime are fields 14 and 15 of the line, 12 and 13 after the
	// name.
	if len(fields) < 13 {
		return 0, fmt.Errorf("malformed /proc/%d/stat", pid)
	}
	utime, err := strconv.ParseUint(fields[11], 10, 64)
	if err != nil {
		return 0, err
	}
	stime, err := strconv.ParseUint(fields[12], 10, 64)
	if err != nil {
		return 0, err
	}
	return utime + stime, nil
}

// readStatus returns the resident memory in MiB and the thread count of a
// process.
func readStatus(pid int) (float64, int, error) {
	f, err := os.Open(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()
	var rss float64
	var threads int
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "VmRSS:"):
			fs := strings.Fields(line)
			if len(fs) >= 2 {
				kb, _ := strconv.ParseFloat(fs[1], 64)
				rss = kb / 1024
			}
		case strings.HasPrefix(line, "Threads:"):
			fs := strings.Fields(line)
			if len(fs) >= 2 {
				threads, _ = strconv.Atoi(fs[1])
			}
		}
	}
	return rss, threads, sc.Err()
}

// hostSample is the CPU time of the whole host and of this process at an
// instant, in clock ticks.
type hostSample struct {
	busy, total, self uint64
	load1             float64
	// cpus counts the CPUs of the host, all of them, not only those this
	// process may run on.
	cpus int
}

// readHost samples the host from /proc/stat and /proc/loadavg.
func readHost() hostSample {
	var h hostSample
	if data, err := os.ReadFile("/proc/stat"); err == nil {
		line, rest, _ := strings.Cut(string(data), "\n")
		for _, l := range strings.Split(rest, "\n") {
			if strings.HasPrefix(l, "cpu") {
				h.cpus++
			}
		}
		fs := strings.Fields(line)
		for i, f := range fs[1:] {
			v, _ := strconv.ParseUint(f, 10, 64)
			// guest and guest_nice are counted in user and nice already.
			if i >= 8 {
				break
			}
			h.total += v
			// idle and iowait are fields 4 and 5.
			if i != 3 && i != 4 {
				h.busy += v
			}
		}
	}
	h.self, _ = readCPU(os.Getpid())
	if data, err := os.ReadFile("/proc/loadavg"); err == nil {
		fs := strings.Fields(string(data))
		if len(fs) > 0 {
			h.load1, _ = strconv.ParseFloat(fs[0], 64)
		}
	}
	return h
}

// hostResult is how busy the host was in the window, in cores: all of
// it, the bots, and what neither the bots nor the server used, which is
// other load on the machine.
type hostResult struct {
	CPUs       int     `json:"cpus"`
	BusyCores  float64 `json:"busy_cores"`
	BotCores   float64 `json:"bot_cores"`
	OtherCores float64 `json:"other_cores"`
	Load1Start float64 `json:"load1_start"`
	Load1End   float64 `json:"load1_end"`
}

// newHostResult compares two host samples taken window seconds apart,
// with the server having used serverSeconds of CPU time in between.
func newHostResult(a, b hostSample, window, serverSeconds float64) hostResult {
	cpus := b.cpus
	if cpus == 0 {
		cpus = runtime.NumCPU()
	}
	r := hostResult{CPUs: cpus, Load1Start: a.load1, Load1End: b.load1}
	if b.total > a.total {
		r.BusyCores = float64(b.busy-a.busy) / float64(b.total-a.total) * float64(cpus)
	}
	r.BotCores = float64(b.self-a.self) / clockTicks / window
	r.OtherCores = max(0, r.BusyCores-r.BotCores-serverSeconds/window)
	return r
}
