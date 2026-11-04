// Package audit keeps the audit log of a server: who ran which admin
// command, changed a list or made a backup, when, where and with what
// result, one JSON object a line.
//
// Writing a record never blocks: a record is handed to the goroutine of
// the Log through a buffered channel, and a record the channel has no room
// for is dropped and counted. The next record written is preceded by a
// record of the drops, {"action":"audit-dropped","count":N}. The owners of
// the worlds record commands as they run them, and must not wait for a
// disk.
package audit

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Kind is the kind of the actor of a record.
type Kind string

const (
	// KindPlayer is a player online.
	KindPlayer Kind = "player"
	// KindConsole is the console of the server.
	KindConsole Kind = "console"
	// KindRemote is a client of the remote admin API.
	KindRemote Kind = "remote"
	// KindCommandBlock is a command block or a command-block minecart.
	KindCommandBlock Kind = "command_block"
	// KindServer is the server itself: a schedule, an expiry.
	KindServer Kind = "server"
	// KindAPI is code calling a method of the server: an embedder, and
	// later a plugin.
	KindAPI Kind = "api"
)

// Actor is who did what a record records.
type Actor struct {
	// Kind is the kind of the actor.
	Kind Kind `json:"kind"`
	// Name is the name to show: the name of a player, of a command block,
	// of a remote token.
	Name string `json:"name,omitempty"`
	// Key is the identity key of a player: "xuid:<XUID>", or its name
	// normalised on a server keyed by name.
	Key string `json:"key,omitempty"`
	// Level is the level of the actor at the time, 0 to 4.
	Level int `json:"level"`
	// Address is the address of a player or a remote client, left out of
	// the file without RecordAddresses.
	Address string `json:"address,omitempty"`
	// Token is the name of the token of a remote client.
	Token string `json:"token,omitempty"`
}

// String returns the actor as the source of a ban shows it: "Console",
// "Steve (xuid:2535412345678901)", "remote:panel", "Server", "API".
func (a Actor) String() string {
	switch a.Kind {
	case KindConsole:
		return "Console"
	case KindServer:
		return "Server"
	case KindAPI:
		if a.Name != "" {
			return a.Name
		}
		return "API"
	case KindRemote:
		return "remote:" + a.Token
	case KindPlayer:
		if strings.HasPrefix(a.Key, "xuid:") {
			return a.Name + " (" + a.Key + ")"
		}
		return a.Name
	}
	if a.Name != "" {
		return a.Name
	}
	return string(a.Kind)
}

// Where is where an actor in a world was.
type Where struct {
	// Dimension is the name of the dimension: "overworld", "nether", "end".
	Dimension string `json:"dimension"`
	// Pos is the position of the actor.
	Pos [3]float64 `json:"pos"`
}

// Result is how what a record records ended.
type Result string

const (
	// ResultOK is a command that ran, a change that was made.
	ResultOK Result = "ok"
	// ResultError is a command that ran with errors, a change that failed.
	ResultError Result = "error"
	// ResultDenied is a command the actor's level is too low for.
	ResultDenied Result = "denied"
	// ResultRefused is a change a rule of the lists refused, as giving a
	// level above one's own.
	ResultRefused Result = "refused"
)

// Record is one line of the audit log.
type Record struct {
	// Time is when it happened, in UTC to the millisecond; the Log sets it
	// if zero.
	Time time.Time `json:"time"`
	// Actor is who did it.
	Actor Actor `json:"actor"`
	// Where is where the actor was, for players and command blocks.
	Where *Where `json:"where,omitempty"`
	// Action is the name of the command, aliases resolved, or of what
	// happened: "op", "ban", "ban-expired", "backup", "auth-failed".
	Action string `json:"action"`
	// Command is the command line as typed.
	Command string `json:"command,omitempty"`
	// Target is the player or the address a list change is of.
	Target string `json:"target,omitempty"`
	// Result is how it ended.
	Result Result `json:"result,omitempty"`
	// Output are the messages and errors of a command, capped at
	// MaxOutput bytes in all.
	Output []string `json:"output,omitempty"`
	// Count is how many records were dropped, for "audit-dropped", or how
	// many failures a summary of failures stands for.
	Count uint64 `json:"count,omitempty"`
}

// MaxOutput is how many bytes of output a record keeps, in all.
const MaxOutput = 4 << 10

// Config is how a Log writes.
type Config struct {
	// File is the path of the audit log.
	File string
	// MaxSize is the size the file is rotated at, in bytes. Zero or less
	// never rotates.
	MaxSize int64
	// Keep is how many rotated files are kept. Zero keeps none.
	Keep int
	// RecordAddresses keeps the addresses of actors in the records.
	RecordAddresses bool
	// Buffer is how many records may wait for the disk before records are
	// dropped. Zero or less is DefaultBuffer.
	Buffer int
	// Log, if not nil, is also written each record at debug level, as the
	// component of the commands logs.
	Log *slog.Logger
	// Now returns the time of a record that sets none. If nil, time.Now.
	Now func() time.Time
}

// DefaultBuffer is the Buffer of a Config that sets none.
const DefaultBuffer = 4096

// Log is an audit log open for writing. A nil *Log records nothing, so
// that a server with the audit log off calls it all the same.
type Log struct {
	conf    Config
	records chan Record
	// dropped counts every record dropped, unreported those not yet
	// written as a record of drops.
	dropped, unreported atomic.Uint64
	closing             sync.Once
	stop                chan struct{}
	done                chan struct{}
	// file is the file written, size its size; both are the writer's.
	file *os.File
	size int64
	// gate, if not nil, is received from before each write, to block the
	// writer in tests.
	gate chan struct{}
	// err is the last error of writing, which Close returns.
	err atomic.Pointer[error]
}

// Open opens the audit log of conf.File, which it appends to, made with
// mode 0600 if missing: the records hold addresses and names of players.
func Open(conf Config) (*Log, error) {
	if conf.Buffer <= 0 {
		conf.Buffer = DefaultBuffer
	}
	if conf.Now == nil {
		conf.Now = time.Now
	}
	l := &Log{conf: conf, records: make(chan Record, conf.Buffer), stop: make(chan struct{}), done: make(chan struct{})}
	if err := l.open(); err != nil {
		return nil, err
	}
	go l.run()
	return l, nil
}

// open opens the file of the log for appending.
func (l *Log) open() error {
	if dir := filepath.Dir(l.conf.File); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("open audit log: %w", err)
		}
	}
	f, err := os.OpenFile(l.conf.File, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open audit log: %w", err)
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return fmt.Errorf("open audit log: %w", err)
	}
	l.file, l.size = f, info.Size()
	return nil
}

// Record hands r to the writer of the log, and returns at once: if the
// writer is behind by Buffer records, r is dropped and counted. A record
// with no time is of now.
func (l *Log) Record(r Record) {
	if l == nil {
		return
	}
	if r.Time.IsZero() {
		r.Time = l.conf.Now()
	}
	if !l.conf.RecordAddresses {
		r.Actor.Address = ""
	}
	r.Output = capOutput(r.Output)
	select {
	case <-l.stop:
		l.drop()
	case l.records <- r:
	default:
		l.drop()
	}
}

// drop counts a record dropped.
func (l *Log) drop() {
	l.dropped.Add(1)
	l.unreported.Add(1)
}

// Dropped returns how many records the log dropped since it opened.
func (l *Log) Dropped() uint64 {
	if l == nil {
		return 0
	}
	return l.dropped.Load()
}

// Close writes the records handed to the log, syncs the file and closes
// it. It returns the last error of writing, if any. Records handed to the
// log after Close are dropped.
func (l *Log) Close() error {
	if l == nil {
		return nil
	}
	l.closing.Do(func() { close(l.stop) })
	<-l.done
	if err := l.err.Load(); err != nil {
		return *err
	}
	return nil
}

// run writes the records handed to the log, syncing the file once a second
// while it wrote, until Close.
func (l *Log) run() {
	defer close(l.done)
	sync := time.NewTicker(time.Second)
	defer sync.Stop()
	dirty := false
	for {
		select {
		case r := <-l.records:
			l.write(r)
			dirty = true
		case <-sync.C:
			if dirty {
				l.fail(l.file.Sync())
				dirty = false
			}
		case <-l.stop:
			for {
				select {
				case r := <-l.records:
					l.write(r)
				default:
					// The drops since the last record written are written
					// too, so that the file counts every record it lacks.
					if n := l.unreported.Swap(0); n > 0 {
						l.writeLine(Record{Time: l.conf.Now(), Actor: Actor{Kind: KindServer}, Action: "audit-dropped", Count: n})
					}
					l.fail(l.file.Sync())
					l.fail(l.file.Close())
					return
				}
			}
		}
	}
}

// write writes r, after a record of the drops not yet reported, rotating
// the file first if it would pass MaxSize.
func (l *Log) write(r Record) {
	if l.gate != nil {
		<-l.gate
	}
	if n := l.unreported.Swap(0); n > 0 {
		l.writeLine(Record{Time: l.conf.Now(), Actor: Actor{Kind: KindServer}, Action: "audit-dropped", Count: n})
	}
	l.writeLine(r)
}

// writeLine writes one record as a line of JSON.
func (l *Log) writeLine(r Record) {
	r.Time = r.Time.UTC().Truncate(time.Millisecond)
	line, err := json.Marshal(r)
	if err != nil {
		l.fail(err)
		return
	}
	line = append(line, '\n')
	if l.conf.MaxSize > 0 && l.size > 0 && l.size+int64(len(line)) > l.conf.MaxSize {
		l.fail(l.rotate(r.Time))
	}
	n, err := l.file.Write(line)
	l.size += int64(n)
	l.fail(err)
	if l.conf.Log != nil {
		l.conf.Log.Debug("audit: " + strings.TrimSuffix(string(line), "\n"))
	}
}

// rotate renames the file to audit-<time>.log beside it, opens a new one,
// and deletes the oldest rotated files beyond Keep.
func (l *Log) rotate(now time.Time) error {
	if err := l.file.Sync(); err != nil {
		return err
	}
	if err := l.file.Close(); err != nil {
		return err
	}
	ext := filepath.Ext(l.conf.File)
	base := strings.TrimSuffix(l.conf.File, ext)
	rotated := base + "-" + now.UTC().Format("2006-01-02T150405.000Z") + ext
	if err := os.Rename(l.conf.File, rotated); err != nil {
		// Writing on to the file is better than losing records.
		if openErr := l.open(); openErr != nil {
			return openErr
		}
		return err
	}
	if err := l.open(); err != nil {
		return err
	}
	return l.prune(base, ext)
}

// prune deletes the oldest rotated files beyond Keep.
func (l *Log) prune(base, ext string) error {
	matches, err := filepath.Glob(base + "-*" + ext)
	if err != nil {
		return err
	}
	slices.Sort(matches)
	for len(matches) > l.conf.Keep {
		if err := os.Remove(matches[0]); err != nil {
			return err
		}
		matches = matches[1:]
	}
	return nil
}

// fail keeps err as the last error of writing, if not nil.
func (l *Log) fail(err error) {
	if err != nil {
		l.err.Store(&err)
	}
}

// capOutput returns the output passed with no more than MaxOutput bytes in
// all, the line that passes it cut and ending in "…".
func capOutput(out []string) []string {
	total := 0
	for i, s := range out {
		if total+len(s) > MaxOutput {
			capped := slices.Clone(out[:i+1])
			capped[i] = strings.ToValidUTF8(s[:MaxOutput-total], "") + "…"
			return capped
		}
		total += len(s)
	}
	return out
}
