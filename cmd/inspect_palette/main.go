package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/sandertv/gophertunnel/minecraft/nbt"
)

type blockState struct {
	Name       string         `nbt:"name"`
	Properties map[string]any `nbt:"states"`
	Version    int32          `nbt:"version"`
}

func main() {
	data, err := os.ReadFile("server/world/block_states.nbt")
	if err != nil {
		panic(err)
	}
	dec := nbt.NewDecoder(bytes.NewBuffer(data))
	for {
		var s blockState
		if err := dec.Decode(&s); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			// Stop gracefully if the file has a trailing decode error.
			break
		}
		if isCandleCakeBlock(s.Name) {
			fmt.Printf("%s => %+v\n", s.Name, s.Properties)
		}
	}
}

func isCandleCakeBlock(name string) bool {
	if name == "minecraft:candle_cake" {
		return true
	}
	// Guard against short names before trimming the minecraft: prefix.
	const prefix = "minecraft:"
	if strings.HasPrefix(name, prefix) {
		name = name[len(prefix):]
	}
	return strings.Contains(name, "candle_cake")
}
