package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"net"
	"os"
	"time"

	"github.com/go-gl/mathgl/mgl32"
	"github.com/google/uuid"
	"github.com/sandertv/gophertunnel/minecraft"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/login"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

type command struct {
	Action    string
	Block     protocol.BlockPos
	Face      int32
	Hit       *mgl32.Vec3
	Feet      *mgl32.Vec3
	RuntimeID *uint32 `json:"runtime_id"`
}

func emit(v any) { b, _ := json.Marshal(v); fmt.Println(string(b)) }
func main() {
	if len(os.Args) != 2 {
		panic("local address required")
	}
	host, _, err := net.SplitHostPort(os.Args[1])
	if err != nil || !net.ParseIP(host).IsLoopback() {
		panic("the audit client only connects to an explicit loopback IP")
	}
	conn, err := (minecraft.Dialer{IdentityData: login.IdentityData{DisplayName: "RedstoneAudit", Identity: uuid.NewString()}}).DialTimeout("raknet", os.Args[1], 20*time.Second)
	if err != nil {
		panic(err)
	}
	defer conn.Close()
	clockStart := time.Now()
	if err = conn.DoSpawnTimeout(25 * time.Second); err != nil {
		panic(err)
	}
	// Without this acknowledgement BDS spawns the player but ignores input.
	if err = conn.WritePacket(&packet.ServerBoundLoadingScreen{Type: packet.LoadingScreenTypeEnd}); err != nil {
		panic(err)
	}
	if err = conn.Flush(); err != nil {
		panic(err)
	}
	pos := conn.GameData().PlayerPosition
	emit(map[string]any{"ready": true, "position": pos, "time": conn.GameData().Time, "permissions": conn.GameData().PlayerPermissions})
	incoming := make(chan packet.Packet, 64)
	errc := make(chan error, 1)
	go func() {
		for {
			p, e := conn.ReadPacket()
			if e != nil {
				errc <- e
				return
			}
			incoming <- p
		}
	}()
	commands := make(chan command)
	go func() {
		s := bufio.NewScanner(os.Stdin)
		for s.Scan() {
			var c command
			if e := json.Unmarshal(s.Bytes(), &c); e != nil {
				panic(e)
			}
			commands <- c
		}
		close(commands)
	}()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	tick := uint64(conn.GameData().Time) + uint64(time.Since(clockStart)/(50*time.Millisecond))
	teleported := false
	var held protocol.ItemInstance
	blocks := map[protocol.BlockPos]uint32{}
	var stopMining *protocol.PlayerBlockAction
	send := func(use *protocol.UseItemTransactionData) {
		tick++
		p := &packet.PlayerAuthInput{InputData: protocol.NewInputFlags(packet.InputFlagCount), Position: pos, Tick: tick, Pitch: 40, Yaw: 180, HeadYaw: 180, InteractPitch: 40, InteractYaw: 180, InputMode: packet.InputModeMouse, InteractionModel: packet.InteractionModelCrosshair, CameraOrientation: mgl32.Vec3{0, 0, 1}}
		p.InputData.Set(packet.InputFlagClientAckServerData)
		p.InputData.Set(packet.InputFlagInternalUpdate)
		if stopMining != nil {
			p.InputData.Set(packet.InputFlagPerformBlockActions)
			p.BlockActions = protocol.Option([]protocol.PlayerBlockAction{*stopMining})
			stopMining = nil
		}
		if teleported {
			p.InputData.Set(packet.InputFlagHandledTeleport)
			teleported = false
		}
		if use != nil {
			direction := mgl32.Vec3{float32(use.BlockPosition[0]), float32(use.BlockPosition[1]), float32(use.BlockPosition[2])}.Add(use.ClickedPosition).Sub(pos).Normalize()
			p.Pitch = float32(-math.Asin(float64(direction[1])) * 180 / math.Pi)
			p.Yaw = float32(-math.Atan2(float64(direction[0]), float64(direction[2])) * 180 / math.Pi)
			p.InteractPitch = p.Pitch
			p.InteractYaw = p.Yaw
			p.HeadYaw = p.Yaw
			p.CameraOrientation = direction

			if use.ActionType == protocol.UseItemActionClickBlock {
				p.ItemInteractionData = protocol.Option(*use)
				p.InputData.Set(packet.InputFlagPerformItemInteraction)
				p.InputData.Set(packet.InputFlagStartUsingItem)
			} else {
				// The native server uses authenticated block actions for
				// instant mining; a use-item break transaction is ignored.
				p.InputData.Set(packet.InputFlagPerformBlockActions)
				p.BlockActions = protocol.Option([]protocol.PlayerBlockAction{{Action: protocol.PlayerActionStartBreak, BlockPos: use.BlockPosition, Face: use.BlockFace}, {Action: protocol.PlayerActionPredictDestroyBlock, BlockPos: use.BlockPosition, Face: use.BlockFace}})
				stopMining = &protocol.PlayerBlockAction{Action: protocol.PlayerActionAbortBreak, BlockPos: use.BlockPosition, Face: use.BlockFace}
			}
		}
		if err := conn.WritePacket(&packet.MovePlayer{EntityRuntimeID: conn.GameData().EntityRuntimeID, Position: pos, Pitch: p.Pitch, Yaw: p.Yaw, HeadYaw: p.Yaw, OnGround: true, Tick: tick}); err != nil {
			panic(err)
		}
		if e := conn.WritePacket(p); e != nil {
			panic(e)
		}
		if e := conn.Flush(); e != nil {
			panic(e)
		}
	}
	var pending *command
	var queuedUse *protocol.UseItemTransactionData
	dispatch := func() {
		if pending == nil {
			return
		}
		c := *pending
		if c.Feet != nil && pos.Sub(c.Feet.Add(mgl32.Vec3{0, 1.62001, 0})).Len() > 0.03 {
			return
		}
		rid := blocks[c.Block]
		if c.RuntimeID != nil {
			rid = *c.RuntimeID
		}
		hit := mgl32.Vec3{0.5, 0.5, 0.5}
		if c.Hit != nil {
			hit = *c.Hit
		}
		use := &protocol.UseItemTransactionData{ActionType: protocol.UseItemActionClickBlock, TriggerType: protocol.TriggerTypePlayerInput, BlockPosition: c.Block, BlockFace: c.Face, Position: pos, ClickedPosition: hit, BlockRuntimeID: rid, HeldItem: held, ClientPrediction: protocol.ClientPredictionSuccess}
		if c.Action == "break" {
			use.ActionType = protocol.UseItemActionBreakBlock
		}
		if c.Action == "legacy" {
			if err := conn.WritePacket(&packet.InventoryTransaction{TransactionData: use}); err != nil {
				panic(err)
			}
			if err := conn.Flush(); err != nil {
				panic(err)
			}
		} else {
			queuedUse = use
		}
		if c.Action == "legacy" {
			emit(map[string]any{"sent": c.Action, "tick": tick, "block": c.Block, "rid": rid, "position": pos})
		}
		pending = nil
	}
	for {
		select {
		case e := <-errc:
			panic(e)
		case p := <-incoming:
			switch x := p.(type) {
			case *packet.InventoryContent:
				if x.WindowID == protocol.WindowIDInventory && len(x.Content) > 0 {
					held = x.Content[0]
					emit(map[string]any{"held": held})
				}
			case *packet.InventorySlot:
				if x.WindowID == protocol.WindowIDInventory && x.Slot == 0 {
					held = x.NewItem
					emit(map[string]any{"held": held})
				}
			case *packet.UpdateBlock:
				if x.Layer == 0 {
					blocks[x.Position] = x.NewBlockRuntimeID
				}
			case *packet.MovePlayer:
				if x.EntityRuntimeID == conn.GameData().EntityRuntimeID {
					pos = x.Position
					teleported = true
					emit(map[string]any{"position": pos, "mode": x.Mode})
				}
			case *packet.CorrectPlayerMovePrediction:
				pos = x.Position
				teleported = true
			case *packet.NetworkStackLatency:
				if x.NeedsResponse {
					_ = conn.WritePacket(&packet.NetworkStackLatency{Timestamp: x.Timestamp})
				}
			case *packet.LevelSoundEvent:
				emit(map[string]any{"sound": x})
			case *packet.LevelEvent:
				emit(map[string]any{"level_event": x})
			case *packet.Disconnect:
				panic(x.Message)
			}
		case <-ticker.C:
			// The real client sends one authenticated input per game tick.
			// Extra packets for clicks would make its simulation clock drift.
			send(queuedUse)
			if queuedUse != nil {
				emit(map[string]any{"sent": "interaction", "action_type": queuedUse.ActionType, "tick": tick, "block": queuedUse.BlockPosition, "rid": queuedUse.BlockRuntimeID, "position": pos, "held": queuedUse.HeldItem})
				queuedUse = nil
			}
		case c, ok := <-commands:
			if !ok || c.Action == "stop" {
				return
			}
			if c.Action != "click" && c.Action != "legacy" && c.Action != "break" {
				panic("unknown audit action")
			}
			if pending != nil || queuedUse != nil {
				panic("audit action still waiting for teleport/block data")
			}
			pending = &c
			emit(map[string]any{"queued": c.Action, "position": pos, "feet": c.Feet, "known_blocks": len(blocks), "rid": blocks[c.Block]})
		}
		dispatch()
	}
}
