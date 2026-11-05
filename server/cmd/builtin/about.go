package builtin

import (
	"runtime"
	"runtime/debug"
	"strings"
	"time"

	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/world"
)

type aboutCommand struct {
	srv serverAdapter
}

func newAboutCommand(srv serverAdapter) cmd.Command {
	return newCommand("about", nil, aboutCommand{srv: srv})
}

func (a aboutCommand) Run(_ cmd.Source, o *cmd.Output, _ *world.Tx) {
	const coreName = "Adamant (Dragonfly fork)"
	o.Print(coreName)

	info, ok := debug.ReadBuildInfo()
	goVersion := runtime.Version()
	if ok && info != nil && info.GoVersion != "" {
		goVersion = info.GoVersion
	}

	primary, joinable := a.srv.GameVersions()
	o.Printf("Minecraft protocol: %s", primary)
	if len(joinable) > 0 {
		o.Printf("Clients that may join: %s", strings.Join(joinable, ", "))
	}
	o.Printf("Go runtime: %s", goVersion)

	if info != nil {
		revision := ""
		for _, setting := range info.Settings {
			if setting.Key == "vcs.revision" && setting.Value != "" {
				revision = setting.Value
				break
			}
		}
		if revision != "" {
			o.Printf("Commit: %s", revision)
		}
	}

	if started := a.srv.StartTime(); !started.IsZero() {
		o.Printf("Uptime: %s", time.Since(started).Round(time.Second))
	}
}
