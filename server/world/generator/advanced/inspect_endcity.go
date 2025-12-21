//go:build tools
// +build tools

package main

import (
  "bytes"
  "compress/gzip"
  "fmt"
  "io"
  "os"

  "github.com/df-mc/dragonfly/server/world/generator/advanced/internal/mc112"
)

func main() {
  if len(os.Args) < 2 {
    fmt.Println("usage: inspect_endcity <file>")
    return
  }
  b, err := os.ReadFile(os.Args[1])
  if err != nil {
    panic(err)
  }
  gr, err := gzip.NewReader(bytes.NewReader(b))
  if err != nil {
    panic(err)
  }
  defer gr.Close()
  raw, err := io.ReadAll(gr)
  if err != nil {
    panic(err)
  }
  root, err := mc112.DecodeNBT(raw)
  if err != nil {
    panic(err)
  }

  blocks, ok := root["blocks"].([]map[string]any)
  if !ok {
    fmt.Printf("blocks type %T\n", root["blocks"])
    return
  }
  for i, entry := range blocks {
    nbt, ok := entry["nbt"].(map[string]any)
    if !ok || nbt == nil {
      continue
    }
    fmt.Printf("block[%d] pos=%v nbt=%v\n", i, entry["pos"], nbt)
  }
}
