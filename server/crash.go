package server

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/version"
	"github.com/df-mc/dragonfly/server/world"
)

// defaultCrashReports is the directory crash reports are written to by
// default: crash-reports in the working directory, beside the config.toml
// the program reads, as Java's server writes them beside its
// server.properties.
const defaultCrashReports = "crash-reports"

// crash handles the failure of a world for the fault f, which nothing could
// be blamed for, as Java's server handles a crash of its tick: it writes a
// crash report, then stops the server in order, which disconnects the
// players, saving them, and closes the worlds, saving them. The failed world
// still runs the transactions this takes. It is the Config.Fatal of the
// worlds of the server, called once per world on its owner, and waits for
// nothing.
func (srv *Server) crash(f world.Fault) {
	path, err := srv.writeCrashReport(f, time.Now())
	if err != nil {
		srv.conf.Log.Error("server crashed, and writing the crash report failed: " + err.Error())
	} else {
		srv.conf.Log.Error("server crashed: see " + path)
	}
	// Close panics on a server not yet started: the stop itself does not
	// need one.
	go srv.once.Do(srv.close)
}

// writeCrashReport writes the crash report of the fault f at now to the
// directory of crash reports and returns its path.
func (srv *Server) writeCrashReport(f world.Fault, now time.Time) (string, error) {
	dir := srv.conf.CrashReports
	if dir == "" {
		dir = defaultCrashReports
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	report := crashReport(f, now)
	name := "crash-" + now.Format("2006-01-02_15.04.05") + "-server"
	for i := 0; ; i++ {
		path := filepath.Join(dir, name+".txt")
		if i > 0 {
			// Two worlds failed within the second.
			path = filepath.Join(dir, fmt.Sprintf("%s-%d.txt", name, i))
		}
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		_, err = file.Write(report)
		if syncErr := file.Sync(); err == nil {
			err = syncErr
		}
		if closeErr := file.Close(); err == nil {
			err = closeErr
		}
		return path, err
	}
}

// crashReport returns the text of the crash report of the fault f at now,
// laid out as Java's: a description, the panic and its stack, the details
// of what failed and of the system, then the stacks of all goroutines.
func crashReport(f world.Fault, now time.Time) []byte {
	var b strings.Builder
	b.WriteString("---- Minecraft Crash Report ----\n")
	b.WriteString("// The world failed and the server stopped in order, saving what it could.\n\n")
	fmt.Fprintf(&b, "Time: %s\n", now.Format("2006-01-02 15:04:05"))
	fmt.Fprintf(&b, "Description: Exception ticking world\n\n")
	fmt.Fprintf(&b, "panic: %v\n\n", f.Value)
	b.Write(f.Stack)
	b.WriteString("\n\nA detailed walkthrough of the error, its code path and all known details is as follows:\n")
	b.WriteString(strings.Repeat("-", 87) + "\n\n")

	b.WriteString("-- Fault --\nDetails:\n")
	fmt.Fprintf(&b, "\tKind: %v\n", f.Kind)
	if f.Type != "" {
		fmt.Fprintf(&b, "\tBlamed on: %s\n", f.Type)
	}
	if w := f.World; w != nil {
		fmt.Fprintf(&b, "\tWorld: %s\n", w.Name())
		fmt.Fprintf(&b, "\tDimension: %s\n", strings.ToLower(fmt.Sprint(w.Dimension())))
	}
	if f.Entity != nil {
		fmt.Fprintf(&b, "\tEntity: %s\n", f.Entity.UUID())
	}
	if f.Pos != (cube.Pos{}) || f.Entity != nil {
		fmt.Fprintf(&b, "\tPosition: %d %d %d\n", f.Pos[0], f.Pos[1], f.Pos[2])
	}
	if f.Count != 0 {
		fmt.Fprintf(&b, "\tCount: %d\n", f.Count)
	}

	b.WriteString("\n-- System Details --\nDetails:\n")
	fmt.Fprintf(&b, "\tMinecraft Version: %s (Bedrock Edition), porting Java Edition %s\n", version.Minecraft, version.Java)
	revision := "unknown"
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, set := range info.Settings {
			if set.Key == "vcs.revision" {
				revision = set.Value
			}
		}
	}
	fmt.Fprintf(&b, "\tCommit: %s\n", revision)
	fmt.Fprintf(&b, "\tGo Version: %s\n", runtime.Version())
	fmt.Fprintf(&b, "\tOperating System: %s (%s)\n", runtime.GOOS, runtime.GOARCH)
	fmt.Fprintf(&b, "\tCPUs: %d\n", runtime.NumCPU())
	fmt.Fprintf(&b, "\tGoroutines: %d\n", runtime.NumGoroutine())

	b.WriteString("\n-- Goroutines --\n")
	b.Write(allStacks())
	b.WriteString("\n")
	return []byte(b.String())
}

// allStacks returns the stacks of all goroutines, at most 64 MiB of them.
func allStacks() []byte {
	buf := make([]byte, 1<<20)
	for {
		n := runtime.Stack(buf, true)
		if n < len(buf) || len(buf) >= 64<<20 {
			return buf[:n]
		}
		buf = make([]byte, 2*len(buf))
	}
}
