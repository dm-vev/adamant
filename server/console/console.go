package console

import (
	"bufio"
	"context"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/df-mc/dragonfly/server"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Console provides a simple CLI backed command source that reads commands from
// an io.Reader (defaulting to os.Stdin) and executes them on the provided server.
type Console struct {
	srv    *server.Server
	log    *slog.Logger
	reader io.Reader
}

// New returns a Console bound to the provided server. The console reads from
// os.Stdin and writes command output to the supplied logger.
func New(srv *server.Server, log *slog.Logger) *Console {
	if log == nil {
		log = slog.Default()
	}
	return &Console{
		srv:    srv,
		log:    log,
		reader: os.Stdin,
	}
}

// WithReader sets a custom reader for the console input. It enables testing the
// console without relying on os.Stdin.
func (c *Console) WithReader(r io.Reader) *Console {
	if r != nil {
		c.reader = r
	}
	return c
}

// Run starts consuming commands from the console. It blocks until the context
// is cancelled or the underlying reader reaches EOF.
func (c *Console) Run(ctx context.Context) {
	scanner := bufio.NewScanner(c.reader)
	src := &consoleSource{log: c.log}

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				c.log.Error("console input error", "err", err)
			}
			return
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "/") {
			line = "/" + line
		}
		done := c.srv.World().Exec(func(tx *world.Tx) {
			cmd.ExecuteLine(src, line, tx, nil)
		})
		<-done
	}
}

type consoleSource struct {
	log *slog.Logger
}

func (c *consoleSource) Position() mgl64.Vec3 { return mgl64.Vec3{} }

func (c *consoleSource) Name() string { return "Console" }

func (c *consoleSource) SendCommandOutput(o *cmd.Output) {
	for _, msg := range o.Messages() {
		c.log.Info(msg.String())
	}
	for _, err := range o.Errors() {
		c.log.Error(err.Error())
	}
}
