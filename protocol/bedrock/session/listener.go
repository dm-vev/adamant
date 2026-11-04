package session

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"sync"

	bedrocktext "github.com/df-mc/dragonfly/protocol/bedrock/text"
	"github.com/df-mc/dragonfly/server/client"
	"github.com/df-mc/dragonfly/server/text"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/google/uuid"
	"golang.org/x/text/language"
)

// ConnListener is a source of Bedrock connections that logged in and were
// admitted, such as a netconn.Listener: the connections of the clients a
// Listener hands the server.
type ConnListener interface {
	// Accept blocks until the next connection logged in, and returns it.
	// An error is returned once the ConnListener was closed.
	Accept() (Conn, error)
	// Disconnect shows conn reason, a string with § codes, and closes it.
	// An empty reason closes it with no screen.
	Disconnect(conn Conn, reason string) error
	io.Closer
}

// ListenConfig configures the sessions of the clients a Listener hands
// the server.
type ListenConfig struct {
	// Log is the logger of the sessions; slog.Default() if nil.
	Log *slog.Logger
	// EmoteChatMuted mutes the chat messages of emotes.
	EmoteChatMuted bool
	// Blocks is the finalized block registry of the worlds of the server.
	// Listen builds its network block hashes, as the server makes its
	// listeners: two states of one hash then fail the start of the server,
	// with a panic, rather than the game of the first client. Nil leaves
	// them to the first session.
	Blocks world.BlockRegistry
}

// Listener is the listener of the Bedrock Edition adapter. It hands the
// server a Join for each connection its ConnListener accepts, and starts the
// game of those the server starts, as BDS does. It implements
// server.Listener.
type Listener struct {
	l    ConnListener
	conf ListenConfig

	// once makes content, the content of the game the clients are defined,
	// as the first client starts its game.
	once    sync.Once
	content *startContent
}

// Listen returns a Listener of the connections l accepts.
func Listen(l ConnListener, conf ListenConfig) *Listener {
	if conf.Log == nil {
		conf.Log = slog.Default()
	}
	if conf.Blocks != nil {
		hashesOf(conf.Blocks)
	}
	return &Listener{l: l, conf: conf}
}

// Accept returns the Join of the next connection that l accepts. A
// connection whose identity holds no valid UUID is closed with no screen,
// and the next one is accepted.
func (l *Listener) Accept() (client.Join, error) {
	for {
		conn, err := l.l.Accept()
		if err != nil {
			return nil, err
		}
		if IdentityOf(conn.IdentityData()).UUID == uuid.Nil {
			l.conf.Log.Debug("login refused: no valid UUID", "raddr", conn.RemoteAddr())
			_ = l.l.Disconnect(conn, "")
			continue
		}
		return &join{l: l, conn: conn}, nil
	}
}

// Close closes the ConnListener of l.
func (l *Listener) Close() error {
	return l.l.Close()
}

// contentOf returns the content of the game the clients of l are
// defined: that of level, the default world of the server, made as the
// first client starts its game.
func (l *Listener) contentOf(level *world.World) *startContent {
	l.once.Do(func() {
		l.content = newStartContent(level.BlockRegistry(), world.CustomItems(), world.CustomDimensions())
	})
	return l.content
}

// join is the client.Join of a connection of a Listener.
type join struct {
	l    *Listener
	conn Conn

	once  sync.Once
	login client.Login
}

// Login returns what the connection told of itself as it logged in. It
// reads the skin on the first call.
func (j *join) Login() client.Login {
	j.once.Do(func() {
		d := j.conn.ClientData()
		locale, _ := language.Parse(strings.Replace(d.LanguageCode, "_", "-", 1))
		j.login = client.Login{
			Identity: IdentityOf(j.conn.IdentityData()),
			Profile:  ProfileOf(d),
			Locale:   locale,
			Skin:     ClientSkin(d),
			Addr:     j.conn.RemoteAddr(),
		}
	})
	return j.login
}

// Start starts the game g for the connection, as BDS does: it sends
// StartGame, then the ItemRegistry of the custom items, and makes the
// session of the player.
func (j *join) Start(ctx context.Context, g client.Game) (client.Joining, error) {
	if err := startGame(ctx, j.conn, g, j.l.contentOf(g.Level), j.l.conf.EmoteChatMuted); err != nil {
		return nil, err
	}
	return Config{
		Log:            j.l.conf.Log,
		MaxChunkRadius: g.MaxChunkRadius,
		EmoteChatMuted: j.l.conf.EmoteChatMuted,
		BlockRegistry:  g.World.BlockRegistry(),
		IsOperator:     g.IsOperator,
		Roster:         g.Roster,
	}.New(j.conn), nil
}

// Reject disconnects the connection with reason, through its
// ConnListener.
func (j *join) Reject(reason text.Text) {
	_ = j.l.l.Disconnect(j.conn, bedrocktext.Inline(reason))
}
