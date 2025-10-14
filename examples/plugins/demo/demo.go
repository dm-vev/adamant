package main

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/df-mc/dragonfly/server"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/player/chat"
	"github.com/df-mc/dragonfly/server/player/form"
	"github.com/df-mc/dragonfly/server/player/title"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// Init is one of the supported plugin factory names. The manager will call it and
// provide a fully initialised PluginAPI scoped to the demo module.
func Init(api *server.PluginAPI) (server.Plugin, error) {
	demo := &pluginDemo{api: api, log: api.Logger().With("component", "demo")}
	if err := demo.init(); err != nil {
		return nil, err
	}
	return demo, nil
}

// pluginDemo showcases most of the plugin API surface.
type pluginDemo struct {
	api    *server.PluginAPI
	log    *slog.Logger
	unsub  []func()
	chat   *chatLogger
	loaded time.Time
}

// Name is part of the server.Plugin interface.
func (d *pluginDemo) Name() string { return "Plugin API Demo" }

// Close tears down runtime state when the plugin is disabled.
func (d *pluginDemo) Close() error {
	for i := len(d.unsub) - 1; i >= 0; i-- {
		d.unsub[i]()
	}
	d.unsub = nil
	if events := d.api.Events(); events != nil {
		events.Clear()
	}
	if d.chat != nil {
		d.api.UnsubscribeChat(d.chat)
		d.chat = nil
	}
	d.log.Info("Shut down demo plugin.")
	return nil
}

func (d *pluginDemo) init() error {
	d.loaded = time.Now()
	d.log.Info("Initialising demo plugin.",
		"pluginDir", d.api.PluginDirectory(),
		"dataRoot", d.api.PluginDataRoot(),
		"start", d.api.StartTime().Format(time.RFC3339),
		"pluginsEnabled", d.api.PluginsEnabled(),
	)

	if err := d.prepareData(); err != nil {
		return err
	}
	d.installChatLogger()
	d.registerEvents()
	d.registerCommand()
	d.startMetricsLoop()
	d.startJoinWatcher()
	d.logLoadedPlugins()
	return nil
}

func (d *pluginDemo) prepareData() error {
	stateDir, err := d.api.EnsureDataSubdir("state")
	if err != nil {
		return fmt.Errorf("ensure state directory: %w", err)
	}
	logFile, err := d.api.OpenDataFile(filepath.Join("state", "startup.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open startup log: %w", err)
	}
	defer logFile.Close()
	if _, err := fmt.Fprintf(logFile, "Started at %s\n", d.loaded.Format(time.RFC3339)); err != nil {
		return fmt.Errorf("write startup log: %w", err)
	}

	if _, err := d.api.EnsureDataSubdir(filepath.Join("players", "forms")); err != nil {
		return fmt.Errorf("ensure nested data dir: %w", err)
	}

	resolved := d.api.ResolvePluginPath("demo")
	d.log.Debug("Resolved plugin path.", "input", "demo", "resolved", resolved)
	d.log.Info("State prepared.", "stateDir", stateDir)
	return nil
}

func (d *pluginDemo) installChatLogger() {
	d.chat = newChatLogger(d.log)
	d.api.SubscribeChat(d.chat)
}

func (d *pluginDemo) registerEvents() {
	events := d.api.Events()
	d.unsub = append(d.unsub,
		events.OnPlayer(playerHandler{demo: d}),
		events.OnWorld(worldHandler{demo: d}),
		events.OnInventory(inventoryHandler{demo: d}),
	)
}

func (d *pluginDemo) registerCommand() {
	d.api.RegisterCommand(newDemoCommand(d))
}

func (d *pluginDemo) startMetricsLoop() {
	d.api.Go(func(ctx context.Context) {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				d.log.Info("Stopping metrics loop.")
				return
			case <-ticker.C:
				summaries := d.api.PlayerSummaries()
				names := make([]string, 0, len(summaries))
				for _, summary := range summaries {
					names = append(names, summary.Name)
				}
				d.log.Info("Server metrics.",
					"players", d.api.PlayerCount(),
					"max", d.api.MaxPlayerCount(),
					"online", strings.Join(names, ", "))
			}
		}
	})
}

func (d *pluginDemo) startJoinWatcher() {
	d.api.Go(func(ctx context.Context) {
		for player := range d.api.Accept() {
			select {
			case <-ctx.Done():
				return
			default:
			}
			d.log.Info("Player accepted.", "name", player.Name(), "uuid", player.UUID())
		}
	})
}

func (d *pluginDemo) logLoadedPlugins() {
	infos := d.api.Plugins()
	if len(infos) == 0 {
		d.log.Info("No other plugins are currently loaded.")
		return
	}
	attrs := make([]any, 0, len(infos)*4)
	for _, info := range infos {
		attrs = append(attrs, "name", info.Name, "version", info.Version, "path", info.Path)
	}
	d.log.Info("Loaded plugins detected.", attrs...)
}

// chatLogger forwards chat messages into the plugin logger.
type chatLogger struct {
	id  uuid.UUID
	log *slog.Logger
}

func newChatLogger(log *slog.Logger) *chatLogger {
	return &chatLogger{id: uuid.New(), log: log.With("component", "chat")} //nolint:exhaustruct
}

func (c *chatLogger) UUID() uuid.UUID { return c.id }

func (c *chatLogger) Message(message ...any) {
	c.log.Info("Chat message.", "text", fmt.Sprint(message...))
}

// playerHandler demonstrates player event hooks.
type playerHandler struct {
	player.NopHandler
	demo *pluginDemo
}

func (h playerHandler) HandleChangeWorld(p *player.Player, before, after *world.World) {
	if before != nil {
		return
	}
	worldName := ""
	if after != nil {
		worldName = after.Name()
	}
	h.demo.log.Info("Player joined.", "name", p.Name(), "world", worldName)
	if summary, ok := h.demo.api.PlayerSummary(p.UUID()); ok {
		h.demo.log.Info("Player summary.", "latency", summary.Latency, "xuid", summary.XUID)
	}
	titleMsg := title.New("Welcome to Dragonfly!").WithSubtitle("Enjoy the demo plugin")
	h.demo.api.SendTitle(p.UUID(), titleMsg)
	h.demo.api.MessagePlayer(p.UUID(), "<green>Welcome! This server runs the plugin API demo.")
	h.demo.api.SendPopup(p.UUID(), "Try /plugindemo for plugin tricks!")
	h.demo.api.SendTip(p.UUID(), "Forms, commands, and more.")
	h.demo.api.SendForm(p.UUID(), newWelcomeForm(h.demo))
}

func (h playerHandler) HandleChat(ctx *player.Context, message *string) {
	if message == nil {
		return
	}
	if strings.HasPrefix(*message, "!shout ") {
		ctx.Cancel()
		payload := strings.TrimPrefix(*message, "!shout ")
		h.demo.api.Broadcast(fmt.Sprintf("[Shout] %s", payload))
	}
}

func (h playerHandler) HandleQuit(p *player.Player) {
	h.demo.log.Info("Player quit.", "name", p.Name())
}

func (h playerHandler) HandleItemUseOnBlock(ctx *player.Context, pos cube.Pos, _ cube.Face, _ mgl64.Vec3) {
	if rand.Float64() < 0.05 { // small chance to cancel the interaction
		ctx.Cancel()
		if p := ctx.Val(); p != nil {
			h.demo.api.MessagePlayer(p.UUID(), "<yellow>The demo plugin blocked that interaction at ", pos)
		}
	}
}

// worldHandler demonstrates world event hooks.
type worldHandler struct {
	world.NopHandler
	demo *pluginDemo
}

func (h worldHandler) HandleSound(ctx *world.Context, s world.Sound, pos mgl64.Vec3) {
	if ctx == nil {
		return
	}
	if _, ok := s.(sound.Explosion); ok {
		ctx.Cancel()
		h.demo.log.Warn("Cancelled explosion sound via plugin event.", "pos", pos)
	}
}

func (h worldHandler) HandleClose(tx *world.Tx) {
	h.demo.log.Info("World closed.", "name", tx.World().Name())
}

// inventoryHandler demonstrates inventory event hooks.
type inventoryHandler struct {
	inventory.NopHandler
	demo *pluginDemo
}

func (h inventoryHandler) HandleTake(ctx *inventory.Context, slot int, it item.Stack) {
	if ctx == nil {
		return
	}
	if holder, ok := ctx.Val().(*player.Player); ok {
		itemName := "air"
		if itm := it.Item(); itm != nil {
			itemName, _ = itm.EncodeItem()
		}
		h.demo.log.Info("Item taken.", "player", holder.Name(), "slot", slot, "item", itemName, "count", it.Count())
	}
}

// demoCommand exposes runtime information and demonstrates player helpers.
type demoCommand struct {
	plugin  *pluginDemo
	Target  cmd.Optional[string]      `cmd:"player"`
	Message cmd.Optional[cmd.Varargs] `cmd:"message"`
}

func newDemoCommand(p *pluginDemo) cmd.Command {
	return cmd.New("plugindemo", "Interact with the plugin demo.", nil, demoCommand{plugin: p})
}

func (c demoCommand) Run(src cmd.Source, out *cmd.Output, tx *world.Tx) {
	out.Printf("Server started at %s.", c.plugin.api.StartTime().Format(time.RFC3339))
	out.Printf("Plugin data stored in %s.", c.plugin.api.DataDirectory())
	out.Printf("%d/%d players online.", c.plugin.api.PlayerCount(), c.plugin.api.MaxPlayerCount())

	names := make([]string, 0)
	for p := range c.plugin.api.Players(tx) {
		names = append(names, fmt.Sprintf("%s(%s)", p.Name(), p.UUID()))
	}
	if len(names) > 0 {
		out.Print("Online: ", strings.Join(names, ", "))
	}

	if name, ok := c.Target.Load(); ok {
		handle, found := c.plugin.api.PlayerByName(name)
		if !found {
			out.Errorf("Player %s is not online.", name)
			return
		}
		var target *player.Player
		executed := handle.ExecWorld(func(tx *world.Tx, entity world.Entity) {
			if p, ok := entity.(*player.Player); ok {
				target = p
			}
		})
		if !executed || target == nil {
			out.Errorf("Failed to resolve player entity.")
			return
		}
		latency, _ := c.plugin.api.PlayerLatency(target.UUID())
		summary, hasSummary := c.plugin.api.PlayerSummary(target.UUID())
		out.Printf("%s latency: %s.", target.Name(), latency)
		if hasSummary {
			out.Printf("Location: %s at %v.", summary.Dimension, summary.Position)
		}
		message, ok := c.Message.Load()
		if ok {
			c.plugin.api.MessagePlayer(target.UUID(), "<light_purple>", string(message))
			c.plugin.api.SendPopup(target.UUID(), "Demo message delivered!")
			c.plugin.api.SendTitle(target.UUID(), title.New("Hello from plugin!"))
		}
	}

	out.Print("Try supplying a player name to message them.")
}

// welcomeForm is a modal shown to new players demonstrating form utilities.
type welcomeForm struct {
	plugin  *pluginDemo
	Accept  form.Button
	Decline form.Button
}

func newWelcomeForm(p *pluginDemo) form.Modal {
	wf := welcomeForm{ //nolint:exhaustruct
		plugin:  p,
		Accept:  form.YesButton(),
		Decline: form.NoButton(),
	}
	return wf.withBody("Would you like a greeting kit?")
}

func (f welcomeForm) Submit(submitter form.Submitter, pressed form.Button, tx *world.Tx) {
	p, ok := submitter.(*player.Player)
	if !ok {
		return
	}
	switch pressed { // compare by value
	case f.Accept:
		f.plugin.api.MessagePlayer(p.UUID(), "<gold>Enjoy your stay! This kit is imaginary.")
	case f.Decline:
		f.plugin.api.MessagePlayer(p.UUID(), "<gray>No worries! Have fun exploring.")
	}
}

func (f welcomeForm) Close(submitter form.Submitter, tx *world.Tx) {
	if p, ok := submitter.(*player.Player); ok {
		f.plugin.api.MessagePlayer(p.UUID(), "<gray>Maybe later!")
	}
}

// Ensure welcomeForm implements the ModalSubmittable and form.Closer interfaces.
var (
	_ form.ModalSubmittable = welcomeForm{}
	_ form.Closer           = welcomeForm{}
	_ chat.Subscriber       = (*chatLogger)(nil)
)

// Support function for form.Modal to add body text.
func (f welcomeForm) withBody(body ...any) form.Modal {
	modal := form.NewModal(f, "Plugin Demo")
	return modal.WithBody(body...)
}
