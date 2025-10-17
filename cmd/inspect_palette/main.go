package main

import (
    "bytes"
    "fmt"
    "io"
    "os"

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
            if err == io.EOF || err.Error() == "EOF" {
                break
            }
            // In case of trailing decode error, stop gracefully.
            break
        }
        if s.Name == "minecraft:candle_cake" || (len(s.Name) > 0 && s.Name[len("minecraft:"): ] != "" && (contains(s.Name, "_candle_cake") || contains(s.Name, ":candle_cake"))) {
            fmt.Printf("%s => %+v\n", s.Name, s.Properties)
        }
        if contains(s.Name, "candle_cake") {
            // Also print all candle cake-like just in case.
            fmt.Printf("%s => %+v\n", s.Name, s.Properties)
        }
    }
}

func contains(s, sub string) bool {
    return len(s) >= len(sub) && (indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
    // simple substring search
    for i := 0; i+len(sub) <= len(s); i++ {
        if s[i:i+len(sub)] == sub {
            return i
        }
    }
    return -1
}

