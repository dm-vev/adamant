package server_test

import (
	"bufio"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server"
	"github.com/df-mc/dragonfly/server/audit"
	"github.com/df-mc/dragonfly/server/client"
	"github.com/df-mc/dragonfly/server/client/clienttest"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/cmd/builtin"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/text"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
	"golang.org/x/text/language"
)

// auditTest is a server that keeps an audit log, made from the defaults
// of config.toml, with players who join through a test listener.
type auditTest struct {
	srv     *server.Server
	l       *clienttest.Listener
	spawned chan *world.EntityHandle
	file    string
}

// newAuditTest returns a server keyed by name, of no saved data, whose
// lists and audit log are in a directory of the test, with the changes to
// the defaults of config.toml passed.
func newAuditTest(t *testing.T, change ...func(uc *server.UserConfig)) *auditTest {
	t.Helper()
	dir := t.TempDir()
	uc := server.DefaultConfig()
	uc.World.SaveData, uc.Players.SaveData = false, false
	uc.World.DisableNether, uc.World.DisableEnd = true, true
	uc.Whitelist.File = filepath.Join(dir, "white-list.txt")
	for _, f := range change {
		f(&uc)
	}
	conf, err := uc.Config(slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	a := &auditTest{l: clienttest.NewListener(), spawned: make(chan *world.EntityHandle, 4), file: filepath.Join(dir, "audit.log")}
	conf.Log = slog.New(slog.NewTextHandler(io.Discard, nil))
	conf.Generator = func(world.Dimension, int64) world.Generator { return world.NopGenerator{} }
	conf.Listeners = []func(server.Config, server.Host) (server.Listener, error){
		func(server.Config, server.Host) (server.Listener, error) { return a.l, nil },
	}
	a.srv = conf.New()
	builtin.Register(a.srv)
	t.Cleanup(func() {
		cmd.SetPermissions(nil)
		cmd.SetObserver(nil)
		cmd.SetAdminFeedback(nil)
	})
	a.srv.Listen()
	go func() {
		for p := range a.srv.Accept() {
			a.spawned <- p.H()
		}
	}()
	return a
}

// join has a player of the name passed join, and returns its handle and
// the recorder of its client.
func (a *auditTest) join(t *testing.T, name string) (*world.EntityHandle, *clienttest.Join) {
	t.Helper()
	j := &clienttest.Join{L: client.Login{
		Identity: client.Identity{Name: name, UUID: uuid.New()},
		Locale:   language.English,
		Addr:     &net.UDPAddr{IP: net.IPv4(203, 0, 113, 9), Port: 4242},
	}}
	a.l.Send(j)
	select {
	case h := <-a.spawned:
		return h, j
	case <-time.After(10 * time.Second):
		t.Fatalf("%s did not spawn", name)
	}
	return nil, nil
}

// records closes the server and returns the records of its audit log.
func (a *auditTest) records(t *testing.T) []audit.Record {
	t.Helper()
	if err := a.srv.Close(); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(a.file)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out []audit.Record
	s := bufio.NewScanner(f)
	for s.Scan() {
		var r audit.Record
		if err := json.Unmarshal(s.Bytes(), &r); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

// consoleLike is a source that says it is the console, as the console of
// package console does, and keeps its output.
type consoleLike struct{ out *cmd.Output }

func (consoleLike) Position() mgl64.Vec3              { return mgl64.Vec3{} }
func (c consoleLike) SendCommandOutput(o *cmd.Output) { *c.out = *o }
func (consoleLike) AuditActor() audit.Actor {
	return audit.Actor{Kind: audit.KindConsole, Name: "Console", Level: 4}
}

// TestAuditRecords checks what the audit log records: a member's /op,
// denied, with its actor, address and place; nothing of /list, of level 0;
// a console command of level 2 with its output, which an operator online
// is shown as "[Console: ...]"; and a record of code calling Server.Audit.
func TestAuditRecords(t *testing.T) {
	a := newAuditTest(t)
	if _, err := a.srv.Op("Watcher"); err != nil {
		t.Fatal(err)
	}
	alice, _ := a.join(t, "Alice")
	_, watcher := a.join(t, "Watcher")
	run := func(line string) {
		<-player.Do(alice, func(tx *world.Tx, p *player.Player) { p.ExecuteCommand(line) }).Done()
	}
	run("/op Alice")
	run("/list")
	out := &cmd.Output{}
	<-a.srv.World().Exec(func(tx *world.Tx) { cmd.ExecuteLine(consoleLike{out}, "/time set day", tx, nil) })
	a.srv.Audit(audit.Record{Actor: audit.Actor{Kind: audit.KindAPI}, Action: "op", Target: "Bob", Result: audit.ResultOK})

	deadline := time.Now().Add(10 * time.Second)
	for {
		found := false
		for _, c := range watcher.Recorder().CallsTo("SendMessage") {
			if s := c.Args[0].(text.Text).Plain(); strings.HasPrefix(s, "[Console: ") {
				found = true
			}
		}
		if found {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the operator online was not shown the feedback of the console")
		}
		time.Sleep(10 * time.Millisecond)
	}

	records := a.records(t)
	if len(records) != 3 {
		t.Fatalf("records %+v, want 3", records)
	}
	op := records[0]
	if op.Action != "op" || op.Command != "/op Alice" || op.Result != audit.ResultDenied || op.Actor.Kind != audit.KindPlayer ||
		op.Actor.Name != "Alice" || op.Actor.Key != "alice" || op.Actor.Level != 0 || op.Actor.Address != "203.0.113.9" ||
		op.Where == nil || op.Where.Dimension != "overworld" {
		t.Errorf("the denied /op: %+v, where %+v", op, op.Where)
	}
	tm := records[1]
	if tm.Action != "time" || tm.Actor.Kind != audit.KindConsole || tm.Result != audit.ResultOK || len(tm.Output) != 1 || tm.Where != nil {
		t.Errorf("the console's /time: %+v", tm)
	}
	if api := records[2]; api.Actor.Kind != audit.KindAPI || api.Target != "Bob" {
		t.Errorf("the record of the API: %+v", api)
	}
}
