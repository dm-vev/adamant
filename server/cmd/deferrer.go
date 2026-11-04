package cmd

// Deferrer is implemented by a Source that waits for the output a command
// sends after it returned, as /save-all sends once the world was saved: a
// remote client, which answers its request with the output of the command,
// waits for it before it answers. A command that sends output later calls
// Defer before it returns, and the done function Defer returns once the
// later output was sent. A Source that is not a Deferrer is sent the later
// output as it comes, as the console is.
type Deferrer interface {
	// Defer tells the Source that output will follow the command, and
	// returns the function to call once it was sent.
	Defer() (done func())
}
