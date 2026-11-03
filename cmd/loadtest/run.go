package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/url"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// runResult is what one run writes, schema "adamant-loadtest-run/1".
// Durations are in milliseconds, rates per second.
type runResult struct {
	Schema   string            `json:"schema"`
	Labels   map[string]string `json:"labels"`
	Workload workload          `json:"workload"`
	Started  time.Time         `json:"started"`
	// WindowStart is when the last bot had the chunks of its view (or
	// gave up waiting for them), and the window of the rates, the chat
	// samples and the server aggregates lasts Workload.Duration from it.
	WindowStart time.Time `json:"window_start"`
	WindowS     float64   `json:"window_s"`
	// StartupMs is from the start of the server process to its ready line,
	// if both were given.
	StartupMs float64 `json:"startup_ms,omitempty"`

	Join    joinResult    `json:"join"`
	Chat    chatResult    `json:"chat"`
	Traffic trafficResult `json:"traffic"`
	Actions actionResult  `json:"actions"`
	Errors  errorResult   `json:"errors"`
	Server  *serverResult `json:"server,omitempty"`
	Host    hostResult    `json:"host"`
}

type joinResult struct {
	// SpawnMs is from the dial to PlayStatus spawn, ChunksMs from the dial
	// to the last chunk of the view radius (its sub-chunks included).
	SpawnMs  dist `json:"spawn_ms"`
	ChunksMs dist `json:"chunks_ms"`
	Joined   int  `json:"joined"`
	Failed   int  `json:"failed"`
	// ChunkTimeouts counts bots whose chunks did not all arrive within the
	// chunk timeout.
	ChunkTimeouts int `json:"chunk_timeouts"`
}

type chatResult struct {
	// RTTMs is from sending a line to receiving its broadcast, for the
	// lines sent in the window.
	RTTMs    dist `json:"rtt_ms"`
	Sent     int  `json:"sent"`
	Received int  `json:"received"`
	Lost     int  `json:"lost"`
}

type trafficResult struct {
	// Per bot and second, over the window, averaged over the bots that
	// stayed connected.
	PacketsPerS     float64 `json:"packets_per_s_per_bot"`
	PacketBytesPerS float64 `json:"packet_bytes_per_s_per_bot"`
	WireBytesPerS   float64 `json:"wire_bytes_per_s_per_bot"`
	// Totals over the window, over all bots.
	Packets     uint64 `json:"packets"`
	PacketBytes uint64 `json:"packet_bytes"`
	WireBytes   uint64 `json:"wire_bytes"`
	// ByID counts the packets of every ID the bots received over the whole
	// run, the join included, with their bytes, decompressed.
	ByID map[string]idCount `json:"by_id"`
}

type idCount struct {
	Packets uint64 `json:"packets"`
	Bytes   uint64 `json:"bytes"`
}

type actionResult struct {
	Placed          uint64 `json:"placed"`
	Broken          uint64 `json:"broken"`
	InventoryOpened uint64 `json:"inventory_opened"`
	StackFailed     uint64 `json:"item_stack_requests_failed"`
}

type errorResult struct {
	DialErrors   int      `json:"dial_errors"`
	Disconnects  int      `json:"disconnects"`
	DecodeErrors uint64   `json:"decode_errors"`
	Messages     []string `json:"messages,omitempty"`
}

// dist summarises samples.
type dist struct {
	N    int     `json:"n"`
	Mean float64 `json:"mean"`
	Min  float64 `json:"min"`
	P50  float64 `json:"p50"`
	P95  float64 `json:"p95"`
	P99  float64 `json:"p99"`
	Max  float64 `json:"max"`
}

func newDist(v []float64) dist {
	if len(v) == 0 {
		return dist{}
	}
	v = slices.Clone(v)
	sort.Float64s(v)
	var sum float64
	for _, x := range v {
		sum += x
	}
	return dist{N: len(v), Mean: sum / float64(len(v)), Min: v[0], P50: quantile(v, 0.5), P95: quantile(v, 0.95), P99: quantile(v, 0.99), Max: v[len(v)-1]}
}

// quantile returns the q quantile of sorted v, interpolated linearly.
func quantile(v []float64, q float64) float64 {
	if len(v) == 1 {
		return v[0]
	}
	pos := q * float64(len(v)-1)
	i := int(pos)
	if i >= len(v)-1 {
		return v[len(v)-1]
	}
	return v[i] + (pos-float64(i))*(v[i+1]-v[i])
}

func ms(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }

// run runs the bots of a workload against a server and writes the result.
func run(args []string) error {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	var w workload
	fs.StringVar(&w.Address, "addr", "http://127.0.0.1:19700", "signaling address of the server")
	fs.IntVar(&w.Bots, "bots", 10, "number of bots")
	fs.DurationVar(&w.JoinInterval, "join-interval", 250*time.Millisecond, "time between the joins of two bots")
	fs.DurationVar(&w.Duration, "duration", 60*time.Second, "measured window, from when the last bot has its chunks")
	var radius int
	fs.IntVar(&radius, "radius", 8, "chunk radius the bots ask for")
	fs.DurationVar(&w.ChunkTimeout, "chunk-timeout", 30*time.Second, "how long a bot waits for its chunks after the dial")
	fs.DurationVar(&w.ChatInterval, "chat-interval", 5*time.Second, "time between two chat lines of a bot; 0 for none")
	fs.Float64Var(&w.Side, "side", 24, "side in blocks of the square a bot walks")
	fs.Float64Var(&w.Speed, "speed", 0.2, "blocks a bot walks a tick")
	fs.BoolVar(&w.Build, "build", true, "place a block and break it by turns")
	fs.DurationVar(&w.BuildInterval, "build-interval", 10*time.Second, "time between two block edits of a bot")
	fs.BoolVar(&w.Inventory, "inventory", true, "open and close the inventory now and then")
	fs.DurationVar(&w.InventoryInterval, "inventory-interval", 15*time.Second, "time between two inventory openings of a bot")
	pid := fs.Int("pid", 0, "server process to sample from /proc; 0 for none")
	out := fs.String("out", "", "file to write the JSON result to; empty for none")
	wait := fs.Duration("wait", 120*time.Second, "how long to wait for the server to be ready")
	serverLog := fs.String("server-log", "", "log of the server; if set, the run waits for -ready in it")
	ready := fs.String("ready", "Dragonfly server started.", "line of the server log that tells it is ready")
	launched := fs.Int64("launched", 0, "Unix time in nanoseconds the server was started at, to measure its startup")
	labels := fs.String("labels", "", "labels of the run, as k=v,k=v, copied into the result")
	_ = fs.Parse(args)
	w.ChunkRadius = int32(radius)

	res := runResult{Schema: "adamant-loadtest-run/1", Labels: parseLabels(*labels), Workload: w, Started: time.Now()}
	if *serverLog != "" {
		at, err := waitLog(*serverLog, *ready, *wait)
		if err != nil {
			return err
		}
		if *launched != 0 {
			res.StartupMs = ms(at.Sub(time.Unix(0, *launched)))
		}
	}
	if err := waitListening(w.Address, *wait); err != nil {
		return err
	}

	var sampler *procSampler
	if *pid != 0 {
		sampler = newProcSampler(*pid)
		go sampler.run()
	}

	bots := make([]*bot, w.Bots)
	var wg sync.WaitGroup
	ctx := context.Background()
	for i := range bots {
		bots[i] = newBot(i, &w)
	}
	// The bots join one after another, each at its time, and wait for the
	// whole load to be in before the window opens.
	joinStart := time.Now()
	joined := make(chan struct{}, len(bots))
	// The end is known once all bots are in: until then it is zero.
	var stop atomic.Int64
	for i, b := range bots {
		wg.Add(1)
		go func() {
			defer wg.Done()
			time.Sleep(time.Until(joinStart.Add(time.Duration(i) * w.JoinInterval)))
			err := b.join(ctx)
			joined <- struct{}{}
			if err != nil {
				return
			}
			go b.read()
			b.play(&stop)
		}()
	}
	for range bots {
		<-joined
	}
	// The window opens once every bot has the chunks of its view, or gave
	// up waiting for them, so that the join is not part of it.
	for _, b := range bots {
		if b.c == nil {
			continue
		}
		select {
		case <-b.chunksDone:
		case <-time.After(time.Until(b.joined.Add(w.ChunkTimeout))):
		}
	}
	res.WindowStart = time.Now()
	windowEnd := res.WindowStart.Add(w.Duration)
	// A grace of three seconds lets the last chat lines of the window come
	// back.
	stopTime := windowEnd.Add(3 * time.Second)
	stop.Store(stopTime.UnixNano())
	start := make([]counters, len(bots))
	for i, b := range bots {
		start[i] = b.snapshot()
	}
	if sampler != nil {
		sampler.mark(res.WindowStart)
	}
	hostStart := readHost()
	time.Sleep(time.Until(windowEnd))
	hostEnd := readHost()
	end := make([]counters, len(bots))
	for i, b := range bots {
		end[i] = b.snapshot()
	}
	if sampler != nil {
		sampler.mark(windowEnd)
	}
	wg.Wait()
	for _, b := range bots {
		if b.c != nil {
			_ = b.c.close()
		}
	}
	if sampler != nil {
		sampler.stop()
		s := sampler.result()
		res.Server = &s
	}
	var serverSeconds float64
	if res.Server != nil {
		serverSeconds = res.Server.Window.CPUSeconds
	}
	res.Host = newHostResult(hostStart, hostEnd, w.Duration.Seconds(), serverSeconds)
	res.WindowS = w.Duration.Seconds()
	collect(&res, bots, start, end, res.WindowStart, windowEnd, stopTime)

	if *out != "" {
		data, err := json.MarshalIndent(res, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(*out, append(data, '\n'), 0o644); err != nil {
			return err
		}
	}
	printSummary(os.Stdout, &res)
	return nil
}

// collect fills the result in from the bots.
func collect(res *runResult, bots []*bot, start, end []counters, from, to, stop time.Time) {
	var spawn, chunks, rtt []float64
	for i, b := range bots {
		if b.c == nil {
			res.Join.Failed++
			res.Errors.DialErrors++
			res.Errors.Messages = appendMessage(res.Errors.Messages, b.name+": dial: "+b.dialErr)
			continue
		}
		res.Join.Joined++
		spawn = append(spawn, ms(b.spawnAfter))
		if c := b.chunksAfter.Load(); c >= 0 {
			chunks = append(chunks, ms(time.Duration(c)))
		}
		if len(b.missing) > 0 {
			res.Join.ChunkTimeouts++
			c := b.spawnChunk()
			res.Errors.Messages = appendMessage(res.Errors.Messages, fmt.Sprintf("%s: %d chunks missing at the chunk timeout around %v, such as %v; %d chunks and %d sub-chunk answers received by then", b.name, len(b.missing), c, b.missing[:min(len(b.missing), 8)], b.chunksAtTimeout[0], b.chunksAtTimeout[1]))
		}
		// A disconnect is one before the bots stopped: closing them at the
		// end ends their reads too.
		if msg := b.disconnect.Load(); msg != nil && time.Unix(0, b.disconnectAt.Load()).Before(stop) {
			res.Errors.Disconnects++
			res.Errors.Messages = appendMessage(res.Errors.Messages, b.name+": "+*msg)
		}
		res.Errors.DecodeErrors += b.decodeErrors.Load()
		for id := range b.byID {
			if n := b.byID[id].Load(); n > 0 {
				if res.Traffic.ByID == nil {
					res.Traffic.ByID = map[string]idCount{}
				}
				c := res.Traffic.ByID[strconv.Itoa(id)]
				c.Packets += n
				c.Bytes += b.bytesByID[id].Load()
				res.Traffic.ByID[strconv.Itoa(id)] = c
			}
		}
		res.Traffic.Packets += end[i].packets - start[i].packets
		res.Traffic.PacketBytes += end[i].packetBytes - start[i].packetBytes
		res.Traffic.WireBytes += end[i].wireBytes - start[i].wireBytes

		b.chatMu.Lock()
		for _, s := range b.chatRTT {
			if !s.sent.Before(from) && s.sent.Before(to) {
				rtt = append(rtt, ms(s.rtt))
				res.Chat.Received++
			}
		}
		for _, t := range b.sentAt {
			if !t.Before(from) && t.Before(to) {
				res.Chat.Sent++
			}
		}
		b.chatMu.Unlock()
		res.Actions.Placed += b.placed.Load()
		res.Actions.Broken += b.broken.Load()
		res.Actions.InventoryOpened += b.invOpened.Load()
		res.Actions.StackFailed += b.stackFailed.Load()
	}
	res.Chat.Lost = res.Chat.Sent - res.Chat.Received
	res.Join.SpawnMs, res.Join.ChunksMs, res.Chat.RTTMs = newDist(spawn), newDist(chunks), newDist(rtt)
	if n := res.Join.Joined; n > 0 {
		secs := to.Sub(from).Seconds()
		res.Traffic.PacketsPerS = float64(res.Traffic.Packets) / float64(n) / secs
		res.Traffic.PacketBytesPerS = float64(res.Traffic.PacketBytes) / float64(n) / secs
		res.Traffic.WireBytesPerS = float64(res.Traffic.WireBytes) / float64(n) / secs
	}
}

// appendMessage keeps the first 50 error messages.
func appendMessage(m []string, s string) []string {
	if len(m) < 50 {
		m = append(m, s)
	}
	return m
}

// waitListening waits until the server answers TCP connections on the
// port of its signaling.
func waitListening(address string, d time.Duration) error {
	host := address
	if u, err := url.Parse(address); err == nil && u.Host != "" {
		host = u.Host
	}
	deadline := time.Now().Add(d)
	for {
		c, err := net.DialTimeout("tcp", host, time.Second)
		if err == nil {
			_ = c.Close()
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("server at %s not listening after %v: %w", host, d, err)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// waitLog waits until a file holds a line and returns when it saw it.
func waitLog(path, line string, d time.Duration) (time.Time, error) {
	deadline := time.Now().Add(d)
	for {
		data, _ := os.ReadFile(path)
		if strings.Contains(string(data), line) {
			return time.Now(), nil
		}
		if time.Now().After(deadline) {
			return time.Time{}, fmt.Errorf("%q not in %s after %v", line, path, d)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// printSummary writes the main numbers of a run as text.
func printSummary(f *os.File, r *runResult) {
	fmt.Fprintf(f, "bots %d joined %d failed %d chunk-timeouts %d disconnects %d decode-errors %d\n",
		r.Workload.Bots, r.Join.Joined, r.Join.Failed, r.Join.ChunkTimeouts, r.Errors.Disconnects, r.Errors.DecodeErrors)
	if r.StartupMs != 0 {
		fmt.Fprintf(f, "server startup  %7.1f ms\n", r.StartupMs)
	}
	fmt.Fprintf(f, "join to spawn   p50 %7.1f ms  p95 %7.1f ms\n", r.Join.SpawnMs.P50, r.Join.SpawnMs.P95)
	fmt.Fprintf(f, "join to chunks  p50 %7.1f ms  p95 %7.1f ms\n", r.Join.ChunksMs.P50, r.Join.ChunksMs.P95)
	fmt.Fprintf(f, "chat rtt        p50 %7.2f ms  p95 %7.2f ms  p99 %7.2f ms  (%d of %d received)\n",
		r.Chat.RTTMs.P50, r.Chat.RTTMs.P95, r.Chat.RTTMs.P99, r.Chat.Received, r.Chat.Sent)
	fmt.Fprintf(f, "per bot         %8.1f packets/s  %10.0f B/s decompressed  %10.0f B/s on the wire\n",
		r.Traffic.PacketsPerS, r.Traffic.PacketBytesPerS, r.Traffic.WireBytesPerS)
	fmt.Fprintf(f, "actions         placed %d broken %d inventories %d stack-requests-failed %d\n",
		r.Actions.Placed, r.Actions.Broken, r.Actions.InventoryOpened, r.Actions.StackFailed)
	if s := r.Server; s != nil {
		fmt.Fprintf(f, "server window   cpu %6.1f %% (p95 %6.1f)  rss %7.1f MiB (max %7.1f)  threads %5.1f (max %d)  cpu-s %.2f\n",
			s.Window.CPUPercent.Mean, s.Window.CPUPercent.P95, s.Window.RSSMiB.Mean, s.Window.RSSMiB.Max, s.Window.Threads.Mean, int(s.Window.Threads.Max), s.Window.CPUSeconds)
	}
	fmt.Fprintf(f, "host window     busy %.2f of %d cores: bots %.2f, other load %.2f; load1 %.2f -> %.2f\n",
		r.Host.BusyCores, r.Host.CPUs, r.Host.BotCores, r.Host.OtherCores, r.Host.Load1Start, r.Host.Load1End)
	for _, m := range r.Errors.Messages {
		fmt.Fprintln(f, "error:", m)
	}
}

// parseLabels reads labels written as k=v,k=v.
func parseLabels(s string) map[string]string {
	m := map[string]string{}
	for _, kv := range strings.Split(s, ",") {
		if k, v, ok := strings.Cut(kv, "="); ok {
			m[k] = v
		}
	}
	return m
}
