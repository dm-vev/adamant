package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/df-mc/go-nethernet/endpoint"
	"github.com/pion/webrtc/v4"
	"github.com/sandertv/gophertunnel/minecraft"
	"github.com/sandertv/gophertunnel/minecraft/auth"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/login"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
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
	dialer := minecraft.Dialer{
		IdentityData:               login.IdentityData{DisplayName: "VanillaProbe"},
		DisconnectOnInvalidPackets: true,
		ErrorLog:                   slog.Default(),
	}
	if *biomesPacket != "" {
		dialer.ClientData.CompatibleWithClientSideChunkGen = true
		dialer.ClientData.MaxViewDistance = 32
	}
	// Reference servers may run in a network namespace with only loopback.
	settings := webrtc.SettingEngine{}
	settings.SetIncludeLoopbackCandidate(true)
	network := minecraft.NetherNet{Signaling: endpoint.NewClient()}
	network.Dialer.API = webrtc.NewAPI(webrtc.WithSettingEngine(settings))
	if *authenticate {
		token, err := auth.RequestLiveTokenContext(ctx, os.Stderr)
		if err != nil {
			return fmt.Errorf("authenticate test account: %w", err)
		}
		dialer.TokenSource = auth.AndroidConfig.TokenSource(ctx, token)
	} else if err := bindOfflineIdentity(&dialer, &network); err != nil {
		return err
	}
	conn, err := dialer.DialContextNetwork(ctx, network, *address)
	if err != nil {
		return fmt.Errorf("NetherNet login: %w", err)
	}
	defer conn.Close()
	if err := conn.DoSpawnContext(ctx); err != nil {
		return fmt.Errorf("spawn: %w", err)
	}
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	var biomePacket *packet.BiomeDefinitionList
	messageSeen := *waitMessage == ""
	for biomePacket == nil || !messageSeen {
		pk, err := conn.ReadPacket()
		if err != nil {
			return fmt.Errorf("receive biome registry and server message %q: %w", *waitMessage, err)
		}
		switch pk := pk.(type) {
		case *packet.BiomeDefinitionList:
			if biomePacket == nil {
				biomePacket = pk
			}
		case *packet.Text:
			if matchesServerMessage(pk, *waitMessage) {
				messageSeen = true
			}
		}
	}
	biomes, err := biomeIdentities(biomePacket)
	if err != nil {
		return err
	}
	data := conn.GameData()
	items := make([]itemIdentity, 0, len(data.Items))
	for _, entry := range data.Items {
		items = append(items, itemIdentity{
			Identifier: entry.Name, RuntimeID: entry.RuntimeID,
			ComponentBased: entry.ComponentBased, Version: entry.Version,
		})
	}
	if *itemsNBT != "" {
		encoded, err := encodeItemsNBT(data.Items)
		if err != nil {
			return fmt.Errorf("encode item registry: %w", err)
		}
		if err := writeNewFileAtomic(*itemsNBT, encoded); err != nil {
			return fmt.Errorf("write item registry: %w", err)
		}
	}
	if *biomesPacket != "" {
		var buffer bytes.Buffer
		biomePacket.Marshal(conn.Proto().NewWriter(&buffer, 0))
		if err := writeNewFileAtomic(*biomesPacket, buffer.Bytes()); err != nil {
			return fmt.Errorf("write biome definitions: %w", err)
		}
	}
	slices.SortFunc(items, func(a, b itemIdentity) int { return strings.Compare(a.Identifier, b.Identifier) })
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(snapshot{
		ProtocolVersion: int(conn.Proto().ID()), LibraryVersion: protocol.CurrentVersion,
		WorldSeed: data.WorldSeed, Dimension: data.Dimension,
		WorldGameMode: data.WorldGameMode, Difficulty: data.Difficulty, Items: items, Biomes: biomes,
	})
}

func matchesServerMessage(text *packet.Text, expected string) bool {
	switch text.TextType {
	case packet.TextTypeObject, packet.TextTypeObjectWhisper, packet.TextTypeObjectAnnouncement:
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
