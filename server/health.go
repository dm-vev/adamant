package server

import (
	"fmt"
	"strings"
	"time"
)

// HealthCheck is one check of a probe of the health of the server.
type HealthCheck struct {
	// Name names what is checked, as "world/overworld/ticked" or
	// "listener/0".
	Name string
	// OK is whether it passed.
	OK bool
	// Detail says why it failed, or what passed, briefly.
	Detail string
}

// HealthReport is the result of a probe: whether the server passed it, and
// each check.
type HealthReport struct {
	OK     bool
	Checks []HealthCheck
}

// add adds a check to the report.
func (r *HealthReport) add(name string, ok bool, format string, args ...any) {
	r.Checks = append(r.Checks, HealthCheck{Name: name, OK: ok, Detail: fmt.Sprintf(format, args...)})
	if !ok {
		r.OK = false
	}
}

// String returns "ok" if the report passed and verbose is false, and
// otherwise a line for each check, "[+]name detail" or "[-]name detail",
// with a last line of "ok" or "failed", as the probes of Kubernetes write.
func (r HealthReport) String(verbose bool) string {
	if r.OK && !verbose {
		return "ok\n"
	}
	var b strings.Builder
	for _, c := range r.Checks {
		mark := "[+]"
		if !c.OK {
			mark = "[-]"
		}
		b.WriteString(mark + c.Name)
		if c.Detail != "" {
			b.WriteString(" " + c.Detail)
		}
		b.WriteByte('\n')
	}
	if r.OK {
		b.WriteString("ok\n")
	} else {
		b.WriteString("failed\n")
	}
	return b.String()
}

// HealthOptions are the bounds of the probes of the health of a server.
type HealthOptions struct {
	// LiveTimeout is how long the owner of a world may be busy with one
	// tick, counted from when the tick was asked for, before the server is
	// not live. It should be above the time after which the stacks of a
	// stuck tick are logged, so that they are in the log before the
	// process is killed.
	LiveTimeout time.Duration
	// ReadyTickAge is how long ago the last tick of a world may have ended
	// for the server to be ready.
	ReadyTickAge time.Duration
}

// DefaultHealthOptions are the bounds of the probes if none are set: 2
// minutes and 5 seconds.
var DefaultHealthOptions = HealthOptions{LiveTimeout: 2 * time.Minute, ReadyTickAge: 5 * time.Second}

// checkpointGrace is what a checkpoint may be late beyond two intervals
// before the server is not ready.
const checkpointGrace = time.Minute

// Live reports whether the server is live: whether no world has an owner
// busy with a tick for longer than o.LiveTimeout. A world idle between ticks
// is live, and so is a server that is starting or closing: a server saving
// as it closes must not be killed.
//
// It reads atomics alone: it never runs a transaction or waits for a world
// owner, so it answers most of all when the owners are stuck. It may be
// called at any time from any goroutine, a Server of no worlds included.
func (srv *Server) Live(o HealthOptions) HealthReport {
	o = o.orDefault()
	now := srv.now()
	r := HealthReport{OK: true}
	for _, w := range srv.distinctWorlds() {
		name := "world/" + dimensionLabel(w.Dimension()) + "/ticking"
		ts := w.TickState()
		if !ts.Ticking {
			continue
		}
		if busy := now.Sub(ts.Started); ts.Running && busy > o.LiveTimeout {
			r.add(name, false, "a tick asked for %v ago has not ended", busy.Round(time.Second))
			continue
		}
		r.add(name, true, "")
	}
	return r
}

// Ready reports whether the server is ready for players: it listens and is
// not closing, every listener accepts, no world failed, every world ticked
// within o.ReadyTickAge, and, while saving is on, the last checkpoint
// succeeded within two intervals of saving and a minute. Before the first
// checkpoint, the start of the server counts as one.
//
// As Live, it reads atomics alone and waits for no world owner.
func (srv *Server) Ready(o HealthOptions) HealthReport {
	o = o.orDefault()
	now := srv.now()
	r := HealthReport{OK: true}
	started := srv.started.Load()
	switch {
	case started == nil:
		r.add("server/listening", false, "starting")
	case srv.closing.Load():
		r.add("server/listening", false, "closing")
	default:
		r.add("server/listening", true, "")
	}
	for i := range srv.listening {
		up := srv.listening[i].Load()
		detail := ""
		if !up {
			detail = "not accepting"
		}
		r.add(fmt.Sprintf("listener/%d", i), up, "%s", detail)
	}
	for _, w := range srv.distinctWorlds() {
		// A world that failed may have stopped ticking: it is checked
		// first.
		if w.Failed() {
			r.add("world/"+dimensionLabel(w.Dimension())+"/failed", false, "the world reported a fatal fault")
		}
		ts := w.TickState()
		if !ts.Ticking {
			continue
		}
		last := ts.Ended
		if last.Before(ts.Since) {
			last = ts.Since
		}
		name := "world/" + dimensionLabel(w.Dimension()) + "/ticked"
		if age := now.Sub(last); age > o.ReadyTickAge {
			r.add(name, false, "last tick ended %v ago", age.Round(time.Millisecond))
			continue
		}
		r.add(name, true, "")
	}
	r.checkpoint(srv, now, started)
	return r
}

// checkpoint adds the check of the last checkpoint.
func (r *HealthReport) checkpoint(srv *Server, now time.Time, started *time.Time) {
	interval := srv.conf.SaveInterval
	if interval <= 0 || !srv.SaveEnabledState() || started == nil {
		r.add("checkpoint", true, "saving off")
		return
	}
	last := *started
	if t := srv.checkpoints.lastSuccess.Load(); t != 0 && time.Unix(0, t).After(last) {
		last = time.Unix(0, t)
	}
	if age, limit := now.Sub(last), 2*interval+checkpointGrace; age > limit {
		r.add("checkpoint", false, "last checkpoint saved %v ago, over %v", age.Round(time.Second), limit)
		return
	}
	r.add("checkpoint", true, "")
}

// orDefault returns o with the bounds not set taken from
// DefaultHealthOptions.
func (o HealthOptions) orDefault() HealthOptions {
	if o.LiveTimeout <= 0 {
		o.LiveTimeout = DefaultHealthOptions.LiveTimeout
	}
	if o.ReadyTickAge <= 0 {
		o.ReadyTickAge = DefaultHealthOptions.ReadyTickAge
	}
	return o
}

// now returns the time of the clock of the server.
func (srv *Server) now() time.Time {
	if srv.clock != nil {
		return srv.clock()
	}
	return time.Now()
}
