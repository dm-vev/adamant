package mcdb

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/df-mc/goleveldb/leveldb"
	"github.com/df-mc/goleveldb/leveldb/filter"
	"github.com/df-mc/goleveldb/leveldb/opt"
	"github.com/dm-vev/adamant/server/logging"
	"github.com/dm-vev/adamant/server/world"
	"github.com/dm-vev/adamant/server/world/mcdb/leveldat"
)

// Config holds the optional parameters of a DB.
type Config struct {
	// Log is the Logger that will be used to log errors and debug messages to.
	// If set to nil, Log is set to slog.Default().
	Log *slog.Logger
	// LDBOptions holds LevelDB specific default options, such as the block size
	// or compression used in the database.
	LDBOptions *opt.Options

	// Blocks is the BlockRegistry used for chunk decoding/encoding. If nil, world.DefaultBlockRegistry is used.
	// When using a non-default registry, pass the same registry used by the World.
	Blocks world.BlockRegistry
}

// Open creates a new DB reading and writing from/to files under the path
// passed. If a world is present at the path, Open will parse its data and
// initialise the world with it. If the data cannot be parsed, an error is
// returned.
func (conf Config) Open(dir string) (*DB, error) {
	if conf.Log == nil {
		conf.Log = slog.Default()
	}
	conf.Log = logging.Sub(conf.Log, logging.Save).With("provider", "mcdb")
	opts := new(opt.Options)
	if conf.LDBOptions != nil {
		*opts = *conf.LDBOptions
	}
	applyDefaultLDBOptions(opts)
	conf.LDBOptions = opts

	_ = os.MkdirAll(filepath.Join(dir, "db"), 0777)

	db := &DB{conf: conf, dir: dir, ldat: &leveldat.Data{}}
	db.SetBlockRegistry(conf.Blocks)
	found, err := readLevelDat(dir, db.ldat, conf.Log)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	ldb, err := leveldb.OpenFile(filepath.Join(dir, "db"), opts)
	if err != nil {
		return nil, fmt.Errorf("open db: leveldb: %w", err)
	}
	db.ldb = ldb
	// level.dat is written after each checkpoint is committed: a crash in
	// between leaves it older than the checkpoint's copy of it.
	if found, err = db.reconcile(found); err != nil {
		_ = ldb.Close()
		return nil, fmt.Errorf("open db: %w", err)
	}
	if r, err := db.LastCheckpoint(); err == nil && r.ID != 0 {
		conf.Log.Info("Opened the world at its last checkpoint.", "checkpoint", r.ID, "time", r.Time.Format(time.RFC3339))
	}
	newWorld := !found
	if newWorld {
		// A level.dat was not currently present for the world.
		db.ldat.FillDefault()
	}
	db.set = db.ldat.Settings()
	db.set.SeedSet = !newWorld
	return db, nil
}

// readLevelDat decodes the level.dat of the world in dir into dst. If it is
// missing or cannot be read or decoded, the level.dat_old the last save kept
// beside it is decoded instead, as Bedrock Dedicated Server does. found is
// false if neither exists, or if the level.dat is missing and the
// level.dat_old cannot be decoded either: the world then gets new settings,
// as one without a level.dat always did. If the level.dat exists but neither
// can be decoded, readLevelDat returns the error of the level.dat.
func readLevelDat(dir string, dst *leveldat.Data, log *slog.Logger) (found bool, err error) {
	err = decodeLevelDat(filepath.Join(dir, "level.dat"), "level.dat", dst)
	if err == nil {
		return true, nil
	}
	missing := errors.Is(err, fs.ErrNotExist)
	oldErr := decodeLevelDat(filepath.Join(dir, "level.dat_old"), "level.dat_old", dst)
	switch {
	case oldErr == nil:
		log.Warn("Could not read level.dat, read level.dat_old instead.", "error", err)
		return true, nil
	case missing && errors.Is(oldErr, fs.ErrNotExist):
		return false, nil
	case missing:
		log.Warn("level.dat is missing and level.dat_old could not be read: the world gets new settings.", "error", oldErr)
		return false, nil
	case errors.Is(oldErr, fs.ErrNotExist):
		return false, err
	default:
		return false, fmt.Errorf("%w (level.dat_old: %w)", err, oldErr)
	}
}

// decodeLevelDat decodes the level.dat file at path, called name in errors,
// into dst. dst is changed only if decoding succeeds.
func decodeLevelDat(path, name string, dst *leveldat.Data) error {
	ldat, err := leveldat.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %v: %w", name, err)
	}
	ver := ldat.Ver()
	if ver != leveldat.Version && ver >= 10 {
		return fmt.Errorf("%v version %v is unsupported", name, ver)
	}
	var data leveldat.Data
	if err = ldat.Unmarshal(&data); err != nil {
		return fmt.Errorf("unmarshal %v: %w", name, err)
	}
	*dst = data
	return nil
}

// applyDefaultLDBOptions fills unset LevelDB options with defaults tuned for
// checkpoint-heavy Minecraft worlds: Snappy compression avoids goleveldb's
// global mutex on Flate compression/decompression while still reading existing
// Flate blocks transparently; a 10-bit Bloom filter avoids disk seeks on
// missing chunk/actor keys; and larger write buffers and compaction tables
// keep multi-thousand-chunk commits from stalling on L0->L1 compaction cascades.
func applyDefaultLDBOptions(opts *opt.Options) {
	if opts.BlockSize == 0 {
		opts.BlockSize = 16 * opt.KiB
	}
	if opts.Compression == opt.DefaultCompression {
		opts.Compression = opt.SnappyCompression
	}
	if opts.Filter == nil {
		opts.Filter = filter.NewBloomFilter(10)
	}
	if opts.BlockCacheCapacity == 0 {
		opts.BlockCacheCapacity = 32 * opt.MiB
	}
	if opts.WriteBuffer == 0 {
		opts.WriteBuffer = 16 * opt.MiB
	}
	if opts.CompactionTableSize == 0 {
		opts.CompactionTableSize = 8 * opt.MiB
	}
	if opts.CompactionTotalSize == 0 {
		opts.CompactionTotalSize = 64 * opt.MiB
	}
	if opts.CompactionL0Trigger == 0 {
		opts.CompactionL0Trigger = 8
	}
	if opts.WriteL0SlowdownTrigger == 0 {
		opts.WriteL0SlowdownTrigger = max(20, opts.CompactionL0Trigger+4)
	}
	if opts.WriteL0PauseTrigger == 0 {
		opts.WriteL0PauseTrigger = max(32, opts.WriteL0SlowdownTrigger+4)
	}
}
