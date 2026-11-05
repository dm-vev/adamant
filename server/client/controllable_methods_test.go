package client

import (
	"reflect"
	"sort"
	"testing"
)

// controllableMethods is the method set of Controllable. The role interfaces
// group the methods Controllable declares, so the set must stay exactly the
// same whichever interface a method lives in.
var controllableMethods = []string{
	"AbortBreaking", "Absorption", "AddDebugShape", "AttackEntity", "BeginMenu",
	"CanReach", "CanUseGameMasterBlocks", "Chat", "ClearInputLocks", "ClientMove",
	"Close", "CloseDialogue", "CloseForm", "CloseMenu", "ContinueBreaking",
	"Crawling", "DashVehicle", "Dead", "DestroyBlock", "DismountVehicle",
	"Drop", "DropFromInventory", "EditBook", "EditCommandBlock", "EditCommandMinecart",
	"EditJigsaw", "EditSign", "EditStructureBlock", "Effects", "Emote",
	"EnderChestInventory", "ExecuteCommand", "Exhaust", "ExperienceLevel", "ExperienceProgress",
	"ExploreMap", "FinishBreaking", "FlightSpeed", "Flying", "Food",
	"GameMode", "Gliding", "H", "Health", "HeldItems",
	"HideHudElement", "HudElementHidden", "InputLocked", "Inventory", "Jump",
	"Leave", "LeaveVehicle", "Locale", "LockInput", "MaxHealth",
	"Menu", "Message", "MoveItemsToInventory", "Name", "OpenSign",
	"PickBlock", "Position", "PunchAir", "ReleaseItem", "RemoveAllDebugShapes",
	"RemoveDebugShape", "Respawn", "RideJump", "Rotation", "SendCommandOutput",
	"SendDialogue", "SendForm", "SetCrafterSlotDisabled", "SetGameMode", "SetHeldItems",
	"SetHeldSlot", "SetSkin", "ShowHudElement", "Skin", "Sleep",
	"Sneaking", "Speed", "Sprinting", "StartBreaking", "StartCrawling",
	"StartFlying", "StartGliding", "StartSneaking", "StartSprinting", "StartSwimming",
	"SteerVehicle", "StopCrawling", "StopFlying", "StopGliding", "StopSneaking",
	"StopSprinting", "StopSwimming", "Swimming", "SwingArm", "TurnLecternPage",
	"UUID", "UnlockInput", "UpdateDiagnostics", "UseItem", "UseItemOnBlock",
	"UseItemOnEntity", "UsingItem", "VerticalFlightSpeed", "VisibleDebugShapes", "Wake",
	"XUID",
}

func TestControllableMethodSet(t *testing.T) {
	typ := reflect.TypeOf((*Controllable)(nil)).Elem()
	got := make([]string, 0, typ.NumMethod())
	for i := 0; i < typ.NumMethod(); i++ {
		got = append(got, typ.Method(i).Name)
	}
	sort.Strings(got)
	if !reflect.DeepEqual(got, controllableMethods) {
		t.Fatalf("method set of Controllable changed (%d, want %d):\n%q", len(got), len(controllableMethods), got)
	}
}
