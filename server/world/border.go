package world

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/goleveldb/leveldb"
	"github.com/go-gl/mathgl/mgl64"
)

const MaxBorderCoordinate = 29999984
const MaxBorderSize = float64(float32(5.999997e7))

// BorderSettings is Java's world_border SavedData payload. Border data is
// dimension-owned, separate from shared level.dat settings and native BDS keys.
type BorderSettings struct {
	CenterX        float64 `json:"center_x"`
	CenterZ        float64 `json:"center_z"`
	DamagePerBlock float64 `json:"damage_per_block"`
	SafeZone       float64 `json:"safe_zone"`
	WarningBlocks  int32   `json:"warning_blocks"`
	WarningTime    int32   `json:"warning_time"`
	Size           float64 `json:"size"`
	LerpTime       int64   `json:"lerp_time"`
	LerpTarget     float64 `json:"lerp_target"`
}

// DefaultBorderSettings matches a ServerLevel after applyInitialSettings.
func DefaultBorderSettings() BorderSettings {
	return BorderSettings{DamagePerBlock: .2, SafeZone: 5, WarningBlocks: 5, WarningTime: 300, Size: MaxBorderSize, LerpTarget: MaxBorderSize}
}

type worldBorder struct {
	settings           BorderSettings
	from, previousSize float64
	duration           int64
}

func (b *worldBorder) apply(settings BorderSettings) {
	b.settings = settings
	b.from, b.previousSize, b.duration = settings.Size, settings.Size, settings.LerpTime
	if settings.LerpTime <= 0 || settings.Size == settings.LerpTarget {
		b.settings.LerpTime = 0
		b.settings.LerpTarget = settings.Size
		b.duration = 0
	}
}
func (b *worldBorder) tick() {
	if b.settings.LerpTime <= 0 {
		return
	}
	b.settings.LerpTime--
	b.previousSize = b.settings.Size
	progress := float64(b.duration-b.settings.LerpTime) / float64(b.duration)
	b.settings.Size = b.from + progress*(b.settings.LerpTarget-b.from)
	if b.settings.LerpTime == 0 {
		b.settings.Size = b.settings.LerpTarget
		b.previousSize = b.settings.Size
	}
}
func (b *worldBorder) bounds() (minX, maxX, minZ, maxZ float64) {
	// MovingBorderExtent's zero-partial-tick bounds use previousSize, not its
	// newly advanced size. The completed extent becomes static immediately.
	half := b.previousSize / 2
	clamp := func(v float64) float64 { return max(-MaxBorderCoordinate, min(MaxBorderCoordinate, v)) }
	return clamp(b.settings.CenterX - half), clamp(b.settings.CenterX + half), clamp(b.settings.CenterZ - half), clamp(b.settings.CenterZ + half)
}
func (b *worldBorder) contains(x, z float64) bool {
	minX, maxX, minZ, maxZ := b.bounds()
	return x >= minX && x < maxX && z >= minZ && z < maxZ
}
func (b *worldBorder) containsBox(box cube.BBox) bool {
	min, max := box.Min(), box.Max()
	const epsilon = float64(float32(1e-5))
	return b.contains(min[0], min[2]) && b.contains(max[0]-epsilon, max[2]-epsilon)
}

// Border returns a detached persisted snapshot, including remaining transition
// ticks. Runtime border queries avoid copying this snapshot.
func (w *World) Border() BorderSettings {
	if w == nil {
		return DefaultBorderSettings()
	}
	w.set.Lock()
	defer w.set.Unlock()
	return w.border.settings
}

// SetBorder applies saved settings and starts any remaining size transition.
func (w *World) SetBorder(settings BorderSettings) {
	if w == nil {
		return
	}
	w.set.Lock()
	defer w.set.Unlock()
	w.border.apply(settings)
}

func (w *World) SetBorderCenter(x, z float64) {
	if w == nil {
		return
	}
	w.set.Lock()
	defer w.set.Unlock()
	w.border.settings.CenterX, w.border.settings.CenterZ = x, z
}

// SetBorderSize transitions from the current size over ticks. Zero ticks sets
// a static extent. Border progress uses game ticks, not wall-clock time.
func (w *World) SetBorderSize(size float64, ticks int64) {
	if w == nil {
		return
	}
	w.set.Lock()
	defer w.set.Unlock()
	b := &w.border
	b.from, b.previousSize, b.duration = b.settings.Size, b.settings.Size, ticks
	b.settings.LerpTarget, b.settings.LerpTime = size, ticks
	if ticks <= 0 || b.settings.Size == size {
		b.settings.Size = size
		b.previousSize = size
		b.settings.LerpTime = 0
		b.duration = 0
	}
}

// WithinBorder implements Source's half-open BlockPos check, without clamping
// a rejected position into the world. Build-height bounds are independent.
func (w *World) WithinBorder(pos cube.Pos) bool {
	if w == nil {
		return false
	}
	w.set.Lock()
	defer w.set.Unlock()
	return w.border.contains(float64(pos[0]), float64(pos[2]))
}

// WithinBorderBounds checks both horizontal corners with Source's float epsilon.
func (w *World) WithinBorderBounds(box cube.BBox) bool {
	if w == nil {
		return false
	}
	w.set.Lock()
	defer w.set.Unlock()
	return w.border.containsBox(box)
}

// ClampToBorder keeps Y unchanged and excludes the border's maximum edge.
func (w *World) ClampToBorder(pos mgl64.Vec3) mgl64.Vec3 {
	if w == nil {
		return pos
	}
	w.set.Lock()
	defer w.set.Unlock()
	minX, maxX, minZ, maxZ := w.border.bounds()
	const epsilon = float64(float32(1e-5))
	return mgl64.Vec3{max(minX, min(maxX-epsilon, pos[0])), pos[1], max(minZ, min(maxZ-epsilon, pos[2]))}
}

func (w *World) borderNamespace() string { return "world-border:" + dimensionStorageKey(w.Dimension()) }
func (w *World) loadBorder() error {
	w.border.apply(DefaultBorderSettings())
	store, ok := w.conf.Provider.(worldDataProvider)
	if !ok {
		return nil
	}
	data, err := store.LoadWorldData(w.borderNamespace())
	if errors.Is(err, leveldb.ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("load world border: %w", err)
	}
	var settings BorderSettings
	if err = json.Unmarshal(data, &settings); err != nil {
		return fmt.Errorf("decode world border: %w", err)
	}
	if math.Abs(settings.CenterX) > MaxBorderCoordinate || math.Abs(settings.CenterZ) > MaxBorderCoordinate {
		return fmt.Errorf("world border center outside Source codec bounds")
	}
	w.border.apply(settings)
	return nil
}
func (w *World) saveBorder() {
	if w.conf.ReadOnly {
		return
	}
	store, ok := w.conf.Provider.(worldDataProvider)
	if !ok {
		return
	}
	data, err := json.Marshal(w.Border())
	if err != nil {
		w.conf.Log.Error("encode world border: " + err.Error())
		return
	}
	if err = store.SaveWorldData(w.borderNamespace(), data); err != nil {
		w.conf.Log.Error("save world border: " + err.Error())
	}
}
