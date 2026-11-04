package remoteadmin

import (
	"log/slog"
	"regexp"
	"sync"

	"github.com/df-mc/dragonfly/server/audit"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/go-gl/mathgl/mgl64"
)

// Line is a line of the output of a command, as the API answers it.
type Line struct {
	// Text is the message or the error, in English, without § codes.
	Text string `json:"text"`
	// Error reports whether the line is an error.
	Error bool `json:"error"`
}

// formatting matches a § code of Minecraft's legacy formatting.
var formatting = regexp.MustCompile("§.")

// source is the source of a remote command: of the level and the name of
// its token. It collects the output of the command, that sent after it
// returned too, until the request is answered; output that comes later is
// logged.
type source struct {
	token   token
	address string
	log     *slog.Logger

	mu       sync.Mutex
	out      []Line
	answered bool
	// pending counts the outputs a command said will follow (cmd.Deferrer).
	pending sync.WaitGroup
}

func (s *source) Name() string               { return "remote:" + s.token.name }
func (s *source) Position() mgl64.Vec3       { return mgl64.Vec3{} }
func (s *source) PermissionLevel() cmd.Level { return s.token.level }

// AuditActor returns the source as the audit log records it.
func (s *source) AuditActor() audit.Actor {
	return audit.Actor{Kind: audit.KindRemote, Name: s.Name(), Token: s.token.name, Level: int(s.token.level), Address: s.address}
}

// SendCommandOutput keeps the output for the answer, or logs it once the
// request was answered.
func (s *source) SendCommandOutput(o *cmd.Output) {
	var lines []Line
	for _, m := range o.Messages() {
		lines = append(lines, Line{Text: formatting.ReplaceAllString(m.String(), "")})
	}
	for _, err := range o.Errors() {
		lines = append(lines, Line{Text: formatting.ReplaceAllString(err.Error(), ""), Error: true})
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.answered {
		s.out = append(s.out, lines...)
		return
	}
	for _, l := range lines {
		s.log.Info("Remote command output after its answer: "+l.Text, "token", s.token.name)
	}
}

// Defer counts an output that follows the command, and returns the
// function that says it was sent.
func (s *source) Defer() func() {
	s.pending.Add(1)
	var once sync.Once
	return func() { once.Do(s.pending.Done) }
}

// answer returns the output so far, and logs what comes after.
func (s *source) answer() []Line {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.answered = true
	if s.out == nil {
		return []Line{}
	}
	return s.out
}

var (
	_ cmd.Deferrer = (*source)(nil)
	_ cmd.Leveled  = (*source)(nil)
)
