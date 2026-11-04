package session

import (
	"fmt"
	"io"
	"log/slog"
	"reflect"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/protocol/bedrock/netconn"
	"github.com/df-mc/dragonfly/server/client"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/player/skin"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/login"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// listedOf returns the player of a session as the session lists it in
// Bind.
func listedOf(s *Session) client.Listed {
	return client.Listed{Identity: identityOf(s.conn), Handle: s.ent, Client: s}
}

// TestRosterListsAndUnlistsConcurrently lists a session while another
// session listed holds its runtime IDs, and unlists it meanwhile: the
// listing and the unlisting run one after the other, and the session listed
// before is left with neither the runtime ID of the other nor its entry.
//
// The session list this replaces installed the runtime IDs before it
// published a session to ViewEntity. The Roster lists the player first:
// nothing can view the entity of a player before it spawns, and Bind lists
// it before the server adds it to any world.
func TestRosterListsAndUnlistsConcurrently(t *testing.T) {
	r := new(client.Roster)
	existing := testSession("existing")
	joining := testSession("joining")
	r.List(listedOf(existing))
	drainPackets(existing)
	existing.entityRuntimeIDs[joining.ent] = joining.ent.RuntimeID()
	existing.entities[joining.ent.RuntimeID()] = joining.ent

	existing.entityMutex.Lock()
	listDone := make(chan struct{})
	go func() {
		r.List(listedOf(joining))
		close(listDone)
	}()
	waitForListed(t, r, joining)

	unlistStarted := make(chan struct{})
	unlistDone := make(chan struct{})
	go func() {
		close(unlistStarted)
		r.Unlist(joining)
		close(unlistDone)
	}()
	<-unlistStarted
	existing.entityMutex.Unlock()

	waitDone(t, listDone)
	waitDone(t, unlistDone)

	existing.entityMutex.Lock()
	_, mapped := existing.entityRuntimeIDs[joining.ent]
	reverseCount := len(existing.entities)
	existing.entityMutex.Unlock()
	if mapped {
		t.Fatal("joining session runtime ID remains after removal")
	}
	if reverseCount != 1 {
		t.Fatalf("runtime ID reverse map contains %d entries after removal, want 1", reverseCount)
	}
	if remaining := r.Players(); len(remaining) != 1 || remaining[0].Client != existing {
		t.Fatalf("roster contains removed session: %v", remaining)
	}

	first := packetOf(existing.testPacket()).(*mc.PlayerListPacket)
	second := packetOf(existing.testPacket()).(*mc.PlayerListPacket)
	if first.Entries[0].ActionType != mc.PlayerListActionAdd || second.Entries[0].ActionType != mc.PlayerListActionRemove {
		t.Fatalf("player list updates out of order: %v then %v", first.Entries[0].ActionType, second.Entries[0].ActionType)
	}
}

// waitForListed waits until s is listed on r.
func waitForListed(t *testing.T, r *client.Roster, s *Session) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if slices.ContainsFunc(r.Players(), func(p client.Listed) bool { return p.Client == s }) {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("the roster did not list the session")
}

func waitDone(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("concurrent roster operation did not finish")
	}
}

func testSession(name string) *Session {
	return testSessionOf(name, entity.NewText(name, mgl64.Vec3{}), uuid.NewString())
}

// testSessionOf returns a session of a test whose player has the handle
// passed, and whose connection logged in as name with the UUID passed. It
// holds what it is given for the test to take.
func testSessionOf(name string, handle *world.EntityHandle, id string) *Session {
	return &Session{
		conn:             testConn{identity: login.Identity{Identity: id, DisplayName: name}},
		conf:             Config{Log: slog.New(slog.NewTextHandler(io.Discard, nil))},
		holdPackets:      1,
		ent:              handle,
		entityRuntimeIDs: map[*world.EntityHandle]uint64{handle: selfEntityRuntimeID},
		entities:         map[uint64]*world.EntityHandle{selfEntityRuntimeID: handle},
	}
}

// testPacket takes the next packet queued for a session of a test, which
// holds what it is given for the test to take: nil if none was queued.
func (s *Session) testPacket() proto.Packet {
	s.packetsMu.Lock()
	defer s.packetsMu.Unlock()
	for _, queue := range []*[]proto.Packet{&s.heldPackets, &s.pendingPackets} {
		if len(*queue) > 0 {
			pk := (*queue)[0]
			*queue = (*queue)[1:]
			return pk
		}
	}
	return nil
}

// drainPackets takes every packet queued for a session of a test, in
// order.
func drainPackets(s *Session) []proto.Packet {
	var out []proto.Packet
	for pk := s.testPacket(); pk != nil; pk = s.testPacket() {
		out = append(out, pk)
	}
	return out
}

type testConn struct {
	Conn
	identity login.Identity
}

func (c testConn) IdentityData() login.Identity { return c.identity }

// packetOf returns the packet a session was given, unwrapping one shared
// with other sessions.
func packetOf(pk proto.Packet) proto.Packet {
	if s, ok := pk.(*netconn.Shared); ok {
		return s.Packet
	}
	return pk
}

// TestPlayerListEntryIsShared lists a session: every other session is given
// the same PlayerList packet of it, encoded once, and the new one the
// entries of the others.
func TestPlayerListEntryIsShared(t *testing.T) {
	t.Parallel()
	r := new(client.Roster)
	a, b, c := testSession("a"), testSession("b"), testSession("c")
	sk := skin.New(64, 64)
	sk.PlayFabID = "c-skin"
	c.listEntry.Store(c.newListEntry(sk))
	r.List(listedOf(a))
	r.List(listedOf(b))
	drainPackets(a)
	drainPackets(b)
	r.List(listedOf(c))
	pa, pb := a.testPacket(), b.testPacket()
	if pa != pb || pa != proto.Packet(c.listEntry.Load()) {
		t.Fatal("the sessions were not given the same entry of the new session")
	}
	if e := packetOf(pa).(*mc.PlayerListPacket).Entries[0]; e.UUID != c.ent.UUID() || e.Skin.PlayFabID != "c-skin" {
		t.Fatalf("entry %+v", e)
	}
	seen := map[uuid.UUID]bool{}
	for range 3 {
		seen[packetOf(c.testPacket()).(*mc.PlayerListPacket).Entries[0].UUID] = true
	}
	if !seen[a.ent.UUID()] || !seen[b.ent.UUID()] || !seen[c.ent.UUID()] {
		t.Fatalf("the new session listed %v", seen)
	}
}

// TestShowSkinReachesEverySession changes the skin of a player: every
// session is given one PlayerSkin of it, whether it sees the player or
// not, and sessions that join later list the player with the new skin.
func TestShowSkinReachesEverySession(t *testing.T) {
	r := new(client.Roster)
	a, b, c := testSession("a"), testSession("b"), testSession("c")
	for _, s := range []*Session{a, b, c} {
		s.conf.Roster = r
		r.List(listedOf(s))
	}
	for _, s := range []*Session{a, b, c} {
		drainPackets(s)
	}
	sk := skin.New(64, 64)
	sk.PlayFabID = "new-skin"
	a.ShowSkin(sk)
	first := a.testPacket()
	for _, s := range []*Session{b, c} {
		if pk := s.testPacket(); pk != first {
			t.Fatal("the sessions were not given the same PlayerSkin")
		}
	}
	if pk, ok := packetOf(first).(*mc.PlayerSkinPacket); !ok || pk.UUID != a.ent.UUID() || pk.Skin.PlayFabID != "new-skin" {
		t.Fatalf("skin packet %+v", packetOf(first))
	}
	if e := a.playerListEntry().Packet.(*mc.PlayerListPacket).Entries[0]; e.Skin.PlayFabID != "new-skin" {
		t.Fatalf("later sessions would list the player with %q", e.Skin.PlayFabID)
	}
}

// TestRosterMatchesSessionList lists and unlists three sessions on a
// Roster in every order in which each is listed before it is unlisted, and
// the same three on the session list the Roster replaced, copied below as
// the oracle. After each step, each session must have been given the same
// packets and hold the same runtime IDs under both.
func TestRosterMatchesSessionList(t *testing.T) {
	names := []string{"a", "b", "c"}
	handles := make([]*world.EntityHandle, len(names))
	ids := make([]string, len(names))
	for i, name := range names {
		handles[i] = entity.NewText(name, mgl64.Vec3{})
		ids[i] = uuid.NewString()
	}
	// The entry of a player draws the IDs of its skin at random: the two
	// sessions of a player share one.
	entries := make([]*netconn.Shared, len(names))
	for i, name := range names {
		entries[i] = testSessionOf(name, handles[i], ids[i]).newListEntry(skin.Skin{})
	}
	sessionsOf := func() []*Session {
		out := make([]*Session, len(names))
		for i, name := range names {
			out[i] = testSessionOf(name, handles[i], ids[i])
			out[i].listEntry.Store(entries[i])
		}
		// b has abilities, which the others are sent with its entry.
		out[1].abilities.Store(&mc.AbilityData{EntityUniqueID: handles[1].UniqueID(), PlayerPermissions: mc.PermissionLevelOperator})
		return out
	}

	for _, steps := range listOrders(len(names)) {
		r, roster := new(client.Roster), sessionsOf()
		l, oracle := new(oracleSessionList), sessionsOf()
		for n, step := range steps {
			i := step.session
			if step.list {
				r.List(listedOf(roster[i]))
				l.Add(oracle[i])
			} else {
				r.Unlist(roster[i])
				l.Remove(oracle[i])
			}
			for k := range names {
				got, want := packetsOf(drainPackets(roster[k])), packetsOf(drainPackets(oracle[k]))
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("%v, step %d: %s was given %s, want %s", steps, n, names[k], describePackets(got), describePackets(want))
				}
				if !reflect.DeepEqual(roster[k].entityRuntimeIDs, oracle[k].entityRuntimeIDs) || !reflect.DeepEqual(roster[k].entities, oracle[k].entities) {
					t.Fatalf("%v, step %d: %s holds runtime IDs %v, want %v", steps, n, names[k], roster[k].entityRuntimeIDs, oracle[k].entityRuntimeIDs)
				}
			}
		}
	}
}

// listStep lists or unlists a session of TestRosterMatchesSessionList.
type listStep struct {
	session int
	list    bool
}

func (s listStep) String() string {
	if s.list {
		return fmt.Sprintf("+%d", s.session)
	}
	return fmt.Sprintf("-%d", s.session)
}

// listOrders returns every order of listing and unlisting n sessions in
// which each is listed once and then unlisted once.
func listOrders(n int) [][]listStep {
	var out [][]listStep
	var walk func(steps []listStep, listed, unlisted []bool)
	walk = func(steps []listStep, listed, unlisted []bool) {
		if len(steps) == 2*n {
			out = append(out, slices.Clone(steps))
			return
		}
		for i := range n {
			switch {
			case !listed[i]:
				listed[i] = true
				walk(append(steps, listStep{session: i, list: true}), listed, unlisted)
				listed[i] = false
			case !unlisted[i]:
				unlisted[i] = true
				walk(append(steps, listStep{session: i}), listed, unlisted)
				unlisted[i] = false
			}
		}
	}
	walk(nil, make([]bool, n), make([]bool, n))
	return out
}

// packetsOf returns the packets passed, unwrapping those shared.
func packetsOf(pks []proto.Packet) []proto.Packet {
	out := make([]proto.Packet, len(pks))
	for i, pk := range pks {
		out[i] = packetOf(pk)
	}
	return out
}

// oracleSessionList is the session list that the Roster replaced, as it
// was: its Add and Remove, without the view layers.
type oracleSessionList struct {
	mu   sync.Mutex
	opMu sync.Mutex
	s    []*Session
}

func (l *oracleSessionList) Add(s *Session) {
	l.opMu.Lock()
	defer l.opMu.Unlock()

	l.mu.Lock()
	others := slices.Clone(l.s)
	l.mu.Unlock()

	// Install every runtime ID before Lookup can publish the session to ViewEntity.
	for _, other := range others {
		l.runtimeID(s, other)
		l.runtimeID(other, s)
	}
	l.runtimeID(s, s)

	l.mu.Lock()
	l.s = append(l.s, s)
	l.mu.Unlock()

	// Avoid holding the session list lock while sending packets to prevent
	// blocking unrelated session list operations on network backpressure.
	for _, other := range others {
		// Show all sessions to the new session and the new session to all
		// existing sessions.
		l.sendSessionTo(s, other)
		l.sendSessionTo(other, s)
	}
	// Show the new session to itself.
	l.sendSessionTo(s, s)
}

func (l *oracleSessionList) Remove(s *Session) {
	l.opMu.Lock()
	l.mu.Lock()
	others := slices.Clone(l.s)
	l.s = deleteVal(l.s, s)
	l.mu.Unlock()

	// Unsend outside the lock to avoid blocking list operations on I/O.
	for _, other := range others {
		l.unsendSessionFrom(s, other)
	}
	l.opMu.Unlock()
}

func (l *oracleSessionList) sendSessionTo(s, to *Session) {
	to.writePacket(s.playerListEntry())
	if data := s.abilities.Load(); data != nil && s != to {
		to.writePacket(&mc.UpdateAbilitiesPacket{AbilityData: *data})
	}
}

func (*oracleSessionList) runtimeID(s, to *Session) uint64 {
	to.entityMutex.Lock()
	defer to.entityMutex.Unlock()

	if runtimeID, ok := to.entityRuntimeIDs[s.ent]; ok {
		to.entities[runtimeID] = s.ent
		return runtimeID
	}
	runtimeID := to.runtimeIDOf(s.ent)
	to.entityRuntimeIDs[s.ent] = runtimeID
	to.entities[runtimeID] = s.ent
	return runtimeID
}

func (l *oracleSessionList) unsendSessionFrom(s, from *Session) {
	from.entityMutex.Lock()
	if id, ok := from.entityRuntimeIDs[s.ent]; ok {
		// Only remove the runtime ID when the mapping exists to avoid deleting unrelated entities.
		delete(from.entities, id)
		delete(from.entityRuntimeIDs, s.ent)
	}
	from.entityMutex.Unlock()

	from.writePacket(&mc.PlayerListPacket{
		Entries: []mc.PlayerListEntry{{
			ActionType: mc.PlayerListActionRemove,
			UUID:       s.ent.UUID(),
		}},
	})
}

// describePackets describes packets for a test failure.
func describePackets(pks []proto.Packet) string {
	var b []byte
	for _, pk := range pks {
		b = fmt.Appendf(b, "%T%+v ", pk, pk)
	}
	return string(b)
}

// deleteVal deletes the first occurrence of a value in a slice of the type E
// and returns a new slice without the value. E must be comparable.
func deleteVal[E comparable](s []E, v E) []E {
	for i, vs := range s {
		if (any)(v) == (any)(vs) {
			return slices.Delete(s, i, i+1)
		}
	}
	return s
}
