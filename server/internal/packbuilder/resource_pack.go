package packbuilder

import (
	_ "embed"
	"github.com/sandertv/gophertunnel/minecraft/resource"
	"golang.org/x/mod/sumdb/dirhash"
	"log/slog"
	"os"
)

//go:embed pack_icon.png
var packIcon []byte

// BuildResourcePack builds a resource pack based on custom features that have been registered to the server.
// It creates a UUID based on the hash of the directory so the client will only be prompted to download it
// once it is changed.
// Errors are logged and cause the resource pack to be skipped so the server can keep running.
func BuildResourcePack() (*resource.Pack, bool) {
	log := slog.Default()

	dir, err := os.MkdirTemp("", "dragonfly_resource_pack-")
	if err != nil {
		log.Error("resource pack: create temp dir failed", "err", err)
		return nil, false
	}
	defer func() {
		if err := os.RemoveAll(dir); err != nil {
			log.Warn("resource pack: cleanup temp dir failed", "err", err, "dir", dir)
		}
	}()

	var assets int
	var lang []string

	itemCount, itemLang, err := buildItems(dir)
	if err != nil {
		log.Error("resource pack: build items failed", "err", err)
		return nil, false
	}
	assets += itemCount
	lang = append(lang, itemLang...)

	blockCount, blockLang, err := buildBlocks(dir)
	if err != nil {
		log.Error("resource pack: build blocks failed", "err", err)
		return nil, false
	}
	assets += blockCount
	lang = append(lang, blockLang...)

	if assets > 0 {
		if err := buildLanguageFile(dir, lang); err != nil {
			log.Error("resource pack: write language file failed", "err", err)
			return nil, false
		}
		if err := os.WriteFile(dir+"/pack_icon.png", packIcon, 0666); err != nil {
			log.Error("resource pack: write pack icon failed", "err", err)
			return nil, false
		}
		hash, err := dirhash.HashDir(dir, "", dirhash.Hash1)
		if err != nil {
			log.Error("resource pack: hash build failed", "err", err)
			return nil, false
		}
		var header, module [16]byte
		copy(header[:], hash)
		copy(module[:], hash[16:])
		if err := buildManifest(dir, header, module); err != nil {
			log.Error("resource pack: build manifest failed", "err", err)
			return nil, false
		}
		pack, err := resource.ReadPath(dir)
		if err != nil {
			log.Error("resource pack: read pack failed", "err", err)
			return nil, false
		}
		return pack, true
	}
	return nil, false
}
