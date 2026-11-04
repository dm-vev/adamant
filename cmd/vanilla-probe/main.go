// Command vanilla-probe joins a Bedrock server, as a client of a protocol
// it is told, and writes what the server defines to its clients: the item
// registry, the block definitions, the biomes, and for checks the entity
// identifiers, the creative inventory and the recipes. It is how the
// vanilla data of each protocol is taken from BDS of that protocol
// (scripts/bedrock-data.sh).
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/dm-vev/adamant/net/bedrock/client"
	"github.com/dm-vev/adamant/net/bedrock/login"
	"github.com/dm-vev/adamant/net/bedrock/proto/codec"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
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
	if err := run(context.Background(), os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "vanilla-probe:", err)
		os.Exit(1)
	}
}

// options are the flags of the probe.
type options struct {
	address      string
	authenticate bool
	timeout      time.Duration
	protocol     *codec.Codec
	itemsNBT     string
	blocksNBT    string
	biomesPacket string
	entities     string
	creative     string
	crafting     string
	waitMessage  string
}

// parseOptions parses the flags of the probe.
func parseOptions(args []string) (options, error) {
	var o options
	fs := flag.NewFlagSet("vanilla-probe", flag.ContinueOnError)
	fs.StringVar(&o.address, "address", "", "NetherNet server URL, including scheme and port")
	fs.BoolVar(&o.authenticate, "auth", false, "authenticate a test account using Microsoft device-code login; tokens are not saved")
	fs.DurationVar(&o.timeout, "timeout", 5*time.Minute, "total authentication, login and spawn deadline")
	number := fs.Int("protocol", int(codec.V2193.Number), "the protocol to join as: 2193 (1.26.50-1.26.52) or 2225 (1.26.60-1.26.61)")
	fs.StringVar(&o.itemsNBT, "items-nbt", "", "write the complete item registry as items.nbt to PATH (must not already exist)")
	fs.StringVar(&o.blocksNBT, "blocks-nbt", "", "write the block definitions of StartGame as blocks.nbt to PATH (must not already exist)")
	fs.StringVar(&o.biomesPacket, "biomes-packet", "", "request client generation data and write the complete biome-definition packet body to PATH (must not already exist)")
	fs.StringVar(&o.entities, "entities", "", "write the entity identifiers the server lists as JSON to PATH (must not already exist)")
	fs.StringVar(&o.creative, "creative", "", "write the creative inventory, items by name, as JSON to PATH (must not already exist)")
	fs.StringVar(&o.crafting, "crafting", "", "write the recipes, items by name, as JSON to PATH (must not already exist)")
	fs.StringVar(&o.waitMessage, "wait-message", "", "after spawning, wait for this exact server text before disconnecting")
	if err := fs.Parse(args); err != nil {
		return o, err
	}
	if o.address == "" || fs.NArg() != 0 || o.timeout <= 0 {
		return o, errors.New("use -address http://host:port [-protocol N] [-auth] [-timeout 5m] [-items-nbt PATH] [-blocks-nbt PATH] " +
			"[-biomes-packet PATH] [-entities PATH] [-creative PATH] [-crafting PATH] [-wait-message TEXT]; timeout must be positive")
	}
	var ok bool
	if o.protocol, ok = codec.ByNumber(int32(*number)); !ok {
		return o, fmt.Errorf("protocol %d: want 2193 or 2225", *number)
	}
	return o, nil
}

func run(ctx context.Context, args []string, stdout io.Writer) error {
	o, err := parseOptions(args)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, o.timeout)
	defer cancel()
	conn, err := dial(ctx, o)
	if err != nil {
		return err
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	got, err := receive(conn, o)
	if err != nil {
		return err
	}
	if err := write(conn, o, got); err != nil {
		return err
	}
	return report(conn, o, got, stdout)
}

// dial joins the server as the options say.
func dial(ctx context.Context, o options) (*client.Conn, error) {
	conf := client.Config{Identity: login.Identity{DisplayName: "VanillaProbe"}, Protocol: o.protocol}
	if o.biomesPacket != "" {
		// The server sends the generation data of biomes only to clients
		// that generate chunks themselves.
		data := login.DefaultClientData(clientAddress(o.address), "VanillaProbe", o.protocol.Version)
		data.CompatibleWithClientSideChunkGen = true
		data.MaxViewDistance = 32
		conf.ClientData = &data
	}
	if o.authenticate {
		dc, err := auth.Android.StartLogin(ctx)
		if err != nil {
			return nil, fmt.Errorf("authenticate test account: %w", err)
		}
		fmt.Fprintf(os.Stderr, "Sign the test account in at %s with the code %s\n", dc.VerificationURI, dc.UserCode)
		account, err := dc.Wait(ctx)
		if err != nil {
			return nil, fmt.Errorf("authenticate test account: %w", err)
		}
		conf.Token = account.MultiplayerToken
	}
	conn, err := client.Dial(ctx, o.address, conf)
	if err != nil {
		return nil, fmt.Errorf("NetherNet login: %w", err)
	}
	return conn, nil
}

// received is what the probe takes of what the server sends after the
// spawn.
type received struct {
	biomes   *mc.BiomeDefinitionListPacket
	entities *mc.AvailableActorIdentifiersPacket
	creative *mc.CreativeContentPacket
	crafting *mc.CraftingDataPacket
}

// done reports whether the probe has every packet the options ask for.
func (r *received) done(o options) bool {
	return r.biomes != nil && (o.entities == "" || r.entities != nil) &&
		(o.creative == "" || r.creative != nil && len(r.creative.Items) > 0) && (o.crafting == "" || r.crafting != nil)
}

// receive reads what the server sends until it has every packet the
// options ask for, and the message they wait for.
func receive(conn *client.Conn, o options) (*received, error) {
	got := &received{}
	messageSeen := o.waitMessage == ""
	for !got.done(o) || !messageSeen {
		pk, err := conn.ReadPacket()
		if _, ok := err.(*client.DecodeError); ok {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("receive biome registry and server message %q: %w", o.waitMessage, err)
		}
		switch pk := pk.(type) {
		case *mc.BiomeDefinitionListPacket:
			if got.biomes == nil {
				got.biomes = pk
			}
		case *mc.AvailableActorIdentifiersPacket:
			got.entities = pk
		case *mc.CreativeContentPacket:
			// A server may send an empty inventory before its own.
			if got.creative == nil || len(pk.Items) > 0 {
				got.creative = pk
			}
		case *mc.CraftingDataPacket:
			got.crafting = pk
		case *mc.TextPacket:
			if matchesServerMessage(pk, o.waitMessage) {
				messageSeen = true
			}
		}
	}
	return got, nil
}

// write writes the files the options name.
func write(conn *client.Conn, o options, got *received) error {
	names := itemNames(conn.Items())
	files := []struct {
		path, what string
		encode     func() ([]byte, error)
	}{
		{o.itemsNBT, "item registry", func() ([]byte, error) { return encodeItemsNBT(conn.Items()) }},
		{o.blocksNBT, "block definitions", func() ([]byte, error) { return encodeBlocksNBT(conn.StartGame().Blocks) }},
		{o.biomesPacket, "biome definitions", func() ([]byte, error) {
			var w wire.Writer
			got.biomes.Encode(&w)
			return w.B, nil
		}},
		{o.entities, "entity identifiers", func() ([]byte, error) {
			ids, err := entityDump(got.entities)
			if err != nil {
				return nil, err
			}
			return jsonFile(ids)
		}},
		{o.creative, "creative inventory", func() ([]byte, error) { return jsonFile(creativeDump(got.creative, names)) }},
		{o.crafting, "recipes", func() ([]byte, error) { return jsonFile(craftingDump(got.crafting, names)) }},
	}
	for _, f := range files {
		if f.path == "" {
			continue
		}
		encoded, err := f.encode()
		if err != nil {
			return fmt.Errorf("encode %s: %w", f.what, err)
		}
		if err := writeNewFileAtomic(f.path, encoded); err != nil {
			return fmt.Errorf("write %s: %w", f.what, err)
		}
	}
	return nil
}

// jsonFile returns a value as the JSON of a file of a dump.
func jsonFile(v any) ([]byte, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	return append(b, '\n'), err
}

// report writes the snapshot of the server to stdout.
func report(conn *client.Conn, o options, got *received, stdout io.Writer) error {
	biomes, err := biomeIdentities(got.biomes)
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
	slices.SortFunc(items, func(a, b itemIdentity) int { return strings.Compare(a.Identifier, b.Identifier) })
	game := conn.StartGame()
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(snapshot{
		ProtocolVersion: int(o.protocol.Number), LibraryVersion: o.protocol.Version,
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

func matchesServerMessage(text *mc.TextPacket, expected string) bool {
	switch text.TextType {
	case mc.TextTypeObject, mc.TextTypeObjectWhisper, mc.TextTypeObjectAnnouncement:
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
