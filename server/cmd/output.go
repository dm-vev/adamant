package cmd

import (
	"errors"
	"fmt"
	"sync"

	"github.com/dm-vev/adamant/server/player/chat"
	"github.com/dm-vev/adamant/server/world"
	"golang.org/x/text/language"
)

// Output holds the output of a command execution. It holds success messages
// and error messages, which the source of a command execution gets sent.
type Output struct {
	errors    []error
	messages  []fmt.Stringer
	result    int
	resultSet bool
	inExecute bool
	hold      *outputHold
}

// outputHold coordinates deferred delivery of a held Output across goroutines.
// Keeping it behind a pointer allows Output to be copied by value (e.g.
// WithoutMessages) without copying a sync.Mutex.
type outputHold struct {
	mu        sync.Mutex
	count     int
	aborted   bool
	later     func(tx *world.Tx, o *Output)
	onObserve func(Source)
	lastSrc   Source
}

// acquire increments the hold count of o, calling Later(src) on the first hold.
// When o was created by Command.Execute, the initial acquire takes an extra
// hold for Command.Execute itself so delivery waits until both Execute and
// every async step finish.
func (o *Output) acquire(src Source) {
	if o.hold == nil {
		initial := 1
		if o.inExecute {
			initial = 2
		}
		o.hold = &outputHold{count: initial, later: Later(src)}
		return
	}
	o.hold.mu.Lock()
	o.hold.count++
	o.hold.mu.Unlock()
}

// release decrements the hold count of o and delivers it (and notifies the
// observer) once the last hold is released.
func (o *Output) release(tx *world.Tx, src Source) {
	h := o.hold
	if h == nil {
		return
	}
	h.mu.Lock()
	h.lastSrc = src
	h.count--
	done := h.count == 0
	aborted := h.aborted
	later, obs := h.later, h.onObserve
	h.mu.Unlock()
	if !done {
		return
	}
	if aborted {
		later(nil, nil)
		return
	}
	if obs != nil {
		obs(src)
	}
	later(tx, o)
}

// abort marks o aborted (e.g. context world closed before then) and releases
// one hold. When the last hold releases, Later is called with a nil Output so
// any Deferrer's done callback still runs.
func (o *Output) abort() {
	h := o.hold
	if h == nil {
		return
	}
	h.mu.Lock()
	h.aborted = true
	h.count--
	done := h.count == 0
	later := h.later
	h.mu.Unlock()
	if done {
		later(nil, nil)
	}
}

// finishExecute releases Command.Execute's hold if o was held during Run.
// It returns false if o was never held, letting Command.Execute send and
// observe synchronously without locking.
func (o *Output) finishExecute(tx *world.Tx, onObserve func(Source)) bool {
	h := o.hold
	if h == nil {
		return false
	}
	h.mu.Lock()
	h.onObserve = onObserve
	h.count--
	done := h.count == 0
	aborted := h.aborted
	later, src := h.later, h.lastSrc
	h.mu.Unlock()
	if !done {
		return true
	}
	if aborted {
		later(nil, nil)
		return true
	}
	if onObserve != nil {
		onObserve(src)
	}
	later(tx, o)
	return true
}

// SetResult records the command's integer result, as returned by a Java
// command and stored by `execute store result`.
func (o *Output) SetResult(v int) {
	o.result, o.resultSet = v, true
}

// Result returns the command result. Without an explicit result, a command
// that produced no errors returns 1 and a failed command returns 0.
func (o *Output) Result() int {
	if len(o.errors) != 0 {
		return 0
	}
	if o.resultSet {
		return o.result
	}
	return 1
}

// Success reports whether the command completed without errors.
func (o *Output) Success() bool { return len(o.errors) == 0 }

// WithoutMessages returns a copy of the output keeping only its errors and
// result, as sent to players while send_command_feedback is off.
func (o *Output) WithoutMessages() *Output {
	c := *o
	c.messages = nil
	c.hold = nil
	c.inExecute = false
	return &c
}

// Errorf formats an error message and adds it to the command output.
func (o *Output) Errorf(format string, a ...any) {
	o.errors = append(o.errors, fmt.Errorf(format, a...))
}

// Error formats an error message and adds it to the command output.
func (o *Output) Error(a ...any) {
	if len(a) == 1 {
		if err, ok := a[0].(error); ok {
			if err == nil {
				// Ignore nil errors to avoid panics when rendering output.
				return
			}
			o.errors = append(o.errors, err)
			return
		}
	}
	o.errors = append(o.errors, errors.New(fmt.Sprint(a...)))
}

// Errort adds a translation as an error message and parameterises it using the
// arguments passed. Errort panics if the number of arguments is incorrect.
func (o *Output) Errort(t chat.Translation, a ...any) {
	o.errors = append(o.errors, t.F(a...))
}

// Printf formats a (success) message and adds it to the command output.
func (o *Output) Printf(format string, a ...any) {
	o.messages = append(o.messages, stringer(fmt.Sprintf(format, a...)))
}

// Print formats a (success) message and adds it to the command output.
func (o *Output) Print(a ...any) {
	o.messages = append(o.messages, stringer(fmt.Sprint(a...)))
}

// Printt adds a translation as a (success) message and parameterises it using
// the arguments passed. Printt panics if the number of arguments is incorrect.
func (o *Output) Printt(t chat.Translation, a ...any) {
	o.messages = append(o.messages, t.F(a...))
}

// Errors returns a list of all errors added to the command output. Usually
// only one error message is set: After one error message, execution of a
// command typically terminates.
func (o *Output) Errors() []error {
	return o.errors
}

// ErrorCount returns the count of errors that the command output has.
func (o *Output) ErrorCount() int {
	return len(o.errors)
}

// Messages returns a list of all messages added to the command output. The
// amount of messages present depends on the command called.
func (o *Output) Messages() []fmt.Stringer {
	return o.messages
}

// MessageCount returns the count of (success) messages that the command output
// has.
func (o *Output) MessageCount() int {
	return len(o.messages)
}

type stringer string

func (s stringer) String() string { return string(s) }

var MessageSyntax = chat.Translate(str("%commands.generic.syntax"), 3, `Syntax error: unexpected value: at "%s>>%s<<%s"`).Enc("<red>%v</red>")
var MessageUsage = chat.Translate(str("%commands.generic.usage"), 1, `Usage: %s`).Enc("<red>%v</red>")
var MessageUnknown = chat.Translate(str("%commands.generic.unknown"), 1, `Unknown command: "%s": Please check that the command exists and that you have permission to use it.`).Enc("<red>%v</red>")
var MessageNoTargets = chat.Translate(str("%commands.generic.noTargetMatch"), 0, `No targets matched selector`).Enc("<red>%v</red>")
var MessageNumberInvalid = chat.Translate(str("%commands.generic.num.invalid"), 1, `'%s' is not a valid number`).Enc("<red>> %v</red>")
var MessageBooleanInvalid = chat.Translate(str("%commands.generic.boolean.invalid"), 1, `'%s' is not true or false`).Enc("<red>> %v</red>")
var MessagePlayerNotFound = chat.Translate(str("%commands.generic.player.notFound"), 0, `That player cannot be found`).Enc("<red>> %v</red>")
var MessageParameterInvalid = chat.Translate(str("%commands.generic.parameter.invalid"), 1, `'%s' is not a valid parameter`).Enc("<red>> %v</red>")

type str string

// Resolve returns the translation identifier as a string.
func (s str) Resolve(language.Tag) string { return string(s) }
