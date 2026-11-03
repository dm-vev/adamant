// Command loadtest measures how an Adamant server binary performs under a
// fixed load of Bedrock bot clients, so that servers built from different
// commits can be compared over the network, which stays byte-identical
// between them.
//
// It has three subcommands:
//
//	loadtest pregen -world DIR -kind flat|vanilla [-seed 1] [-radius 24]
//	loadtest run -addr http://127.0.0.1:19700 -bots 10 -pid PID -out run.json [flags]
//	loadtest summarize -results DIR -out summary.json [-base COMMIT]
//
// pregen writes a world folder whose chunks around the spawn are already
// generated, flat or with the vanilla generator and a fixed seed, so that
// world generation does not take part in a run. The spawn, a fixed time of
// day, no weather, no mob spawning and creative mode are stored in its
// level.dat. A run copies the folder and points the server at the copy.
//
// run joins the bots one after another over NetherNet, with offline
// logins, and runs the same workload on each: it waits for the chunks of
// its view radius (answering the sub-chunk requests the game sends), then
// walks a fixed square around the spawn, faces the bot that joined before
// it, chats at a fixed rate and, optionally, places and breaks a block and
// opens its inventory. It measures the join (to the spawn and to the full
// chunk radius), the round trip of chat (from sending a line to receiving
// its broadcast), the packets and bytes each bot receives, and errors, and
// samples the CPU, the resident memory and the threads of the server
// process from /proc/<pid> once a second. The results go to one JSON file
// per run and a short text summary to standard output.
//
// summarize reads the run files of a results directory, of the layout
// scripts/loadtest_compare.sh writes, and the microbenchmark outputs
// beside them, and writes one summary.json with the mean and spread of
// every metric over the repetitions of each commit and scenario.
//
// The text benchmarks of this package (go test -run '^$' -bench . ./cmd/loadtest)
// compare the old text path, net/bedrock/text.Colourf with strings passed
// through, with the structured one, server/text with the renderers of
// protocol/bedrock/text.
//
// scripts/loadtest_compare.sh builds the server of each commit passed and
// runs the scenarios against each, alternating the commits.
package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	var err error
	switch os.Args[1] {
	case "pregen":
		err = pregen(os.Args[2:])
	case "run":
		err = run(os.Args[2:])
	case "summarize":
		err = summarize(os.Args[2:])
	default:
		usage()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "loadtest:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: loadtest pregen|run|summarize [flags]; loadtest <subcommand> -h lists the flags")
	os.Exit(2)
}
