package builtin

import (
	"slices"
	"strings"
	"testing"

	"github.com/df-mc/goleveldb/leveldb"
	"github.com/dm-vev/adamant/server/cmd"
	"github.com/dm-vev/adamant/server/world"
)

type legacyStorageDataProvider struct {
	world.NopProvider
	data map[string][]byte
}

func (p *legacyStorageDataProvider) LoadWorldData(ns string) ([]byte, error) {
	if b, ok := p.data[ns]; ok {
		return slices.Clone(b), nil
	}
	return nil, leveldb.ErrNotFound
}

func (p *legacyStorageDataProvider) SaveWorldData(ns string, data []byte) error {
	if data == nil {
		delete(p.data, ns)
		return nil
	}
	p.data[ns] = slices.Clone(data)
	return nil
}

func TestDataStorageLegacyNamespaceFallbackAndMigrate(t *testing.T) {
	t.Parallel()
	p := &legacyStorageDataProvider{
		data: map[string][]byte{"java:storage:test:key": []byte(`{"count":5}`)},
	}
	w := world.Config{Provider: p, Synchronous: true, RandomTickSpeed: -1}.New()

	<-w.Exec(func(tx *world.Tx) {
		out := &cmd.Output{}
		dataStorage(&dataCall{action: "get", words: []string{"get", "storage", "test:key"}, out: out, tx: tx})
		if len(out.Messages()) != 1 || !strings.Contains(out.Messages()[0].String(), `{"count":5}`) {
			t.Fatalf("get legacy storage messages=%v errs=%v", out.Messages(), out.Errors())
		}
		storeDataValue(tx, "test:key", "next", "int", 9)
	})
	_ = w.Close()
	if got := string(p.data["cmd:storage:test:key"]); !strings.Contains(got, `"count":5`) || !strings.Contains(got, `"next":9`) {
		t.Fatalf("migrated cmd:storage payload = %q", got)
	}
}
