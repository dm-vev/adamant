package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/dm-vev/adamant/net/bedrock/client"
	"github.com/dm-vev/adamant/net/bedrock/login"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/dm-vev/adamant/net/bedrock/wire"
	"github.com/dm-vev/adamant/net/xbox/auth"
)

type itemIdentity struct {
	Identifier     string `json:"identifier"`
	RuntimeID      int16  `json:"runtime_id"`
	ComponentBased bool   `json:"component_based"`
	Version        int32  `json:"version"`
}

type snapshot struct {
	ProtocolVersion int             `json:"protocol_version"`
	LibraryVersion  string          `json:"library_game_version"`
	WorldSeed       int64           `json:"world_seed"`
	Dimension       int32           `json:"dimension"`
	WorldGameMode   int32           `json:"world_game_mode"`
	Difficulty      int32           `json:"difficulty"`
	Items           []itemIdentity  `json:"items"`
	Biomes          []biomeIdentity `json:"biomes"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "vanilla-probe:", err)
		os.Exit(1)
	}
}

func run() error {
	address := flag.String("address", "", "NetherNet server URL, including scheme and port")
	authenticate := flag.Bool("auth", false, "authenticate a test account using Microsoft device-code login; tokens are not saved")
	timeout := flag.Duration("timeout", 5*time.Minute, "total authentication, login and spawn deadline")
	itemsNBT := flag.String("items-nbt", "", "write the complete item registry as vanilla_items.nbt to PATH (must not already exist)")
	biomesPacket := flag.String("biomes-packet", "", "request client generation data and write the complete biome-definition packet body to PATH (must not already exist)")
	waitMessage := flag.String("wait-message", "", "after spawning, wait for this exact server text before disconnecting")
	flag.Parse()
	if *address == "" || flag.NArg() != 0 || *timeout <= 0 {
		return fmt.Errorf("use -address http://host:port [-auth] [-timeout 5m] [-items-nbt PATH] [-biomes-packet PATH] [-wait-message TEXT]; timeout must be positive")
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	conf := client.Config{Identity: login.Identity{DisplayName: "VanillaProbe"}}
	if *biomesPacket != "" {
		// The server sends the generation data of biomes only to clients
		// that generate chunks themselves.
		data := login.DefaultClientData(clientAddress(*address), "VanillaProbe", v2193.Version)
		data.CompatibleWithClientSideChunkGen = true
		data.MaxViewDistance = 32
		conf.ClientData = &data
	}
	if *authenticate {
		dc, err := auth.Android.StartLogin(ctx)
		if err != nil {
			return fmt.Errorf("authenticate test account: %w", err)
		}
		fmt.Fprintf(os.Stderr, "Sign the test account in at %s with the code %s\n", dc.VerificationURI, dc.UserCode)
		account, err := dc.Wait(ctx)
		if err != nil {
			return fmt.Errorf("authenticate test account: %w", err)
		}
		conf.Token = account.MultiplayerToken
	}
	conn, err := client.Dial(ctx, *address, conf)
	if err != nil {
		return fmt.Errorf("NetherNet login: %w", err)
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()

	var biomePacket *v2193.BiomeDefinitionListPacket
	messageSeen := *waitMessage == ""
	for biomePacket == nil || !messageSeen {
		pk, err := conn.ReadPacket()
		if _, ok := err.(*client.DecodeError); ok {
			continue
		}
		if err != nil {
			return fmt.Errorf("receive biome registry and server message %q: %w", *waitMessage, err)
		}
		switch pk := pk.(type) {
		case *v2193.BiomeDefinitionListPacket:
			if biomePacket == nil {
				biomePacket = pk
			}
		case *v2193.TextPacket:
			if matchesServerMessage(pk, *waitMessage) {
				messageSeen = true
			}
		}
	}
	biomes, err := biomeIdentities(biomePacket)
	if err != nil {
		return err
	}
	registry := conn.Items()
	items := make([]itemIdentity, 0, len(registry))
	for _, entry := range registry {
		items = append(items, itemIdentity{
			Identifier: entry.Name, RuntimeID: entry.RuntimeID,
			ComponentBased: entry.ComponentBased, Version: entry.Version,
		})
	}
	if *itemsNBT != "" {
		encoded, err := encodeItemsNBT(registry)
		if err != nil {
			return fmt.Errorf("encode item registry: %w", err)
		}
		if err := writeNewFileAtomic(*itemsNBT, encoded); err != nil {
			return fmt.Errorf("write item registry: %w", err)
		}
	}
	if *biomesPacket != "" {
		var w wire.Writer
		biomePacket.Encode(&w)
		if err := writeNewFileAtomic(*biomesPacket, w.B); err != nil {
			return fmt.Errorf("write biome definitions: %w", err)
		}
	}
	slices.SortFunc(items, func(a, b itemIdentity) int { return strings.Compare(a.Identifier, b.Identifier) })
	game := conn.StartGame()
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(snapshot{
		ProtocolVersion: v2193.Protocol, LibraryVersion: v2193.Version,
		WorldSeed: game.WorldSeed, Dimension: game.Dimension,
		WorldGameMode: game.WorldGameMode, Difficulty: game.Difficulty, Items: items, Biomes: biomes,
	})
}

// clientAddress returns the host and port the client data names the
// server by, as the client does when no client data is given.
func clientAddress(address string) string {
	if _, rest, ok := strings.Cut(address, "://"); ok {
		host, _, _ := strings.Cut(rest, "/")
		return host
	}
	return address
}

func matchesServerMessage(text *v2193.TextPacket, expected string) bool {
	switch text.TextType {
	case v2193.TextTypeObject, v2193.TextTypeObjectWhisper, v2193.TextTypeObjectAnnouncement:
		var message struct {
			RawText []struct {
				Text string `json:"text"`
			} `json:"rawtext"`
		}
		decoder := json.NewDecoder(strings.NewReader(text.Message))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&message); err != nil {
			return false
		}
		var extra any
		if err := decoder.Decode(&extra); err != io.EOF {
			return false
		}
		var literal strings.Builder
		for _, component := range message.RawText {
			literal.WriteString(component.Text)
		}
		return literal.String() == expected
	default:
		return !text.NeedsTranslation && text.Message == expected
	}
}
