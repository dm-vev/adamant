package client

import (
	"bytes"
	"context"
	"os"
	"reflect"
	"sort"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/dm-vev/adamant/net/bedrock"
	"github.com/dm-vev/adamant/net/bedrock/login"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/dm-vev/adamant/net/bedrock/wire"
)

// corpusCommands are run on BDS while the corpus is taken, for the packets
// a walk alone does not bring: items, mobs, effects, weather, scores,
// titles, the camera and more. BDS runs them for an operator with cheats
// on.
var corpusCommands = []string{
	"gamemode creative", "give @s diamond_sword", "give @s shield", "give @s enchanted_book",
	"give @s potion 1 5", "replaceitem entity @s slot.armor.head 0 diamond_helmet",
	"summon zombie ~3 ~ ~3", "summon villager ~-3 ~ ~3", "summon armor_stand ~ ~ ~4",
	"summon boat ~4 ~ ~", "summon xp_orb ~2 ~ ~", "summon lightning_bolt ~6 ~ ~6",
	"effect @s speed 30 1", "effect @s night_vision 30", "time set night", "weather rain",
	"weather thunder", "xp 100 @s", "title @s title Corpus", "title @s actionbar corpus",
	"playsound random.levelup @s", "particle minecraft:heart_particle ~ ~1 ~",
	"setblock ~1 ~ ~ chest", "setblock ~-1 ~ ~ oak_sign", "fill ~2 ~ ~2 ~4 ~2 ~4 glass",
	"scoreboard objectives add corpus dummy Corpus", "scoreboard objectives setdisplay sidebar corpus",
	"scoreboard players set @s corpus 7", "tp @s ~ ~3 ~", "camera @s fade time 0.1 0.1 0.1",
	"camera @s set minecraft:free pos ~ ~5 ~", "camera @s clear", "hud @s hide hotbar", "hud @s reset",
	"damage @s 2", "summon tnt ~5 ~ ~5", "kill @e[type=zombie]", "clear @s", "difficulty hard",
	"gamerule dodaylightcycle false", "say corpus", `tellraw @s {"rawtext":[{"text":"corpus"}]}`,
	"spawnpoint @s", "fog @s push minecraft:fog_crimson_forest corpus", "fog @s pop corpus",
	"inputpermission set @s jump disabled", "inputpermission set @s jump enabled", "gamemode survival",
}

// TestBDSCorpus joins the BDS whose signaling URL is BDS_ADDR, walks for
// BDS_SECONDS (30 if unset) while it runs corpusCommands, and checks that
// every packet the server sends decodes with nothing left over and
// encodes back to the same bytes. NBT compounds encode with their keys in
// order: a server that writes them in another order, as gophertunnel does,
// passes when what is encoded decodes to the same packet.
func TestBDSCorpus(t *testing.T) {
	addr := os.Getenv("BDS_ADDR")
	if addr == "" {
		t.Skip("BDS_ADDR is not set")
	}
	seconds := 30
	if s := os.Getenv("BDS_SECONDS"); s != "" {
		seconds, _ = strconv.Atoi(s)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	c, err := Dial(ctx, addr, Config{Identity: login.Identity{DisplayName: "Corpus"}})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	stop := time.Now().Add(time.Duration(seconds) * time.Second)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		// Walk: the server sends chunks, entities and blocks as the player
		// goes.
		defer func() { _ = c.nn.SetReadDeadline(time.Now()) }()
		pos := c.game.PlayerPosition
		commands := corpusCommands
		for tick := uint64(1); time.Now().Before(stop); tick++ {
			time.Sleep(50 * time.Millisecond)
			pos[0] += 0.2
			_ = c.WritePacket(&v2193.PlayerAuthInputPacket{Position: pos, Delta: [3]float32{0.2, 0, 0}, InputMode: 1, Tick: tick})
			if tick%5 == 0 && len(commands) > 0 {
				_ = c.WritePacket(&v2193.CommandRequestPacket{
					CommandLine:   "/" + commands[0],
					CommandOrigin: v2193.CommandOrigin{PlayerUniqueID: c.game.EntityUniqueID},
					Version:       "latest",
				})
				commands = commands[1:]
			}
			if c.Flush() != nil {
				return
			}
		}
	}()

	type stat struct{ n, failed, changed, reordered int }
	stats := map[uint32]*stat{}
	check := func(raw []byte) {
		h, body, err := bedrock.ReadHeader(raw)
		if err != nil {
			t.Errorf("header of % x: %v", raw[:min(len(raw), 8)], err)
			return
		}
		s := stats[h.ID]
		if s == nil {
			s = &stat{}
			stats[h.ID] = s
		}
		s.n++
		pk := v2193.New(h.ID)
		if pk == nil {
			s.failed++
			return
		}
		r := wire.NewReader(body)
		pk.Decode(r)
		if r.Err() != nil || r.Len() != 0 {
			if s.failed++; s.failed == 1 {
				t.Errorf("packet %d (%T): %v, %d bytes left of % x", h.ID, pk, r.Err(), r.Len(), body[:min(len(body), 64)])
			}
			return
		}
		var w wire.Writer
		pk.Encode(&w)
		if !bytes.Equal(w.B, body) {
			again := v2193.New(h.ID)
			r := wire.NewReader(w.B)
			again.Decode(r)
			if r.Err() == nil && reflect.DeepEqual(again, pk) {
				s.reordered++
				return
			}
			if s.changed++; s.changed == 1 {
				i := 0
				for i < min(len(w.B), len(body)) && w.B[i] == body[i] {
					i++
				}
				from := max(0, i-32)
				t.Errorf("packet %d (%T) of %d bytes encodes back to %d, first apart at %d\nBDS  % x\nours % x", h.ID, pk, len(body), len(w.B), i,
					body[from:min(len(body), i+48)], w.B[from:min(len(w.B), i+48)])
			}
		}
	}
	for _, raw := range c.deferred {
		check(raw)
	}
	c.deferred = nil
	for {
		raw, err := c.b.ReadPacket()
		if err != nil {
			break
		}
		check(raw)
	}
	wg.Wait()

	ids := make([]uint32, 0, len(stats))
	total := 0
	for id, s := range stats {
		ids = append(ids, id)
		total += s.n
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		s := stats[id]
		t.Logf("%4d %-40T %6d packets, %d did not decode, %d encode back differently, %d with NBT reordered", id, v2193.New(id), s.n, s.failed, s.changed, s.reordered)
	}
	t.Logf("%d packets of %d kinds", total, len(ids))
}
