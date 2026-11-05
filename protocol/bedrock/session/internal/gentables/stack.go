package main

import (
	"bytes"
	"fmt"
)

// stackKind is the list of the actions of an item stack request, whose
// table (stack_request_actions.go) is a map of functions and so has no switch
// to write: the command writes the list of the types only, that of the
// actions the protocol the session is compiled against has: the types named
// for one, as its interface is one that any type with an Encode and a Decode
// implements.
var stackKind = kindSpec{
	pkg:      "github.com/dm-vev/adamant/net/bedrock/proto/v2225",
	worldPkg: "github.com/dm-vev/adamant/net/bedrock/proto/v2225",
	iface:    "StackRequestAction",
	pointers: true,
	suffix:   "StackRequestAction",
	testOut:  "stack_action_types_gen_test.go",

	generateTypes: generateStackTypes,
}

// generateStackTypes returns the source of the test file that lists a zero
// value of each action.
func generateStackTypes(names []string) []byte {
	var b bytes.Buffer
	b.WriteString(header)
	b.WriteString(`package session_test

import mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"

// stackActionTypes holds a zero value of every type of the package v2225 that
// is a StackRequestAction, for the tests that compare how the actions of an item
// stack request are handled.
var stackActionTypes = []mc.StackRequestAction{
`)
	for _, n := range names {
		fmt.Fprintf(&b, "\t&mc.%s{},\n", n)
	}
	b.WriteString("}\n")
	return b.Bytes()
}
