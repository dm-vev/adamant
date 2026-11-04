package javaadapter

import (
	"fmt"
	"net"
	"sync"

	"github.com/df-mc/dragonfly/server"
	"github.com/df-mc/dragonfly/server/client"
	"github.com/df-mc/dragonfly/server/config"
	"github.com/df-mc/dragonfly/server/version"
	"github.com/dm-vev/adamant/net/java"
)

// UserConfig is the part of config.toml the adapter owns: the [Java] table.
// It may be serialised and is converted to a Config by UserConfig.Config.
type UserConfig struct {
	// Java holds what clients of Java Edition see of the server, which they
	// cannot join yet: the server shows in their server list as one of
	// Bedrock Edition, with its name and players, and a client that joins
	// anyway is told where to join instead.
	Java struct {
		// Address is the TCP address clients of Java Edition find the server
		// at, such as ":25565". An empty value leaves them out.
		Address string
		// Message is what a client of Java Edition that joins is told. An
		// empty value tells it the server is one of Bedrock Edition.
		Message string
	}
}

// DefaultConfig returns the default part of config.toml of the adapter: no
// address, so that clients of Java Edition are left out.
func DefaultConfig() UserConfig {
	return UserConfig{}
}

// Check adds the problems of the values of uc to p, as package
// server/config describes.
func (uc UserConfig) Check(p *config.Problems) {
	config.Address(p, "Java.Address", "tcp", uc.Java.Address)
}

// Config converts the UserConfig to a Config.
func (uc UserConfig) Config() Config {
	return Config{Address: uc.Java.Address, Message: uc.Java.Message}
}

// Config is the configuration of the listener of the adapter.
type Config struct {
	// Address is the TCP address the listener listens on. An empty Address
	// is not to be listened on: the server leaves the listener out.
	Address string
	// Message is what a client that joins is told. If empty, it is told
	// that the server is one of Bedrock Edition and of which version.
	Message string
}

// defaultMessage is what a client of Java Edition that joins is told.
const defaultMessage = "This server is for Minecraft: Bedrock Edition " + version.Minecraft + ". Join it with Bedrock Edition."

// Listener returns the listener of the adapter: a server.Listener for
// server.Config.Listeners. It shows clients of Java Edition the status of
// the host, as a server of Bedrock Edition, and tells those that join
// c.Message. Its Accept never yields a Join: it blocks until the listener
// is closed.
func (c Config) Listener(conf server.Config, host server.Host) (server.Listener, error) {
	message := c.Message
	if message == "" {
		message = defaultMessage
	}
	jl, err := java.Listen(c.Address, func() java.Status {
		st := host.Status()
		maxPlayers := st.MaxPlayers
		if maxPlayers == 0 {
			maxPlayers = st.PlayerCount + 1
		}
		// A protocol no client speaks lists the server as one of another
		// version, which the name says.
		return java.Status{Name: "Bedrock " + version.Minecraft, Protocol: -1, Description: st.Name, Online: st.PlayerCount, Max: maxPlayers}
	})
	if err != nil {
		return nil, fmt.Errorf("listen for Java Edition: %w", err)
	}
	conf.Log.Info("Clients of Java Edition see the server.", "addr", jl.Addr())
	l := &statusListener{jl: jl, closed: make(chan struct{})}
	go func() {
		for {
			login, err := jl.Accept()
			if err != nil {
				return
			}
			go func() { _ = login.Disconnect(message) }()
		}
	}()
	return l, nil
}

// statusListener is the server.Listener of the adapter: a java.Listener
// whose logins are told to join with Bedrock Edition.
type statusListener struct {
	jl     *java.Listener
	once   sync.Once
	closed chan struct{}
}

// Addr returns the address the listener listens on.
func (l *statusListener) Addr() net.Addr { return l.jl.Addr() }

// Accept blocks until the listener is closed, and then returns
// net.ErrClosed: no client of Java Edition joins yet.
func (l *statusListener) Accept() (client.Join, error) {
	<-l.closed
	return nil, net.ErrClosed
}

// Close closes the listener.
func (l *statusListener) Close() error {
	l.once.Do(func() { close(l.closed) })
	return l.jl.Close()
}
