package main

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
)

// workload is what every bot does, the same for all runs of a scenario.
type workload struct {
	Address      string        `json:"address"`
	Bots         int           `json:"bots"`
	JoinInterval time.Duration `json:"join_interval_ns"`
	Duration     time.Duration `json:"duration_ns"`
	ChunkRadius  int32         `json:"chunk_radius"`
	ChunkTimeout time.Duration `json:"chunk_timeout_ns"`
	ChatInterval time.Duration `json:"chat_interval_ns"`
	// Side is the side in blocks of the square each bot walks, at Speed
	// blocks a tick.
	Side  float64 `json:"side"`
	Speed float64 `json:"speed"`
	// Build places a block and breaks it again by turns every
	// BuildInterval; Inventory opens and closes the inventory every
	// InventoryInterval.
	Build             bool          `json:"build"`
	BuildInterval     time.Duration `json:"build_interval_ns"`
	Inventory         bool          `json:"inventory"`
	InventoryInterval time.Duration `json:"inventory_interval_ns"`
}

// tick is the time of a game tick: bots send their input once a tick, as
// the game does.
const tick = 50 * time.Millisecond

// counters are the numbers a bot counts while it runs, read at the edges
// of the measured window.
type counters struct {
	packets, packetBytes, wireBytes uint64
}

// bot is one client of the load.
type bot struct {
	idx  int
	name string
	w    *workload

	c      *conn
	joined time.Time // when the dial began

	spawnAfter  time.Duration // -1 if the bot did not spawn
	chunksAfter atomic.Int64  // nanoseconds, -1 if not (yet) all chunks arrived
	dialErr     string

	packets, packetBytes atomic.Uint64
	// byID counts the packets and their bytes by packet ID over the whole
	// run.
	byID         [1024]atomic.Uint64
	bytesByID    [1024]atomic.Uint64
	decodeErrors atomic.Uint64
	disconnect   atomic.Pointer[string]
	disconnectAt atomic.Int64 // Unix nanoseconds

	// The chunks of the view: those still awaited at the spawn, and the
	// columns whose sub-chunks were asked for but not received.
	chunkMu     sync.Mutex
	radius      int32
	awaited     map[[2]int32]bool
	subRequests [][3]int32
	chunksDone  chan struct{}
	// missing holds the chunks still awaited at the chunk timeout.
	missing         [][2]int32
	chunksAtTimeout [2]uint64

	// Chat lines sent and not yet received back, by line.
	chatMu   sync.Mutex
	chatSent map[string]time.Time
	chatRTT  []chatSample
	sentAt   []time.Time

	// Other players: where the bot that joined before this one is.
	peerMu      sync.Mutex
	peerID      uint64
	peerPos     [3]float32
	peerKnown   bool
	creativeID  uint32 // creative item network ID of the block placed, 0 if none
	creative    v2193.ItemStack
	itemReady   atomic.Bool
	windowOpen  atomic.Int32 // window ID of an open container, -1 if none
	invOpened   atomic.Uint64
	placed      atomic.Uint64
	broken      atomic.Uint64
	stackFailed atomic.Uint64
}

// chatSample is the round trip of one chat line.
type chatSample struct {
	sent time.Time
	rtt  time.Duration
}

func newBot(idx int, w *workload) *bot {
	b := &bot{idx: idx, name: fmt.Sprintf("Bot%02d", idx), w: w, spawnAfter: -1, chatSent: map[string]time.Time{}, chunksDone: make(chan struct{})}
	b.chunksAfter.Store(-1)
	b.windowOpen.Store(-1)
	return b
}

// join dials the server and returns once the bot spawned.
func (b *bot) join(ctx context.Context) error {
	b.joined = time.Now()
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	c, err := dial(ctx, b.w.Address, b.name, b.w.ChunkRadius)
	if err != nil {
		b.dialErr = err.Error()
		return err
	}
	b.c, b.spawnAfter = c, time.Since(b.joined)
	b.radius = b.w.ChunkRadius
	pos := c.game.PlayerPosition
	b.awaited = map[[2]int32]bool{}
	for _, p := range chunksWithinXZ(int32(math.Floor(float64(pos[0])))>>4, int32(math.Floor(float64(pos[2])))>>4, b.radius) {
		b.awaited[p] = true
	}
	return nil
}

// snapshot returns the counters as they are now.
func (b *bot) snapshot() counters {
	if b.c == nil {
		return counters{}
	}
	return counters{packets: b.packets.Load(), packetBytes: b.packetBytes.Load(), wireBytes: b.c.wireBytes()}
}

// read handles the packets of the server until the connection ends.
func (b *bot) read() {
	for {
		raw, err := b.c.readRaw()
		if err != nil {
			b.setDisconnect(err.Error())
			return
		}
		b.packets.Add(1)
		b.packetBytes.Add(uint64(len(raw)))
		id, body, err := header(raw)
		if err != nil {
			b.decodeErrors.Add(1)
			continue
		}
		b.byID[id&1023].Add(1)
		b.bytesByID[id&1023].Add(uint64(len(raw)))
		switch id {
		case 9, 58, 174, 70, 12, 19, 111, 145, 148, 46, 5:
		default:
			// Not looked at: counted only.
			continue
		}
		pk, err := decode(id, body)
		if err != nil {
			b.decodeErrors.Add(1)
			continue
		}
		b.handle(pk)
	}
}

// handle handles a packet the bot looks at.
func (b *bot) handle(pk proto.Packet) {
	now := time.Now()
	switch pk := pk.(type) {
	case *v2193.DisconnectPacket:
		b.setDisconnect("disconnected by the server: " + pk.Message)
	case *v2193.TextPacket:
		b.chat(pk, now)
	case *v2193.ChunkRadiusUpdatedPacket:
		b.chunkMu.Lock()
		if pk.ChunkRadius < b.radius && b.chunksAfter.Load() < 0 {
			// The server caps the radius: only the chunks within its own are
			// sent.
			b.radius = pk.ChunkRadius
			for p := range b.awaited {
				if chunkDistance(p[0]-b.spawnChunk()[0], p[1]-b.spawnChunk()[1]) > b.radius {
					delete(b.awaited, p)
				}
			}
		}
		b.chunkMu.Unlock()
		b.checkChunks(now)
	case *v2193.LevelChunkPacket:
		pos := [2]int32{pk.Position[0], pk.Position[1]}
		if !pk.SubChunkLimit.Set || pk.SubChunkLimit.Val <= 0 {
			b.chunkArrived(pos, now)
			return
		}
		b.chunkMu.Lock()
		b.subRequests = append(b.subRequests, [3]int32{pos[0], pk.SubChunkLimit.Val, pos[1]})
		b.chunkMu.Unlock()
	case *v2193.SubChunkPacket:
		b.chunkArrived([2]int32{pk.Position[0], pk.Position[2]}, now)
	case *v2193.AddPlayerPacket:
		if pk.Username == b.peerName() {
			b.peerMu.Lock()
			b.peerID, b.peerPos, b.peerKnown = pk.EntityRuntimeID, pk.Position, true
			b.peerMu.Unlock()
		}
	case *v2193.MovePlayerPacket:
		b.peerMu.Lock()
		if b.peerKnown && pk.EntityRuntimeID == b.peerID {
			b.peerPos = pk.Position
		}
		b.peerMu.Unlock()
	case *v2193.MoveActorDeltaPacket:
		b.peerMu.Lock()
		if b.peerKnown && pk.EntityRuntimeID == b.peerID {
			for i, v := range []proto.Optional[float32]{pk.PositionX, pk.PositionY, pk.PositionZ} {
				if v.Set {
					b.peerPos[i] = v.Val
				}
			}
		}
		b.peerMu.Unlock()
	case *v2193.CreativeContentPacket:
		b.pickCreativeItem(pk)
	case *v2193.ItemStackResponsePacket:
		for _, r := range pk.Responses {
			if r.Status == v2193.ItemStackResponseStatusOK {
				b.itemReady.Store(true)
			} else {
				b.stackFailed.Add(1)
			}
		}
	case *v2193.ContainerOpenPacket:
		b.windowOpen.Store(int32(pk.WindowID))
		b.invOpened.Add(1)
	}
}

// setDisconnect records why the connection ended, the first reason only.
func (b *bot) setDisconnect(msg string) {
	if b.disconnect.CompareAndSwap(nil, &msg) {
		b.disconnectAt.Store(time.Now().UnixNano())
	}
}

// spawnChunk returns the chunk the bot spawned in.
func (b *bot) spawnChunk() [2]int32 {
	pos := b.c.game.PlayerPosition
	return [2]int32{int32(math.Floor(float64(pos[0]))) >> 4, int32(math.Floor(float64(pos[2]))) >> 4}
}

// chunkArrived marks the chunk at pos as received.
func (b *bot) chunkArrived(pos [2]int32, now time.Time) {
	b.chunkMu.Lock()
	delete(b.awaited, pos)
	b.chunkMu.Unlock()
	b.checkChunks(now)
}

// checkChunks records the time all chunks of the view had arrived at.
func (b *bot) checkChunks(now time.Time) {
	b.chunkMu.Lock()
	done := len(b.awaited) == 0
	b.chunkMu.Unlock()
	if done && b.chunksAfter.CompareAndSwap(-1, int64(now.Sub(b.joined))) {
		close(b.chunksDone)
	}
}

// peerName returns the name of the bot this one looks at: the one that
// joined before it, or the last for the first.
func (b *bot) peerName() string {
	peer := b.idx - 1
	if peer < 0 {
		peer = b.w.Bots - 1
	}
	return fmt.Sprintf("Bot%02d", peer)
}

// chatToken is the start of every chat line of the load.
const chatToken = "lt:"

// chat matches a text the server sent with a chat line the bot sent.
func (b *bot) chat(pk *v2193.TextPacket, now time.Time) {
	line := ""
	for _, s := range append([]string{pk.Message}, pk.Parameters...) {
		if i := strings.Index(s, chatToken); i >= 0 {
			line = s[i:]
			break
		}
	}
	if line == "" {
		return
	}
	b.chatMu.Lock()
	if sent, ok := b.chatSent[line]; ok {
		delete(b.chatSent, line)
		b.chatRTT = append(b.chatRTT, chatSample{sent: sent, rtt: now.Sub(sent)})
	}
	b.chatMu.Unlock()
}

// pickCreativeItem selects the creative item of stone, to place.
func (b *bot) pickCreativeItem(pk *v2193.CreativeContentPacket) {
	stone := int32(-1)
	for _, it := range b.c.items {
		if it.Name == "minecraft:stone" {
			stone = int32(it.RuntimeID)
		}
	}
	for _, it := range pk.Items {
		if it.Item.NetworkID == stone && it.Item.BlockRuntimeID != 0 {
			b.peerMu.Lock()
			b.creativeID, b.creative = it.CreativeItemNetworkID, it.Item
			b.creative.Count = 64
			b.peerMu.Unlock()
			return
		}
	}
}

// play runs the workload of the bot from its spawn until stop, the Unix
// time in nanoseconds the run ends at, zero while it is not known yet.
func (b *bot) play(stop *atomic.Int64) {
	c := b.c
	spawn := c.game.PlayerPosition
	pos := spawn
	var t uint64
	ticker := time.NewTicker(tick)
	defer ticker.Stop()

	chunkDeadline := b.joined.Add(b.w.ChunkTimeout)
	walking := false
	var walked float64
	// The square of the bot, turned by its index so that bots spread.
	angle := 2 * math.Pi * float64(b.idx) / float64(max(b.w.Bots, 1))
	chatOffset := time.Duration(0)
	if b.w.Bots > 0 {
		chatOffset = b.w.ChatInterval * time.Duration(b.idx) / time.Duration(b.w.Bots)
	}
	var nextChat, nextBuild, nextInventory time.Time
	var chatSeq int
	var requestedItem, placedBlock bool
	var buildPos [3]int32
	var closeInventoryAt time.Time

	for now := range ticker.C {
		if end := stop.Load(); (end != 0 && now.UnixNano() > end) || b.disconnect.Load() != nil {
			return
		}
		t++
		if !walking {
			select {
			case <-b.chunksDone:
				walking = true
				nextChat = now.Add(chatOffset)
				nextBuild = now.Add(b.w.BuildInterval * time.Duration(b.idx+1) / time.Duration(max(b.w.Bots, 1)))
				nextInventory = now.Add(b.w.InventoryInterval * time.Duration(b.idx+1) / time.Duration(max(b.w.Bots, 1)))
			default:
				if now.After(chunkDeadline) {
					b.chunkMu.Lock()
					for p := range b.awaited {
						b.missing = append(b.missing, p)
					}
					b.chunkMu.Unlock()
					b.chunksAtTimeout = [2]uint64{b.byID[58].Load(), b.byID[174].Load()}
					// Walk anyway: the join is counted as one that did not get
					// its chunks.
					walking = true
					nextChat = now.Add(chatOffset)
					nextBuild, nextInventory = now.Add(b.w.BuildInterval), now.Add(b.w.InventoryInterval)
				}
			}
		}
		// Sub-chunks the server announced, asked for as the game asks.
		b.chunkMu.Lock()
		requests := b.subRequests
		b.subRequests = nil
		b.chunkMu.Unlock()
		for _, r := range requests {
			offsets := make([]v2193.SubChunkOffset, r[1])
			for i := range offsets {
				offsets[i] = v2193.SubChunkOffset{0, int8(i), 0}
			}
			_ = c.write(&v2193.SubChunkRequestPacket{Dimension: 0, Position: v2193.SubChunkPos{r[0], -4, r[2]}, Offsets: offsets})
		}

		var delta [3]float32
		if walking {
			walked += b.w.Speed
			next := squarePoint(spawn, b.w.Side, angle, walked)
			delta = [3]float32{next[0] - pos[0], next[1] - pos[1], next[2] - pos[2]}
			pos = next
		}
		yaw := float32(math.Atan2(-float64(delta[0]), float64(delta[2])) * 180 / math.Pi)
		b.peerMu.Lock()
		if b.peerKnown {
			dx, dz := float64(b.peerPos[0]-pos[0]), float64(b.peerPos[2]-pos[2])
			if dx != 0 || dz != 0 {
				yaw = float32(math.Atan2(-dx, dz) * 180 / math.Pi)
			}
		}
		creativeID, held := b.creativeID, b.creative
		b.peerMu.Unlock()

		input := &v2193.PlayerAuthInputPacket{
			Yaw: yaw, HeadYaw: yaw, Position: pos, Delta: delta, Tick: t,
			InputMode: 1, PlayMode: 0, InteractionModel: 0,
			MoveVector: [2]float32{0, 1}, RawMoveVector: [2]float32{0, 1}, AnalogueMoveVector: [2]float32{0, 1},
		}
		if !walking {
			input.MoveVector, input.RawMoveVector, input.AnalogueMoveVector = [2]float32{}, [2]float32{}, [2]float32{}
		}

		if walking && b.w.Build && creativeID != 0 && !requestedItem {
			// Take a stack of stone from the creative inventory into the first
			// slot of the hotbar.
			requestedItem = true
			id := int32(-1 - 2*b.idx)
			_ = c.write(&v2193.ItemStackRequestPacket{Requests: []v2193.ItemStackRequest{{
				RequestID: id,
				Actions: []v2193.StackRequestAction{
					&v2193.CraftCreativeStackRequestAction{CreativeItemNetworkID: creativeID, NumberOfCrafts: 1},
					&v2193.PlaceStackRequestAction{
						Count:       64,
						Source:      v2193.StackRequestSlotInfo{Container: v2193.FullContainerName{ContainerID: v2193.ContainerCreatedOutput}, Slot: 50, StackNetworkID: id},
						Destination: v2193.StackRequestSlotInfo{Container: v2193.FullContainerName{ContainerID: v2193.ContainerHotBar}, Slot: 0},
					},
				},
			}}})
		}
		if walking && b.w.Build && b.itemReady.Load() && now.After(nextBuild) {
			nextBuild = now.Add(b.w.BuildInterval)
			if !placedBlock {
				// Place stone on the ground two blocks beside the bot.
				feet := int32(math.Floor(float64(pos[1]) - 1.62))
				buildPos = [3]int32{int32(math.Floor(float64(pos[0]))) + 2, feet, int32(math.Floor(float64(pos[2])))}
				_ = c.write(&v2193.InventoryTransactionPacket{TransactionData: &v2193.UseItemTransactionData{
					ActionType:       v2193.UseItemActionClickBlock,
					TriggerType:      v2193.TriggerTypePlayerInput,
					BlockPosition:    [3]int32{buildPos[0], buildPos[1] - 1, buildPos[2]},
					BlockFace:        1,
					HotBarSlot:       0,
					HeldItem:         v2193.ItemInstance{Stack: held},
					Position:         pos,
					ClickedPosition:  [3]float32{0.5, 1, 0.5},
					ClientPrediction: v2193.ClientPredictionSuccess,
				}})
				placedBlock = true
				b.placed.Add(1)
			} else {
				// Break the block placed, as a player in creative mode does.
				input.InputData = v2193.InputFlags{v2193.InputFlagPerformBlockActions}
				input.BlockActions = proto.Some([]v2193.PlayerBlockAction{
					{Action: v2193.PlayerActionStartBreak, BlockPos: buildPos, Face: 1},
					{Action: v2193.PlayerActionPredictDestroyBlock, BlockPos: buildPos, Face: 1},
				})
				placedBlock = false
				b.broken.Add(1)
			}
		}
		_ = c.write(input)

		if walking && b.w.Inventory {
			if w := b.windowOpen.Load(); w >= 0 && closeInventoryAt.IsZero() {
				closeInventoryAt = now.Add(time.Second)
			}
			if !closeInventoryAt.IsZero() && now.After(closeInventoryAt) {
				_ = c.write(&v2193.ContainerClosePacket{WindowID: uint8(b.windowOpen.Load())})
				b.windowOpen.Store(-1)
				closeInventoryAt = time.Time{}
			}
			if now.After(nextInventory) && b.windowOpen.Load() < 0 {
				nextInventory = now.Add(b.w.InventoryInterval)
				_ = c.write(&v2193.InteractPacket{ActionType: v2193.InteractActionOpenInventory, TargetEntityRuntimeID: c.game.EntityRuntimeID})
			}
		}
		if walking && b.w.ChatInterval > 0 && now.After(nextChat) {
			nextChat = nextChat.Add(b.w.ChatInterval)
			line := chatToken + strconv.Itoa(b.idx) + ":" + strconv.Itoa(chatSeq)
			chatSeq++
			b.chatMu.Lock()
			b.chatSent[line] = time.Now()
			b.sentAt = append(b.sentAt, time.Now())
			b.chatMu.Unlock()
			_ = c.write(&v2193.TextPacket{TextType: v2193.TextTypeChat, SourceName: b.name, Message: line})
		}
		if err := c.flush(); err != nil {
			b.setDisconnect("flush: " + err.Error())
			return
		}
	}
}

// squarePoint returns the point a distance d along a square of a side,
// with a corner at start, turned by angle around start.
func squarePoint(start [3]float32, side, angle, d float64) [3]float32 {
	d = math.Mod(d, 4*side)
	var x, z float64
	switch {
	case d < side:
		x, z = d, 0
	case d < 2*side:
		x, z = side, d-side
	case d < 3*side:
		x, z = 3*side-d, side
	default:
		x, z = 0, 4*side-d
	}
	sin, cos := math.Sincos(angle)
	return [3]float32{start[0] + float32(x*cos-z*sin), start[1], start[2] + float32(x*sin+z*cos)}
}

// chunksWithinXZ returns the chunk positions within a radius r around the
// chunk (x, z), as a loader of the game counts them.
func chunksWithinXZ(x, z, r int32) [][2]int32 {
	var out [][2]int32
	for dx := -r; dx <= r; dx++ {
		for dz := -r; dz <= r; dz++ {
			if chunkDistance(dx, dz) <= r {
				out = append(out, [2]int32{x + dx, z + dz})
			}
		}
	}
	return out
}

// chunkDistance is the distance of a chunk offset, rounded, as the loaders
// of the game measure it.
func chunkDistance(dx, dz int32) int32 {
	return int32(math.Round(math.Sqrt(float64(dx)*float64(dx) + float64(dz)*float64(dz))))
}
