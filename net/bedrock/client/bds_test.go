package client

import (
	"bytes"
	"context"
	"os"
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

// TestBDSCorpus joins the BDS whose signaling URL is BDS_ADDR, walks for
// BDS_SECONDS (30 if unset), and checks that every packet the server
// sends decodes with nothing left over and encodes back to the same bytes.
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
		pos := c.game.PlayerPosition
		for tick := uint64(1); time.Now().Before(stop); tick++ {
			time.Sleep(50 * time.Millisecond)
			pos[0] += 0.2
			_ = c.WritePacket(&v2193.PlayerAuthInputPacket{Position: pos, Delta: [3]float32{0.2, 0, 0}, InputMode: 1, Tick: tick})
			if c.Flush() != nil {
				return
			}
		}
		_ = c.nn.SetReadDeadline(time.Now())
	}()

	type stat struct{ n, failed, changed int }
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
		t.Logf("%4d %-40T %6d packets, %d did not decode, %d encode back differently", id, v2193.New(id), s.n, s.failed, s.changed)
	}
	t.Logf("%d packets of %d kinds", total, len(ids))
}
