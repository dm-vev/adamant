package session

import (
	"encoding/json"
	"fmt"
	"image/color"
	"maps"
	"math"
	"net"
	"slices"
	"sync/atomic"
	"time"
	_ "unsafe" // Imported for compiler directives.

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/item/recipe"
	"github.com/df-mc/dragonfly/server/player/debug"
	"github.com/df-mc/dragonfly/server/player/dialogue"
	"github.com/df-mc/dragonfly/server/player/form"
	"github.com/df-mc/dragonfly/server/player/hud"
	"github.com/df-mc/dragonfly/server/player/input"
	"github.com/df-mc/dragonfly/server/player/skin"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/go-gl/mathgl/mgl32"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// StopShowingEntity stops showing a world.Entity to the Session. It will be completely invisible until a call to
// StartShowingEntity is made.
func (s *Session) StopShowingEntity(e world.Entity) {
	s.entityMutex.Lock()
	_, ok := s.hiddenEntities[e.H().UUID()]
	if !ok {
		s.hiddenEntities[e.H().UUID()] = struct{}{}
		s.hidesEntities.Store(true)
	}
	s.entityMutex.Unlock()

	if !ok {
		s.HideEntity(e)
	}
}

// StartShowingEntity starts showing a world.Entity to the Session that was previously hidden using StopShowingEntity.
func (s *Session) StartShowingEntity(e world.Entity) {
	s.entityMutex.Lock()
	_, ok := s.hiddenEntities[e.H().UUID()]
	if ok {
		delete(s.hiddenEntities, e.H().UUID())
		s.hidesEntities.Store(len(s.hiddenEntities) > 0)
	}
	s.entityMutex.Unlock()

	if ok {
		s.ViewEntity(e)
		s.ViewEntityState(e)
		s.ViewEntityItems(e)
		s.ViewEntityArmour(e)
	}
}

// closeCurrentContainer closes the container the player might currently have open.
func (s *Session) closeCurrentContainer(tx *world.Tx, clientRequested bool) {
	openedEntity := s.openedEntity.Load()
	openedPos := s.openedPos.Load()
	if !s.closeWindow(clientRequested) {
		return
	}

	if openedEntity != nil {
		if ent, ok := openedEntity.Entity(tx); ok {
			if container, ok := ent.(interface {
				RemoveViewer(v block.ContainerViewer)
			}); ok {
				container.RemoveViewer(s)
			}
			if trader, ok := ent.(entity.TradingMerchant); ok {
				trader.StopTrading(s.ent)
			}
		}
		return
	}
	if openedPos == nil {
		return
	}
	pos := *openedPos
	b := tx.Block(pos)
	if container, ok := b.(block.Container); ok {
		container.RemoveViewer(s, tx, pos)
	} else if enderChest, ok := b.(block.EnderChest); ok {
		enderChest.RemoveViewer(tx, pos)
	}
}

// SendRespawn spawns the Controllable entity of the session client-side in the world, provided it has died.
func (s *Session) SendRespawn(pos mgl64.Vec3, c Controllable) {
	s.writePacket(&v2193.RespawnPacket{
		Position:        vec64To32(entityNetworkPosition(c, pos)),
		State:           packet.RespawnStateReadyToSpawn,
		EntityRuntimeID: selfEntityRuntimeID,
	})
}

// SendPlayerSpawn updates the player's spawn point on the client-side. There is currently little reason
// to do so other than to prevent the client-side "Respawn point set" message when sleeping in a bed.
func (s *Session) SendPlayerSpawn(pos mgl64.Vec3) {
	blockPos := protocol.BlockPos{int32(pos[0]), int32(pos[1]), int32(pos[2])}
	s.writePacket(&v2193.SetSpawnPositionPacket{
		SpawnType:     packet.SpawnTypePlayer,
		Position:      blockPos,
		Dimension:     packet.DimensionOverworld,
		SpawnPosition: blockPos,
	})
}

// sendBiomes sends all the vanilla biomes to the session.
func (s *Session) sendBiomes() {
	definitions, stringList := world.BiomeDefinitions()
	s.writePacket(&v2193.BiomeDefinitionListPacket{
		BiomeDefinitions: definitions,
		StringList:       stringList,
	})
}

// sendRecipes sends the current crafting recipes to the session.
func (s *Session) sendRecipes() {
	d := recipesFor(s.br)
	s.recipes = maps.Clone(d.recipes)
	s.javaRecipeIDs = maps.Clone(d.javaIDs)
	s.writePacket(d.packet)
}

// UnlockRecipes sends newly unlocked Java recipes that have a corresponding
// recipe in the current Bedrock crafting data. It returns the Java IDs accepted.
func (s *Session) UnlockRecipes(javaIDs []string) []string {
	return s.unlockRecipes(javaIDs, packet.UnlockedRecipesTypeNewlyUnlocked)
}

// SendInitiallyUnlockedRecipes sends persisted recipe unlocks after the player
// has joined and the crafting data has been sent.
func (s *Session) SendInitiallyUnlockedRecipes(javaIDs []string) {
	_ = s.unlockRecipes(javaIDs, packet.UnlockedRecipesTypeInitiallyUnlocked)
}

func (s *Session) unlockRecipes(javaIDs []string, unlockType uint32) []string {
	accepted := make([]string, 0, len(javaIDs))
	packetIDs := make([]string, 0, len(javaIDs))
	seenPacketIDs := make(map[string]struct{}, len(javaIDs))
	for _, javaID := range javaIDs {
		ids, ok := s.javaRecipeIDs[javaID]
		if !ok {
			continue
		}
		accepted = append(accepted, javaID)
		for _, packetID := range ids {
			if _, seen := seenPacketIDs[packetID]; seen {
				continue
			}
			seenPacketIDs[packetID] = struct{}{}
			packetIDs = append(packetIDs, packetID)
		}
	}
	if len(packetIDs) != 0 {
		s.writePacket(&v2193.UnlockedRecipesPacket{UnlockType: unlockType, Recipes: packetIDs})
	}
	return accepted
}

// sendArmourTrimData sends the armour trim data.
func (s *Session) sendArmourTrimData() {
	var trimPatterns []v2193.TrimPattern
	var trimMaterials []v2193.TrimMaterial

	for _, t := range item.SmithingTemplates() {
		if t == item.TemplateNetheriteUpgrade() {
			continue
		}
		name, _ := item.SmithingTemplate{Template: t}.EncodeItem()
		trimPatterns = append(trimPatterns, v2193.TrimPattern{
			ItemName:  name,
			PatternID: t.String(),
		})
	}

	for _, i := range item.ArmourTrimMaterials() {
		if material, ok := i.(item.ArmourTrimMaterial); ok {
			name, _ := i.EncodeItem()

			trimMaterials = append(trimMaterials, v2193.TrimMaterial{
				MaterialID: material.TrimMaterial(),
				Colour:     material.MaterialColour(),
				ItemName:   name,
			})
		}
	}

	s.writePacket(&v2193.TrimDataPacket{Patterns: trimPatterns, Materials: trimMaterials})
}

// ResyncInventory resends the main and off-hand inventories after an unfulfilled client prediction.
func (s *Session) ResyncInventory() {
	s.sendInv(s.inv, protocol.WindowIDInventory)
	s.sendInv(s.offHand, protocol.WindowIDOffHand)
}

// sendInv sends the inventory passed to the client with the window ID.
func (s *Session) sendInv(inv *inventory.Inventory, windowID uint32) {
	pk := &v2193.InventoryContentPacket{
		WindowID: windowID,
		Content:  make([]v2193.ItemInstance, 0, inv.Size()),
	}
	for _, i := range inv.Slots() {
		pk.Content = append(pk.Content, instanceFromItem(s.br, i))
	}
	s.writePacket(pk)
}

// sendItem sends the item stack passed to the client with the window ID and slot passed.
func (s *Session) sendItem(item item.Stack, slot int, windowID uint32) {
	s.writePacket(&v2193.InventorySlotPacket{
		WindowID: windowID,
		Slot:     uint32(slot),
		NewItem:  instanceFromItem(s.br, item),
	})
}

const (
	craftingGridSizeSmall   = 4
	craftingGridSizeLarge   = 9
	craftingGridSmallOffset = 28
	craftingGridLargeOffset = 32
	craftingResult          = 50
)

// smelter is an interface representing a block used to smelt items.
type smelter interface {
	// ResetExperience resets the collected experience of the smelter, and returns the amount of experience that was reset.
	ResetExperience() int
}

// invByID attempts to return an inventory by the ID passed. If found, the inventory is returned and the bool
// returned is true.
func (s *Session) invByID(id int32, tx *world.Tx) (*inventory.Inventory, bool) {
	switch id {
	case protocol.ContainerCraftingInput, protocol.ContainerCreatedOutput, protocol.ContainerCursor:
		// UI inventory.
		return s.ui, true
	case protocol.ContainerHotBar, protocol.ContainerInventory, protocol.ContainerCombinedHotBarAndInventory:
		// Hotbar 'inventory', rest of inventory, inventory when container is opened.
		return s.inv, true
	case protocol.ContainerOffhand:
		return s.offHand, true
	case protocol.ContainerArmor:
		// Armour inventory.
		return s.armour.Inventory(), true
	default:
		if !s.containerOpened.Load() {
			return nil, false
		}
		if id == protocol.ContainerLevelEntity {
			return s.openedWindow.Load(), true
		}
		if s.merchantTradeOpen() {
			switch id {
			case protocol.ContainerTradeIngredientOne, protocol.ContainerTradeIngredientTwo,
				protocol.ContainerTradeTwoIngredientOne, protocol.ContainerTradeTwoIngredientTwo:
				// The trade screen's inputs are UI inventory slots 4 and 5.
				return s.ui, true
			}
		}
		if s.openedEntity.Load() != nil {
			return nil, false
		}
		switch id {
		case protocol.ContainerShulkerBox:
			if pos := s.openedPos.Load(); pos != nil {
				if _, ok := tx.Block(*pos).(block.ShulkerBox); ok {
					return s.openedWindow.Load(), true
				}
			}
		case protocol.ContainerBarrel:
			if _, barrel := tx.Block(*s.openedPos.Load()).(block.Barrel); barrel {
				return s.openedWindow.Load(), true
			}
		case protocol.ContainerBeaconPayment:
			if _, beacon := tx.Block(*s.openedPos.Load()).(block.Beacon); beacon {
				return s.ui, true
			}
		case protocol.ContainerBrewingStandInput, protocol.ContainerBrewingStandResult, protocol.ContainerBrewingStandFuel:
			if _, brewingStand := tx.Block(*s.openedPos.Load()).(block.BrewingStand); brewingStand {
				return s.openedWindow.Load(), true
			}
		case protocol.ContainerAnvilInput, protocol.ContainerAnvilMaterial:
			if _, anvil := tx.Block(*s.openedPos.Load()).(block.Anvil); anvil {
				return s.ui, true
			}
		case protocol.ContainerSmithingTableTemplate, protocol.ContainerSmithingTableInput, protocol.ContainerSmithingTableMaterial:
			if _, smithing := tx.Block(*s.openedPos.Load()).(block.SmithingTable); smithing {
				return s.ui, true
			}
		case protocol.ContainerLoomInput, protocol.ContainerLoomDye, protocol.ContainerLoomMaterial:
			if _, loom := tx.Block(*s.openedPos.Load()).(block.Loom); loom {
				return s.ui, true
			}
		case protocol.ContainerCartographyInput, protocol.ContainerCartographyAdditional:
			if _, ok := tx.Block(*s.openedPos.Load()).(block.CartographyTable); ok {
				return s.ui, true
			}
		case protocol.ContainerStonecutterInput:
			if _, ok := tx.Block(*s.openedPos.Load()).(block.Stonecutter); ok {
				return s.ui, true
			}
		case protocol.ContainerGrindstoneInput, protocol.ContainerGrindstoneAdditional:
			if _, ok := tx.Block(*s.openedPos.Load()).(block.Grindstone); ok {
				return s.ui, true
			}
		case protocol.ContainerEnchantingInput, protocol.ContainerEnchantingMaterial:
			if _, enchanting := tx.Block(*s.openedPos.Load()).(block.EnchantingTable); enchanting {
				return s.ui, true
			}
		case protocol.ContainerCrafterLevelEntity:
			if _, crafter := tx.Block(*s.openedPos.Load()).(block.Crafter); crafter {
				return s.openedWindow.Load(), true
			}
		case protocol.ContainerFurnaceIngredient, protocol.ContainerFurnaceFuel, protocol.ContainerFurnaceResult,
			protocol.ContainerBlastFurnaceIngredient, protocol.ContainerSmokerIngredient:
			if _, ok := tx.Block(*s.openedPos.Load()).(smelter); ok {
				return s.openedWindow.Load(), true
			}
		}
	}
	return nil, false
}

// Disconnect disconnects the client and ultimately closes the session. If the message passed is non-empty,
// it will be shown to the client.
func (s *Session) Disconnect(message string) {
	if s != Nop {
		s.connWriteMu.Lock()
		_ = s.conn.WritePacket(&v2193.DisconnectPacket{
			HideDisconnectionScreen: message == "",
			Message:                 message,
		})
		_ = s.conn.Flush()
		s.connWriteMu.Unlock()
	}
}

// SendSpeed sends the speed of the player in an UpdateAttributes packet, so that it is updated client-side.
func (s *Session) SendSpeed(speed float64) {
	s.writePacket(&v2193.UpdateAttributesPacket{
		EntityRuntimeID: selfEntityRuntimeID,
		Attributes: []v2193.Attribute{{
			Name:       "minecraft:movement",
			Value:      float32(speed),
			Max:        math.MaxFloat32,
			DefaultMax: math.MaxFloat32,
			Default:    0.1,
		}},
	})
}

// SendFood ...
func (s *Session) SendFood(food int, saturation, exhaustion float64) {
	s.writePacket(&v2193.UpdateAttributesPacket{
		EntityRuntimeID: selfEntityRuntimeID,
		Attributes: []v2193.Attribute{
			{
				Name:       "minecraft:player.hunger",
				Value:      float32(food),
				Max:        20,
				DefaultMax: 20,
				Default:    20,
			},
			{
				Name:       "minecraft:player.saturation",
				Value:      float32(saturation),
				Max:        20,
				DefaultMax: 20,
				Default:    20,
			},
			{
				Name:       "minecraft:player.exhaustion",
				Value:      float32(exhaustion),
				Max:        5,
				DefaultMax: 5,
			},
		},
	})
}

// SendDialogue sends an NPC dialogue to the client of the connection. The Submit method of the dialogue is
// called when the client interacts with a button in the dialogue.
func (s *Session) SendDialogue(d dialogue.Dialogue, e world.Entity) {
	// Nop sessions are returned for offline players and must remain safe to call.
	if s == Nop || s.handlers == nil {
		return
	}
	if e == nil {
		if s.conf.Log != nil {
			s.conf.Log.Error("SendDialogue: nil entity")
		}
		return
	}
	b, err := json.Marshal(d)
	if err != nil {
		if s.conf.Log != nil {
			s.conf.Log.Error("SendDialogue: failed to marshal dialogue", "err", err)
		}
		return
	}

	h, ok := s.handlers[packet.IDNPCRequest].(*NPCRequestHandler)
	if !ok || h == nil {
		if s.conf.Log != nil {
			s.conf.Log.Error("SendDialogue: missing NPC request handler")
		}
		return
	}
	entityRuntimeID := s.handleRuntimeID(e.H())
	if entityRuntimeID == 0 {
		return
	}
	dialogueCopy := d
	h.mu.Lock()
	h.dialogue = &dialogueCopy
	h.entityRuntimeID = entityRuntimeID
	h.entityUniqueID = uint64(s.entityUniqueID(e))
	h.mu.Unlock()

	metadata := s.parseEntityMetadata(e)
	metadata[protocol.EntityDataKeyHasNPC] = uint8(1)

	disp := d.Display()
	disp.EntityOffset = disp.EntityOffset.Add(entityOffset(e))
	display, err := json.Marshal(map[string]any{"portrait_offsets": disp})
	if err != nil {
		if s.conf.Log != nil {
			s.conf.Log.Error("SendDialogue: failed to marshal dialogue display", "err", err)
		}
	} else {
		metadata[protocol.EntityDataKeyNPCData] = string(display)
	}

	s.writePacket(&v2193.SetActorDataPacket{
		EntityRuntimeID: entityRuntimeID,
		EntityMetadata:  toMetadata(metadata),
	})
	s.writePacket(&v2193.NPCDialoguePacket{
		EntityUniqueID: uint64(s.entityUniqueID(e)),
		ActionType:     packet.NPCDialogueActionOpen,
		Dialogue:       d.Body(),
		SceneName:      "default",
		NPCName:        d.Title(),
		ActionJSON:     string(b),
	})
}

func (s *Session) CloseDialogue() {
	if s == Nop || s.handlers == nil {
		return
	}
	h, ok := s.handlers[packet.IDNPCRequest].(*NPCRequestHandler)
	if !ok || h == nil {
		if s.conf.Log != nil {
			s.conf.Log.Error("CloseDialogue: missing NPC request handler")
		}
		return
	}
	h.mu.Lock()
	entityRuntimeID := h.entityRuntimeID
	entityUniqueID := h.entityUniqueID
	h.entityRuntimeID = 0
	h.entityUniqueID = 0
	h.dialogue = nil
	h.mu.Unlock()
	if entityRuntimeID == 0 {
		return
	}

	s.writePacket(&v2193.NPCDialoguePacket{
		EntityUniqueID: entityUniqueID,
		ActionType:     packet.NPCDialogueActionClose,
	})
}

// SendForm sends a form to the client of the connection. The Submit method of the form is called when the
// client submits the form.
func (s *Session) SendForm(f form.Form) {
	if s == Nop || s.handlers == nil {
		return
	}
	b, err := json.Marshal(f)
	if err != nil {
		if s.conf.Log != nil {
			s.conf.Log.Error("SendForm: failed to marshal form", "err", err)
		}
		return
	}

	h, ok := s.handlers[packet.IDModalFormResponse].(*ModalFormResponseHandler)
	if !ok || h == nil {
		if s.conf.Log != nil {
			s.conf.Log.Error("SendForm: missing modal form handler")
		}
		return
	}
	id := h.currentID.Add(1)

	h.mu.Lock()
	if len(h.forms) > 10 {
		s.conf.Log.Debug("SendForm: more than 10 active forms: dropping an existing one")
		for k := range h.forms {
			delete(h.forms, k)
			break
		}
	}
	h.forms[id] = f
	h.mu.Unlock()

	s.writePacket(&v2193.ModalFormRequestPacket{
		FormID:   id,
		FormData: b,
	})
}

// CloseForm closes any forms that the player currently has open. If the player has no forms open, nothing
// happens.
func (s *Session) CloseForm() {
	s.writePacket(&v2193.ClientBoundCloseFormPacket{})
}

// Transfer transfers the player to a server with the IP and port passed.
func (s *Session) Transfer(ip net.IP, port int) {
	if port < 0 || port > 65535 {
		if s.conf.Log != nil {
			s.conf.Log.Error("Transfer: invalid port", "port", port)
		}
		return
	}
	s.writePacket(&v2193.TransferPacket{
		Address: ip.String(),
		Port:    uint16(port),
	})
}

// SendGameMode sends the game mode of the Controllable entity of the session to the client. It makes sure the right
// flags are set to create the full game mode.
func (s *Session) SendGameMode(c Controllable) {
	if s == Nop {
		return
	}
	s.writePacket(&v2193.SetPlayerGameTypePacket{GameType: GameTypeFromMode(c.GameMode())})
	s.resendAbilities(c)
}

// resendAbilities queues an ability update after a game mode update. Bedrock ignores abilities sent immediately
// after the game type, so the update runs later on the controllable's world owner.
func (s *Session) resendAbilities(c Controllable) {
	delay := s.abilityResendDelay
	if delay <= 0 {
		delay = defaultAbilityResendDelay
	}

	s.abilityResendMu.Lock()
	if s.closing.Load() {
		s.abilityResendMu.Unlock()
		return
	}
	s.abilityResendGen++
	generation := s.abilityResendGen
	previous := s.abilityResend
	s.abilityResend = c.H().DoAfter(delay, func(_ *world.Tx, _ world.Entity) {
		s.abilityResendMu.Lock()
		if s.closing.Load() || s.abilityResendGen != generation {
			s.abilityResendMu.Unlock()
			return
		}
		s.abilityResend = nil
		s.abilityResendMu.Unlock()
		s.SendAbilities(c)
	})
	s.abilityResendMu.Unlock()
	if previous != nil {
		previous.Cancel()
	}
}

// SendAbilities sends the abilities of the Controllable entity of the session to the client.
func (s *Session) SendAbilities(c Controllable) {
	if s == Nop {
		return
	}
	if !c.GameMode().HasCollision() {
		// Spectator mode needs a teleport before the ability update on the
		// owning client, then flight may schedule a further ability resend.
		defer c.StartFlying()
		s.ViewEntityTeleport(c, c.Position())
	}
	data := s.abilityData(c, c.H().UniqueID())
	s.abilities.Store(&data)
	self := data
	self.EntityUniqueID = selfEntityRuntimeID
	s.writePacket(&v2193.UpdateAbilitiesPacket{AbilityData: self})
}

// SendPermissions refreshes abilities for this player and the other clients
// that have the player in their player list, then refreshes command access.
func (s *Session) SendPermissions(c Controllable) {
	if s == Nop {
		return
	}
	s.SendAbilities(c)
	s.commandsDirty.Store(true)
	data := s.abilityData(c, c.H().UniqueID())
	s.abilities.Store(&data)
	sessions.sendAbilities(s, data)
}

// abilityData builds abilities without changing player state, so the same
// permissions can be included in AddPlayer and updates for other clients.
func (s *Session) abilityData(c Controllable, uniqueID int64) v2193.AbilityData {
	mode, abilities := c.GameMode(), uint32(0)
	if mode.AllowsFlying() {
		abilities |= protocol.AbilityMayFly
		if c.Flying() {
			abilities |= protocol.AbilityFlying
		}
	}
	if !mode.HasCollision() {
		abilities |= protocol.AbilityNoClip
	}
	if !mode.AllowsTakingDamage() {
		abilities |= protocol.AbilityInvulnerable
	}
	if mode.CreativeInventory() {
		abilities |= protocol.AbilityInstantBuild
	}
	if mode.AllowsEditing() {
		abilities |= protocol.AbilityBuild | protocol.AbilityMine
	}
	if mode.AllowsInteraction() {
		abilities |= protocol.AbilityDoorsAndSwitches | protocol.AbilityOpenContainers | protocol.AbilityAttackPlayers | protocol.AbilityAttackMobs
	}
	playerPermissions, commandPermissions := byte(packet.PermissionLevelMember), byte(protocol.CommandPermissionLevelAny)
	if s.conf.IsOperator != nil && s.conf.IsOperator(c.Name()) {
		playerPermissions, commandPermissions = packet.PermissionLevelOperator, protocol.CommandPermissionLevelGameDirectors
		abilities |= protocol.AbilityOperatorCommands | protocol.AbilityTeleport
	}
	return v2193.AbilityData{
		EntityUniqueID:     uniqueID,
		PlayerPermissions:  playerPermissions,
		CommandPermissions: commandPermissions,
		Layers: []v2193.AbilityLayer{
			{
				Type:             protocol.AbilityLayerTypeBase,
				Abilities:        protocol.AbilityCount - 1,
				Values:           abilities,
				FlySpeed:         float32(c.FlightSpeed()),
				VerticalFlySpeed: float32(c.VerticalFlightSpeed()),
				WalkSpeed:        protocol.AbilityBaseWalkSpeed,
			},
		},
	}
}

// SendHealth sends the health and max health to the player.
func (s *Session) SendHealth(health, max, absorption float64) {
	s.writePacket(&v2193.UpdateAttributesPacket{
		EntityRuntimeID: selfEntityRuntimeID,
		Attributes: []v2193.Attribute{{
			Name:       "minecraft:health",
			Value:      float32(math.Ceil(health)),
			Max:        float32(math.Ceil(max)),
			DefaultMax: 20,
			Default:    20,
		}, {
			Name:       "minecraft:absorption",
			Value:      float32(math.Ceil(absorption)),
			Max:        float32(math.MaxFloat32),
			DefaultMax: float32(math.MaxFloat32),
		}},
	})
}

// SendEffect sends an effect newly added to the player.
func (s *Session) SendEffect(e effect.Effect) {
	s.sendEffect(e, packet.MobEffectAdd)
}

// SendEffectModification sends an effect of the player that replaced one of
// the same type, which BDS sends as a modification rather than a new effect.
func (s *Session) SendEffectModification(e effect.Effect) {
	s.sendEffect(e, packet.MobEffectModify)
}

func (s *Session) sendEffect(e effect.Effect, op byte) {
	id, _ := effect.ID(e.Type())
	dur := e.Duration() / (time.Second / 20)
	if e.Infinite() {
		dur = -1
	}
	s.writePacket(&v2193.MobEffectPacket{
		EntityRuntimeID: selfEntityRuntimeID,
		Operation:       op,
		EffectType:      int32(id),
		Amplifier:       int32(e.Level() - 1),
		Particles:       !e.ParticlesHidden(),
		Duration:        int32(dur),
		Ambient:         e.Ambient(),
	})
}

// SendEffectRemoval sends the removal of an effect passed.
func (s *Session) SendEffectRemoval(e effect.Type) {
	id, ok := effect.ID(e)
	if !ok {
		panic(fmt.Sprintf("unregistered effect type %T", e))
	}
	s.writePacket(&v2193.MobEffectPacket{
		EntityRuntimeID: selfEntityRuntimeID,
		Operation:       packet.MobEffectRemove,
		EffectType:      int32(id),
	})
}

// SendGameRules sends all the provided game rules to the player. Once sent, they will be immediately updated
// on the client if they are valid.
func (s *Session) sendGameRules(gameRules []v2193.GameRule) {
	s.writePacket(&v2193.GameRulesChangedPacket{GameRules: gameRules})
}

// EnableCoordinates will either enable or disable coordinates for the player depending on the value given.
func (s *Session) EnableCoordinates(enable bool) {
	//noinspection SpellCheckingInspection
	s.sendGameRules([]v2193.GameRule{{Name: "showcoordinates", Value: enable}})
}

// EnableInstantRespawn will either enable or disable instant respawn for the player depending on the value given.
func (s *Session) EnableInstantRespawn(enable bool) {
	//noinspection SpellCheckingInspection
	s.sendGameRules([]v2193.GameRule{{Name: "doimmediaterespawn", Value: enable}})
}

// HandleInventories starts handling the inventories of the Controllable entity of the session. It sends packets when
// slots in the inventory are changed.
func (s *Session) HandleInventories(tx *world.Tx, c Controllable, inv, offHand, enderChest, ui *inventory.Inventory, armour *inventory.Armour, heldSlot *uint32) {
	// Store the held slot pointer before attaching callbacks to avoid races with concurrent inventory updates.
	s.heldSlot.Store(heldSlot)
	s.inv = inv
	// Use tx-less broadcast functions that schedule work on the entity's world transaction.
	s.inv.SlotFunc(s.broadcastInvFunc(c))
	s.offHand = offHand
	s.offHand.SlotFunc(s.broadcastOffHandFunc(c))
	s.enderChest = enderChest
	s.enderChest.SlotFunc(s.broadcastEnderChestFunc(c))
	s.armour = armour
	s.armour.Inventory().SlotFunc(s.broadcastArmourFunc(c))
	s.ui = ui
	s.ui.SlotFunc(s.uiInventoryFunc(c))
	if unlocker, ok := c.(interface{ UnlockedRecipeIDs() []string }); ok {
		s.SendInitiallyUnlockedRecipes(unlocker.UnlockedRecipeIDs())
	}
}

func (s *Session) broadcastInvFunc(c Controllable) inventory.SlotFunc {
	return func(slot int, _, after item.Stack) {
		heldSlot := s.heldSlot.Load()
		if heldSlot != nil && slot == int(atomic.LoadUint32(heldSlot)) {
			// Acquire viewers within a fresh transaction to avoid using a stale tx. Schedule this asynchronously
			// to prevent re-entrancy deadlocks when the inventory update originates from an existing world
			// transaction.
			c.H().Do(func(tx *world.Tx, e world.Entity) {
				viewers := tx.Viewers(e.Position())
				// Equipment updates are extremely frequent; using the pooled slice keeps hotbar spam from
				// producing unnecessary garbage.
				for _, viewer := range viewers {
					viewer.ViewEntityItems(e)
				}
				tx.ReleaseViewers(viewers)
			})
		}
		if !s.inTransaction.Load() {
			s.sendItem(after, slot, protocol.WindowIDInventory)
		}
	}
}

func (s *Session) broadcastEnderChestFunc(c Controllable) inventory.SlotFunc {
	return func(slot int, _, after item.Stack) {
		if s.inTransaction.Load() || !s.containerOpened.Load() {
			return
		}
		openedPos := s.openedPos.Load()
		if openedPos == nil {
			return
		}
		pos := *openedPos
		// Check container state inside the transaction to avoid sending updates after the chest is closed or moved.
		c.H().Do(func(tx *world.Tx, _ world.Entity) {
			if !s.containerOpened.Load() {
				return
			}
			current := s.openedPos.Load()
			if current == nil || *current != pos {
				return
			}
			if _, ok := tx.Block(pos).(block.EnderChest); ok {
				s.ViewSlotChange(slot, after)
			}
		})
	}
}

func (s *Session) broadcastOffHandFunc(c Controllable) inventory.SlotFunc {
	return func(slot int, _, after item.Stack) {
		// Broadcast off-hand changes within a valid transaction. Run asynchronously to avoid blocking existing
		// world transactions that triggered the inventory update.
		c.H().Do(func(tx *world.Tx, e world.Entity) {
			viewers := tx.Viewers(e.Position())
			// Off-hand sync leverages the same pooled viewer slice so rapidly switching totems or shields remains
			// allocation free.
			for _, viewer := range viewers {
				viewer.ViewEntityItems(e)
			}
			tx.ReleaseViewers(viewers)
		})
		if !s.inTransaction.Load() {
			i, _ := s.offHand.Item(0)
			s.writePacket(&v2193.InventoryContentPacket{
				WindowID: protocol.WindowIDOffHand,
				Content:  []v2193.ItemInstance{instanceFromItem(s.br, i)},
			})
		}
	}
}

func (s *Session) broadcastArmourFunc(c Controllable) inventory.SlotFunc {
	return func(slot int, before, after item.Stack) {
		inTransaction := s.inTransaction.Load()
		if !inTransaction {
			s.sendItem(after, slot, protocol.WindowIDArmour)
		}
		if before.Comparable(after) && before.Empty() == after.Empty() {
			// Only send armour if the item type actually changed.
			return
		}
		// Broadcast armour changes within a valid transaction. Schedule asynchronously to prevent blocking the
		// caller when the update occurs inside an existing world transaction.
		c.H().Do(func(tx *world.Tx, e world.Entity) {
			viewers := tx.Viewers(e.Position())
			// Armour broadcasts also borrow the pooled buffer. Players often spam armour swaps in PvP; the pool keeps
			// the resulting updates cheap.
			for _, viewer := range viewers {
				viewer.ViewEntityArmour(e)
			}
			if !after.Empty() && inTransaction {
				tx.PlaySound(entity.EyePosition(e), sound.EquipItem{Item: after.Item()})
			}
			tx.ReleaseViewers(viewers)
		})
	}
}

// uiInventoryFunc handles an update to the UI inventory, used for updating enchantment options and possibly more
// in the future.
func (s *Session) uiInventoryFunc(c Controllable) inventory.SlotFunc {
	return func(slot int, _, after item.Stack) {
		if slot == enchantingInputSlot && s.containerOpened.Load() {
			pos := *s.openedPos.Load()
			c.H().Do(func(tx *world.Tx, _ world.Entity) {
				if _, enchanting := tx.Block(pos).(block.EnchantingTable); enchanting {
					s.sendEnchantmentOptions(tx, c, pos, after)
				}
			})
		}
		s.sendInv(s.ui, protocol.WindowIDUI)
	}
}

// SendHeldSlot sends the currently held hotbar slot.
func (s *Session) SendHeldSlot(slot int, c Controllable, force bool) {
	if s.changingSlot.Load() && !force {
		return
	}
	mainHand, _ := c.HeldItems()
	s.writePacket(&v2193.MobEquipmentPacket{
		EntityRuntimeID: selfEntityRuntimeID,
		NewItem:         instanceFromItem(s.br, mainHand),
		InventorySlot:   byte(slot),
		HotBarSlot:      byte(slot),
	})
}

// VerifyAndSetHeldSlot verifies if the slot passed is a valid hotbar slot and
// if the expected item.Stack is in it. Afterwards, it changes the held slot
// of the player.
func (s *Session) VerifyAndSetHeldSlot(slot int, expected item.Stack, c Controllable) error {
	if err := s.VerifySlot(slot, expected); err != nil {
		return err
	}
	s.changingSlot.Store(true)
	defer s.changingSlot.Store(false)
	return c.SetHeldSlot(slot)
}

// VerifySlot verifies if the slot passed is a valid hotbar slot and if the
// expected item.Stack is in it.
func (s *Session) VerifySlot(slot int, expected item.Stack) error {
	// The slot that the player might have selected must be within the hotbar:
	// The held item cannot be in a different place in the inventory.
	if slot < 0 || slot > 8 {
		return fmt.Errorf("slot exceeds hotbar range 0-8: slot is %v", slot)
	}
	clientSideItem := expected
	actual, _ := s.inv.Item(slot)
	if clientCompass, clientOK := clientSideItem.Item().(item.Compass); clientOK {
		if serverCompass, serverOK := actual.Item().(item.Compass); serverOK && clientCompass.TrackingHandle != serverCompass.TrackingHandle {
			clientSideItem = clientSideItem.WithItem(serverCompass)
		}
	}

	// The item the client claims to have must be identical to the one we have
	// registered server-side.
	if !clientSideItem.Equal(actual) {
		s.sendItem(actual, slot, protocol.WindowIDInventory)
		// Only ever debug these as they are frequent and expected to happen
		// whenever client and server get out of sync.
		s.conf.Log.Debug("verify slot: client-side item was not equal to server-side item", "client-held", clientSideItem.String(), "server-held", actual.String())
	}
	return nil
}

// SendExperience sends the experience level and progress from the given experience manager to the player.
func (s *Session) SendExperience(level int, progress float64) {
	s.writePacket(&v2193.UpdateAttributesPacket{
		EntityRuntimeID: selfEntityRuntimeID,
		Attributes: []v2193.Attribute{
			{
				Name:       "minecraft:player.level",
				Value:      float32(level),
				Max:        float32(math.MaxInt32),
				DefaultMax: float32(math.MaxInt32),
			},
			{
				Name:       "minecraft:player.experience",
				Value:      float32(progress),
				Max:        1,
				DefaultMax: 1,
			},
		},
	})
}

// SendChargeItemComplete sends a packet to indicate that the item charging process has been completed.
func (s *Session) SendChargeItemComplete() {
	s.writePacket(&v2193.ActorEventPacket{
		EntityRuntimeID: selfEntityRuntimeID,
		EventType:       packet.ActorEventFinishedChargingItem,
	})
}

// ShowHudElement shows a HUD element to the player if it is not already shown. If the element is waiting to
// be hidden, it will be removed from the updates and remain visible to the player.
func (s *Session) ShowHudElement(e hud.Element) {
	s.hudMu.Lock()
	defer s.hudMu.Unlock()

	if _, ok := s.hiddenHud[e]; ok {
		s.hudUpdates[e] = true
	} else if _, ok = s.hudUpdates[e]; ok {
		delete(s.hudUpdates, e)
	}
}

// HideHudElement hides a HUD element from the player if it is not already hidden. If the element is waiting
// to be shown, it will be removed from the updates and remain hidden from the player.
func (s *Session) HideHudElement(e hud.Element) {
	s.hudMu.Lock()
	defer s.hudMu.Unlock()

	if _, ok := s.hiddenHud[e]; !ok {
		s.hudUpdates[e] = false
	} else if _, ok = s.hudUpdates[e]; ok {
		delete(s.hudUpdates, e)
	}
}

// HudElementHidden checks if a HUD element is currently hidden from the player.
func (s *Session) HudElementHidden(e hud.Element) bool {
	s.hudMu.RLock()
	defer s.hudMu.RUnlock()

	if _, ok := s.hiddenHud[e]; ok {
		return true
	}
	vis, ok := s.hudUpdates[e]
	return ok && !vis
}

// SendHudUpdates sends any pending HUD updates to the player. The updates are batched to reduce the number
// of packets being sent. Up to 2 packets will be sent, one for showing elements and one for hiding elements.
func (s *Session) SendHudUpdates() {
	s.hudMu.Lock()
	if len(s.hudUpdates) == 0 {
		s.hudMu.Unlock()
		return
	}
	var show, hide []int32
	for e, vis := range s.hudUpdates {
		if vis {
			show = append(show, int32(e.Uint8()))
			delete(s.hiddenHud, e)
		} else {
			hide = append(hide, int32(e.Uint8()))
			s.hiddenHud[e] = struct{}{}
		}
	}
	s.hudUpdates = make(map[hud.Element]bool)
	s.hudMu.Unlock()

	if len(show) > 0 {
		s.writePacket(&v2193.SetHudPacket{Elements: show, Visibility: packet.HudVisibilityReset})
	}
	if len(hide) > 0 {
		s.writePacket(&v2193.SetHudPacket{Elements: hide, Visibility: packet.HudVisibilityHide})
	}
}

// AddDebugShape adds a debug shape to be rendered to the player. If the shape already exists, it will be
// updated with the new information.
func (s *Session) AddDebugShape(shape debug.Shape) {
	if s == Nop {
		return
	}
	s.queueDebugShapeUpdate(debugShapeUpdate{id: shape.ShapeID(), shape: shape})
}

// RemoveDebugShape removes a debug shape from the player by its unique identifier.
func (s *Session) RemoveDebugShape(shape debug.Shape) {
	if s == Nop {
		return
	}
	s.queueDebugShapeUpdate(debugShapeUpdate{id: shape.ShapeID()})
}

// VisibleDebugShapes returns a slice of all debug shapes that are currently being shown to the player.
func (s *Session) VisibleDebugShapes() []debug.Shape {
	s.debugShapesMu.RLock()
	defer s.debugShapesMu.RUnlock()

	return slices.Collect(maps.Values(s.debugShapes))
}

// RemoveAllDebugShapes removes all rendered debug shapes from the player, as well as any shapes that have
// not yet been rendered.
func (s *Session) RemoveAllDebugShapes() {
	if s == Nop {
		return
	}
	s.debugShapesMu.Lock()
	s.debugShapeUpdates = s.debugShapeUpdates[:0]
	for id := range s.debugShapes {
		s.debugShapeUpdates = append(s.debugShapeUpdates, debugShapeUpdate{id: id})
	}
	s.debugShapesMu.Unlock()
}

// SendDebugShapes sends any pending additions/removals of debug shapes to the player. Shapes should be sent
// every tick to allow for batching and time-efficient updates.
func (s *Session) SendDebugShapes(dim world.Dimension) {
	if s == Nop {
		return
	}
	s.debugShapesMu.Lock()
	updates := s.debugShapeUpdates
	if len(updates) == 0 {
		s.debugShapesMu.Unlock()
		return
	}

	shapes := make([]v2193.PrimitiveShape, 0, len(updates))
	for _, update := range updates {
		if update.shape == nil {
			delete(s.debugShapes, update.id)
			shapes = append(shapes, v2193.PrimitiveShape{
				NetworkID:      uint64(update.id),
				DimensionID:    proto.Some(s.dimensionID(dim)),
				ExtraShapeData: &v2193.LastShape{},
			})
			continue
		}
		s.debugShapes[update.id] = update.shape
		shapes = append(shapes, debugShapeToProtocol(update.shape, dim, s.shapeAttachedEntityRuntimeID(update.shape), update.id))
	}
	s.debugShapeUpdates = s.debugShapeUpdates[:0]
	s.debugShapesMu.Unlock()

	s.writePacket(&v2193.PrimitiveShapesPacket{Shapes: shapes})
}

// LockInput applies an input lock to the player, disabling the specified input. If the lock is already
// applied, this is a no-op.
func (s *Session) LockInput(l input.Lock) {
	s.inputLocksMu.Lock()
	defer s.inputLocksMu.Unlock()
	s.inputLocks |= l.Uint32()
}

// UnlockInput removes an input lock from the player, re-enabling the specified input. If the lock is not
// currently applied, this is a no-op.
func (s *Session) UnlockInput(l input.Lock) {
	s.inputLocksMu.Lock()
	defer s.inputLocksMu.Unlock()
	s.inputLocks &^= l.Uint32()
}

// ClearInputLocks removes all input locks from the player, re-enabling all inputs.
func (s *Session) ClearInputLocks() {
	s.inputLocksMu.Lock()
	defer s.inputLocksMu.Unlock()
	s.inputLocks = 0
}

// InputLocked checks if a specific input lock is currently applied to the player.
func (s *Session) InputLocked(l input.Lock) bool {
	s.inputLocksMu.RLock()
	defer s.inputLocksMu.RUnlock()
	return s.inputLocks&l.Uint32() != 0
}

// SendInputLocks sends the current input lock state to the client.
func (s *Session) SendInputLocks() {
	s.inputLocksMu.RLock()
	defer s.inputLocksMu.RUnlock()
	s.writePacket(&v2193.UpdateClientInputLocksPacket{
		Locks: s.inputLocks,
	})
}

// queueDebugShapeUpdate queues a debug shape mutation to be applied the next time debug shapes are sent.
func (s *Session) queueDebugShapeUpdate(update debugShapeUpdate) {
	s.debugShapesMu.Lock()
	s.debugShapeUpdates = append(s.debugShapeUpdates, update)
	s.debugShapesMu.Unlock()
}

// shapeAttachedEntityRuntimeID returns the runtime ID of the entity attached to a debug shape.
func (s *Session) shapeAttachedEntityRuntimeID(shape debug.Shape) uint64 {
	var handle *world.EntityHandle
	switch shape := shape.(type) {
	case *debug.Arrow:
		handle = shape.Entity
	case *debug.Box:
		handle = shape.Entity
	case *debug.Circle:
		handle = shape.Entity
	case *debug.Line:
		handle = shape.Entity
	case *debug.Sphere:
		handle = shape.Entity
	case *debug.Text:
		handle = shape.Entity
	case *debug.Cylinder:
		handle = shape.Entity
	case *debug.Pyramid:
		handle = shape.Entity
	case *debug.Ellipsoid:
		handle = shape.Entity
	case *debug.Cone:
		handle = shape.Entity
	}
	if handle == nil {
		return 0
	}
	return s.handleRuntimeID(handle)
}

// debugShapeToProtocol converts a debug shape to its protocol representation. It also provides defaults
// for some fields such as colour, scale and other per-shape properties.
func debugShapeToProtocol(shape debug.Shape, dim world.Dimension, attachedEntityID uint64, networkID int) v2193.PrimitiveShape {
	dimID, _ := world.DimensionID(dim)
	ps := v2193.PrimitiveShape{
		NetworkID:   uint64(networkID),
		DimensionID: proto.Some(int32(dimID)),
	}
	if attachedEntityID > 0 {
		ps.AttachedToEntityID = proto.Some(int64(attachedEntityID))
	}
	white := color.RGBA{R: 255, G: 255, B: 255, A: 255}
	switch shape := shape.(type) {
	case *debug.Arrow:
		ps.Type = proto.Some(protocol.PrimitiveShapeArrow)
		ps.Colour = proto.Some(toColour(valueOrDefault(shape.Colour, white)))
		ps.Location = proto.Some([3]float32(vec64To32(shape.Position)))
		ps.ExtraShapeData = &v2193.ArrowShape{
			ArrowEndLocation: proto.Some([3]float32(vec64To32(shape.EndPosition))),
			ArrowHeadLength:  proto.Some(valueOrDefault(float32(shape.HeadLength), 1)),
			ArrowHeadRadius:  proto.Some(valueOrDefault(float32(shape.HeadRadius), 0.5)),
			Segments:         proto.Some(valueOrDefault(uint8(shape.HeadSegments), 4)),
		}
	case *debug.Box:
		ps.Type = proto.Some(protocol.PrimitiveShapeBox)
		ps.Colour = proto.Some(toColour(valueOrDefault(shape.Colour, white)))
		ps.Location = proto.Some([3]float32(vec64To32(shape.Position)))
		ps.Scale = proto.Some(valueOrDefault(float32(shape.Scale), 1))
		ps.ExtraShapeData = &v2193.BoxShape{BoxBound: valueOrDefault(vec64To32(shape.Bounds), mgl32.Vec3{1, 1, 1})}
	case *debug.Circle:
		ps.Type = proto.Some(protocol.PrimitiveShapeCircle)
		ps.Colour = proto.Some(toColour(valueOrDefault(shape.Colour, white)))
		ps.Location = proto.Some([3]float32(vec64To32(shape.Position)))
		ps.Scale = proto.Some(valueOrDefault(float32(shape.Scale), 1))
		ps.ExtraShapeData = &v2193.SphereShape{Segments: valueOrDefault(uint8(shape.Segments), 20)}
	case *debug.Line:
		ps.Type = proto.Some(protocol.PrimitiveShapeLine)
		ps.Colour = proto.Some(toColour(valueOrDefault(shape.Colour, white)))
		ps.Location = proto.Some([3]float32(vec64To32(shape.Position)))
		ps.ExtraShapeData = &v2193.LineShape{LineEndLocation: vec64To32(shape.EndPosition)}
	case *debug.Sphere:
		ps.Type = proto.Some(protocol.PrimitiveShapeSphere)
		ps.Colour = proto.Some(toColour(valueOrDefault(shape.Colour, white)))
		ps.Location = proto.Some([3]float32(vec64To32(shape.Position)))
		ps.Scale = proto.Some(valueOrDefault(float32(shape.Scale), 1))
		ps.ExtraShapeData = &v2193.SphereShape{Segments: valueOrDefault(uint8(shape.Segments), 20)}
	case *debug.Text:
		ps.Type = proto.Some(protocol.PrimitiveShapeText)
		ps.Colour = proto.Some(toColour(valueOrDefault(shape.Colour, white)))
		ps.Location = proto.Some([3]float32(vec64To32(shape.Position)))
		ps.Scale = proto.Some(valueOrDefault(float32(shape.Scale), 1))
		if shape.LockRotation {
			ps.Rotation = proto.Some([3]float32(vec64To32(shape.Rotation)))
		}
		textData := &v2193.TextShape{
			Text:             shape.Text,
			UseRotation:      shape.LockRotation,
			DepthTest:        !shape.DisableDepthTest,
			ShowBackface:     !shape.HideBackface,
			ShowBackfaceText: !shape.HideBackfaceText,
		}
		switch {
		case shape.HideBackground:
			textData.BackgroundColour = proto.Some(proto.Color{})
		case shape.BackgroundColour != (color.RGBA{}):
			textData.BackgroundColour = proto.Some(toColour(shape.BackgroundColour))
		}
		ps.ExtraShapeData = textData
	case *debug.Cylinder:
		ps.Type = proto.Some(protocol.PrimitiveShapeCylinder)
		ps.Colour = proto.Some(toColour(valueOrDefault(shape.Colour, white)))
		ps.Location = proto.Some([3]float32(vec64To32(shape.Position)))
		ps.Scale = proto.Some(valueOrDefault(float32(shape.Scale), 1))
		base := valueOrDefault(shape.BaseRadius, mgl64.Vec2{1, 1})
		top := valueOrDefault(shape.TopRadius, base)
		ps.ExtraShapeData = &v2193.CylinderShape{
			RadiusX:     mgl32.Vec2{float32(base[0]), float32(top[0])},
			RadiusZ:     mgl32.Vec2{float32(base[1]), float32(top[1])},
			Height:      valueOrDefault(float32(shape.Height), 1),
			NumSegments: valueOrDefault(uint8(shape.Segments), 20),
		}
	case *debug.Pyramid:
		ps.Type = proto.Some(protocol.PrimitiveShapePyramid)
		ps.Colour = proto.Some(toColour(valueOrDefault(shape.Colour, white)))
		ps.Location = proto.Some([3]float32(vec64To32(shape.Position)))
		ps.Scale = proto.Some(valueOrDefault(float32(shape.Scale), 1))
		pyramid := &v2193.PyramidShape{
			Width:  valueOrDefault(float32(shape.Width), 1),
			Height: valueOrDefault(float32(shape.Height), 1),
		}
		if shape.Depth != 0 {
			pyramid.Depth = proto.Some(float32(shape.Depth))
		}
		ps.ExtraShapeData = pyramid
	case *debug.Ellipsoid:
		ps.Type = proto.Some(protocol.PrimitiveShapeEllipsoid)
		ps.Colour = proto.Some(toColour(valueOrDefault(shape.Colour, white)))
		ps.Location = proto.Some([3]float32(vec64To32(shape.Position)))
		ps.Scale = proto.Some(valueOrDefault(float32(shape.Scale), 1))
		ps.ExtraShapeData = &v2193.EllipsoidShape{
			Radii:           valueOrDefault(vec64To32(shape.Radii), mgl32.Vec3{1, 1, 1}),
			SegmentsPerAxis: valueOrDefault(uint8(shape.SegmentsPerAxis), 20),
		}
	case *debug.Cone:
		ps.Type = proto.Some(protocol.PrimitiveShapeCone)
		ps.Colour = proto.Some(toColour(valueOrDefault(shape.Colour, white)))
		ps.Location = proto.Some([3]float32(vec64To32(shape.Position)))
		ps.Scale = proto.Some(valueOrDefault(float32(shape.Scale), 1))
		ps.ExtraShapeData = &v2193.ConeShape{
			Radii:       valueOrDefault(vec2To32(shape.Radii), mgl32.Vec2{1, 1}),
			Height:      valueOrDefault(float32(shape.Height), 1),
			NumSegments: valueOrDefault(uint8(shape.Segments), 20),
		}
	default:
		panic(fmt.Sprintf("unknown debug shape type %T", shape))
	}
	return ps
}

// valueOrDefault returns the value passed if it is not the zero value of the type T, otherwise it returns
// the default value provided.
func valueOrDefault[T comparable](v, def T) T {
	var zero T
	if v == zero {
		return def
	}
	return v
}

// stackFromItem converts an item.Stack to its network ItemStack representation.
func stackFromItem(br world.BlockRegistry, it item.Stack) v2193.ItemStack {
	if it.Empty() {
		return v2193.ItemStack{}
	}

	var blockRuntimeID uint32
	if b, ok := it.Item().(world.Block); ok {
		blockRuntimeID = networkBlockID(br, b)
	}

	rid, meta, _ := world.ItemRuntimeID(it.Item())

	return v2193.ItemStack{
		NetworkID:      rid,
		MetadataValue:  uint32(meta),
		Count:          uint16(it.Count()),
		BlockRuntimeID: int32(blockRuntimeID),
		NBTData:        item.WriteNBT(it, false),
	}
}

// stackToItem converts a network ItemStack representation back to an item.Stack.
func stackToItem(br world.BlockRegistry, it v2193.ItemStack) item.Stack {
	t, ok := world.ItemByRuntimeID(it.NetworkID, int16(it.MetadataValue))
	if !ok {
		t = block.Air{}
	}
	if it.BlockRuntimeID != 0 {
		// Clients name the block of a block item by its network block hash. An
		// unknown hash or a block that is no item leaves air.
		t = block.Air{}
		if rid, known := br.HashToRuntimeID(uint32(it.BlockRuntimeID)); known {
			if b, found := br.BlockByRuntimeID(rid); found {
				if blockItem, isItem := b.(world.Item); isItem {
					t = blockItem
				}
			}
		}
	}
	//noinspection SpellCheckingInspection
	if compass, ok := t.(item.Compass); ok {
		// Decode even without NBT so the lodestone-compass registry variant cannot leak its registration handle.
		t = compass.DecodeNBT(it.NBTData).(world.Item)
	} else if nbter, ok := t.(world.NBTer); ok && len(it.NBTData) != 0 {
		t = world.DecodeNBT(nbter, it.NBTData, br).(world.Item)
	}
	s := item.NewStack(t, int(it.Count))
	return item.ReadNBT(it.NBTData, &s, br)
}

// instanceFromItem converts an item.Stack to its network ItemInstance representation.
func instanceFromItem(br world.BlockRegistry, it item.Stack) v2193.ItemInstance {
	return v2193.ItemInstance{
		StackNetworkID: item_id(it),
		Stack:          stackFromItem(br, it),
	}
}

// stacksToRecipeStacks converts a list of item.Stacks to their protocol representation with damage stripped for recipes.
func stacksToRecipeStacks(br world.BlockRegistry, inputs []item.Stack) []v2193.ItemStack {
	items := make([]v2193.ItemStack, 0, len(inputs))
	for _, i := range inputs {
		items = append(items, deleteDamage(stackFromItem(br, i)))
	}
	return items
}

// stacksToIngredientItems converts a list of item.Stacks to recipe ingredient items used over the network.
func stacksToIngredientItems(_ world.BlockRegistry, inputs []recipe.Item) []v2193.ItemDescriptorCount {
	items := make([]v2193.ItemDescriptorCount, 0, len(inputs))
	for _, i := range inputs {
		var d v2193.ItemDescriptor = &v2193.InvalidItemDescriptor{}
		switch i := i.(type) {
		case item.Stack:
			if i.Empty() {
				items = append(items, v2193.ItemDescriptorCount{Descriptor: &v2193.InvalidItemDescriptor{}})
				continue
			}
			name, meta := i.Item().EncodeItem()
			if _, ok := i.Value("variants"); ok {
				meta = math.MaxInt16 // Used to indicate that the item has multiple selectable variants.
			}
			d = &v2193.DefaultItemDescriptor{
				Name:          name,
				MetadataValue: int32(meta),
			}
		case recipe.ItemTag:
			d = &v2193.ItemTagItemDescriptor{Tag: i.Tag()}
		}
		items = append(items, v2193.ItemDescriptorCount{
			Descriptor: d,
			Count:      int32(i.Count()),
		})
	}
	return items
}

// deleteDamage strips the damage from a protocol item.
func deleteDamage(st v2193.ItemStack) v2193.ItemStack {
	delete(st.NBTData, "Damage")
	return st
}

// protocolToSkin converts v2193.Skin to skin.Skin.
func protocolToSkin(sk v2193.Skin) (s skin.Skin, err error) {
	if sk.SkinID == "" {
		return skin.Skin{}, fmt.Errorf("SkinID must not be an empty string")
	}
	if !validSkinDimensions(int(sk.SkinImageWidth), int(sk.SkinImageHeight)) {
		return skin.Skin{}, fmt.Errorf("invalid skin dimensions: %dx%d", sk.SkinImageWidth, sk.SkinImageHeight)
	}
	skinDataLen, err := expectedRGBABytes(int(sk.SkinImageWidth), int(sk.SkinImageHeight))
	if err != nil {
		return skin.Skin{}, fmt.Errorf("invalid skin dimensions: %w", err)
	}
	if len(sk.SkinData) != skinDataLen {
		return skin.Skin{}, fmt.Errorf("invalid skin data length: got %d, want %d", len(sk.SkinData), skinDataLen)
	}

	s = skin.New(int(sk.SkinImageWidth), int(sk.SkinImageHeight))
	s.Persona = sk.PersonaSkin
	s.Pix = append([]byte(nil), sk.SkinData...)
	s.Model = sk.SkinGeometry
	s.PlayFabID = sk.PlayFabID
	s.FullID = sk.FullID

	if sk.CapeImageWidth != 0 || sk.CapeImageHeight != 0 || len(sk.CapeData) != 0 {
		if !validCapeDimensions(int(sk.CapeImageWidth), int(sk.CapeImageHeight)) {
			return skin.Skin{}, fmt.Errorf("invalid cape dimensions: %dx%d", sk.CapeImageWidth, sk.CapeImageHeight)
		}
		capeDataLen, err := expectedRGBABytes(int(sk.CapeImageWidth), int(sk.CapeImageHeight))
		if err != nil {
			return skin.Skin{}, fmt.Errorf("invalid cape dimensions: %w", err)
		}
		if len(sk.CapeData) != capeDataLen {
			return skin.Skin{}, fmt.Errorf("invalid cape data length: got %d, want %d", len(sk.CapeData), capeDataLen)
		}
		s.Cape = skin.NewCape(int(sk.CapeImageWidth), int(sk.CapeImageHeight))
		s.Cape.Pix = append([]byte(nil), sk.CapeData...)
	}

	m := make(map[string]any)
	if err = json.Unmarshal(sk.SkinGeometry, &m); err != nil {
		return skin.Skin{}, fmt.Errorf("SkinGeometry was not a valid JSON string: %w", err)
	}

	if s.ModelConfig, err = skin.DecodeModelConfig(sk.SkinResourcePatch); err != nil {
		return skin.Skin{}, fmt.Errorf("SkinResourcePatch was not a valid JSON string: %w", err)
	}

	for _, anim := range sk.Animations {
		var t skin.AnimationType
		switch anim.AnimationType {
		case protocol.SkinAnimationHead:
			t = skin.AnimationHead
		case protocol.SkinAnimationBody32x32:
			t = skin.AnimationBody32x32
		case protocol.SkinAnimationBody128x128:
			t = skin.AnimationBody128x128
		default:
			return skin.Skin{}, fmt.Errorf("invalid animation type: %v", anim.AnimationType)
		}

		frameCount := int(anim.FrameCount)
		if frameCount <= 0 {
			return skin.Skin{}, fmt.Errorf("invalid animation frame count: %v", anim.FrameCount)
		}
		animDataLen, err := expectedRGBABytes(int(anim.ImageWidth), int(anim.ImageHeight))
		if err != nil {
			return skin.Skin{}, fmt.Errorf("invalid animation dimensions: %w", err)
		}
		animDataLen *= frameCount
		if len(anim.ImageData) != animDataLen {
			return skin.Skin{}, fmt.Errorf("invalid animation data length: got %d, want %d", len(anim.ImageData), animDataLen)
		}

		animation := skin.NewAnimation(int(anim.ImageWidth), int(anim.ImageHeight), int(anim.ExpressionType), t)
		animation.FrameCount = frameCount
		animation.Pix = append([]byte(nil), anim.ImageData...)

		s.Animations = append(s.Animations, animation)
	}
	return
}

// expectedRGBABytes returns the RGBA buffer length for the supplied dimensions.
func expectedRGBABytes(width, height int) (int, error) {
	const bytesPerPixel = 4
	if width <= 0 || height <= 0 {
		return 0, fmt.Errorf("non-positive dimensions: %dx%d", width, height)
	}
	maxInt := int(^uint(0) >> 1)
	if width > maxInt/bytesPerPixel/height {
		return 0, fmt.Errorf("dimensions overflow: %dx%d", width, height)
	}
	return width * height * bytesPerPixel, nil
}

func validSkinDimensions(width, height int) bool {
	switch {
	case width == 64 && (height == 32 || height == 64):
		return true
	case width == 128 && height == 128:
		return true
	default:
		return false
	}
}

func validCapeDimensions(width, height int) bool {
	return width == 32 && height == 64
}

// GameTypeFromMode returns the protocol game type ID for mode.
func GameTypeFromMode(mode world.GameMode) int32 {
	if mode.AllowsFlying() && mode.CreativeInventory() {
		return packet.GameTypeCreative
	}
	if !mode.Visible() && !mode.HasCollision() {
		return packet.GameTypeSpectator
	}
	if !mode.AllowsEditing() {
		return packet.GameTypeAdventure
	}
	return packet.GameTypeSurvival
}

// The following functions use the go:linkname directive in order to make sure the item.byID and item.toID
// functions do not need to be exported.

// noinspection ALL
//
//go:linkname item_id github.com/df-mc/dragonfly/server/item.id
func item_id(s item.Stack) int32
