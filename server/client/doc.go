// Package client holds the contract between the game and the client of a
// player, whichever protocol it speaks: Client is what the game tells a
// client, Controllable what a client drives its player through. A protocol
// adapter, such as the Bedrock Edition session, implements Client and drives
// a Controllable; the game holds the Client of each player that has one and
// never learns the protocol behind it.
//
// The text the game tells a client is structured text (server/text), which
// the adapter renders as its protocol shows text. Forms and dialogues are the
// exception for now: their types (server/player/form, server/player/dialogue)
// hold legacy strings, § codes with their Bedrock Edition meanings, as items
// and signs do.
//
// The package imports no protocol: neither net/ nor protocol/.
package client
