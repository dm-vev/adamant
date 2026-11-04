package cmd

import "sync/atomic"

// Execution is a command a source ran, or tried to run, as the observer
// SetObserver installs sees it once its output was sent.
type Execution struct {
	// Source is the source that ran the command.
	Source Source
	// Command is the command, of the name the source typed or an alias.
	Command Command
	// Args are the arguments after the name of the command.
	Args string
	// Output is the output the source was sent.
	Output *Output
	// Level is the level the command needed: that of the runnable that
	// ran, or for a command that ran none, the least level of the
	// runnables the source may run, or of all if it may run none.
	Level Level
	// Denied reports that the source may run no runnable of the command:
	// its level is too low, and it was told the command does not exist.
	Denied bool
}

var (
	observer atomic.Pointer[func(Execution)]
	feedback atomic.Pointer[func(src Source, name, message string) bool]
)

// SetObserver installs the function the server is told every command
// through, after its output was sent, as the audit log records them. It
// is called on the goroutine that ran the command, a world owner for a
// player, and must not wait for anything.
func SetObserver(f func(Execution)) {
	observer.Store(&f)
}

// observe calls the installed observer with e, if any.
func observe(e Execution) {
	if f := observer.Load(); f != nil && *f != nil {
		(*f)(e)
	}
}

// SetAdminFeedback installs the function that shows the operators online
// a message a command sent its source, as "[name: message]", with the game
// rule log_admin_commands on. It is called on the goroutine that ran the
// command and must not wait for anything. It reports whether it showed the
// message; a source sends it to the operators near it otherwise.
func SetAdminFeedback(f func(src Source, name, message string) bool) {
	feedback.Store(&f)
}

// AdminFeedback shows the operators online a message a command sent src,
// named as name, through the function SetAdminFeedback installed. It
// reports false if none is installed.
func AdminFeedback(src Source, name, message string) bool {
	if f := feedback.Load(); f != nil && *f != nil {
		return (*f)(src, name, message)
	}
	return false
}
