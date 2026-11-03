package clienttest

import (
	"context"
	"errors"
	"slices"
	"sync"

	"github.com/df-mc/dragonfly/server/client"
	"github.com/df-mc/dragonfly/server/text"
)

// Join is a client.Join of a test. Login returns L. Start records the Game
// it is given and returns a Recorder started with it, whose Answers hold
// the profile and address of L, or Err if Err is set. Reject records its
// reason. A Join is safe for concurrent use.
type Join struct {
	L   client.Login
	Err error

	mu       sync.Mutex
	games    []client.Game
	rejected []text.Text
	rec      *Recorder
}

// Compile time check to make sure Join implements client.Join.
var _ client.Join = (*Join)(nil)

// Login returns L.
func (j *Join) Login() client.Login { return j.L }

// Start records g and returns Err if it is set, or the error of ctx if ctx
// is done. Otherwise it returns a new Recorder that keeps g.
func (j *Join) Start(ctx context.Context, g client.Game) (client.Joining, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.games = append(j.games, g)
	if j.Err != nil {
		return nil, j.Err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r := New()
	r.Answers.Profile = j.L.Profile
	r.Answers.Addr = j.L.Addr
	r.game = &g
	j.rec = r
	return r, nil
}

// Reject records reason.
func (j *Join) Reject(reason text.Text) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.rejected = append(j.rejected, reason)
}

// Games returns the Games of the calls to Start, in order.
func (j *Join) Games() []client.Game {
	j.mu.Lock()
	defer j.mu.Unlock()
	return slices.Clone(j.games)
}

// Rejected returns the reasons of the calls to Reject, in order.
func (j *Join) Rejected() []text.Text {
	j.mu.Lock()
	defer j.mu.Unlock()
	return slices.Clone(j.rejected)
}

// Recorder returns the Recorder that Start returned, nil if it returned
// none.
func (j *Join) Recorder() *Recorder {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.rec
}

// ErrListenerClosed is returned by the Accept of a closed Listener.
var ErrListenerClosed = errors.New("clienttest: listener closed")

// Listener is a listener of a test that a server accepts the Joins of: its
// Accept yields the Joins sent with Send, in order, until Close. It has the
// methods of a server.Listener.
type Listener struct {
	joins  chan client.Join
	closed chan struct{}
	once   sync.Once
}

// NewListener returns a Listener that yields no Join until one is sent.
func NewListener() *Listener {
	return &Listener{joins: make(chan client.Join), closed: make(chan struct{})}
}

// Send hands j to the next call to Accept. It blocks until Accept takes j,
// or until the Listener is closed, which drops j.
func (l *Listener) Send(j client.Join) {
	select {
	case l.joins <- j:
	case <-l.closed:
	}
}

// Accept blocks until a Join is sent and returns it. Once the Listener is
// closed, it returns ErrListenerClosed.
func (l *Listener) Accept() (client.Join, error) {
	select {
	case <-l.closed:
		return nil, ErrListenerClosed
	default:
	}
	select {
	case j := <-l.joins:
		return j, nil
	case <-l.closed:
		return nil, ErrListenerClosed
	}
}

// Close closes the Listener. It returns nil, also when called again.
func (l *Listener) Close() error {
	l.once.Do(func() { close(l.closed) })
	return nil
}
