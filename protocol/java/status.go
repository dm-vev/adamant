package javaadapter

import (
	"fmt"
	"net"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/df-mc/dragonfly/server"
	"github.com/df-mc/dragonfly/server/client"
	"github.com/df-mc/dragonfly/server/config"
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

// maxVersionName bounds the version name of the status in bytes. Java
// clients limit the whole status to 32767 characters and show the name in a
// line of the server list; the name of an odd server is cut to leave the
// message of the day its room.
const maxVersionName = 256

// bedrockVersions returns the versions of Bedrock Edition that may join a
// server whose GameVersions are primary and joinable, as the server lists
// them with the 1. of the release, such as "1.26.50–1.26.52, 1.26.60–1.26.61".
// A server that lists no joinable version names its primary.
func bedrockVersions(primary string, joinable []string) string {
	if len(joinable) == 0 {
		return primary
	}
	ranges := make([]string, len(joinable))
	for i, j := range joinable {
		ends := strings.Split(j, "–")
		for k, e := range ends {
			if strings.Count(e, ".") == 1 {
				ends[k] = "1." + e
			}
		}
		ranges[i] = strings.Join(ends, "–")
	}
	return strings.Join(ranges, ", ")
}

// versionName returns the name of the version the status shows: "Bedrock "
// and the versions that may join, cut to maxVersionName.
func versionName(primary string, joinable []string) string {
	name := "Bedrock " + bedrockVersions(primary, joinable)
	if len(name) <= maxVersionName {
		return name
	}
	name = name[:maxVersionName-len("…")]
	for !utf8.ValidString(name) {
		name = name[:len(name)-1]
	}
	return name + "…"
}

// defaultMessage returns what a client of Java Edition that joins is told when the
// Config sets none.
func defaultMessage(primary string, joinable []string) string {
	return "This server is for Minecraft: Bedrock Edition " + bedrockVersions(primary, joinable) + ". Join it with Bedrock Edition."
}

// Listener returns the listener of the adapter: a server.Listener for
// server.Config.Listeners. It shows clients of Java Edition the status of
// the host, as a server of Bedrock Edition, and tells those that join
// c.Message. Its Accept never yields a Join: it blocks until the listener
// is closed.
func (c Config) Listener(conf server.Config, host server.Host) (server.Listener, error) {
	jl, err := java.Listen(c.Address, func() java.Status {
		st := host.Status()
		maxPlayers := st.MaxPlayers
		if maxPlayers == 0 {
			maxPlayers = st.PlayerCount + 1
		}
		// A protocol no client speaks lists the server as one of another
		// version, which the name says.
		primary, joinable := host.GameVersions()
		return java.Status{Name: versionName(primary, joinable), Protocol: -1, Description: st.Name, Online: st.PlayerCount, Max: maxPlayers}
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
			message := c.Message
			if message == "" {
				message = defaultMessage(host.GameVersions())
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
