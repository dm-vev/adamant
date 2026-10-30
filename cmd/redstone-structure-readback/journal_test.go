package main

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/df-mc/goleveldb/leveldb"
	"github.com/df-mc/goleveldb/leveldb/journal"
)

func auditTestBatch(batch *leveldb.Batch) []byte {
	header := make([]byte, 12)
	binary.LittleEndian.PutUint64(header, 4825)
	binary.LittleEndian.PutUint32(header[8:], uint32(batch.Len()))
	return append(header, batch.Dump()...)
}

func TestImmutableAuditJournalReplaysFragmentedBatchAndChecksCRC(t *testing.T) {
	prefix := "structuretemplate_mystructure:audit_snap_"
	large := bytes.Repeat([]byte("native-snapshot"), 100000)
	batch := new(leveldb.Batch)
	batch.Put([]byte(prefix+"first"), large)
	batch.Put([]byte("unrelated-key"), []byte("unrelated-value"))
	batch.Put([]byte(prefix+"second"), []byte("second-snapshot"))
	var encoded bytes.Buffer
	writer := journal.NewWriter(&encoded)
	record, err := writer.Next()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = record.Write(auditTestBatch(batch)); err != nil {
		t.Fatal(err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	path := filepath.Join(directory, "000004.log")
	if err = os.WriteFile(path, encoded.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	values := map[string][]byte{prefix + "first": bytes.Clone(large)}
	count, err := readAuditJournals(directory, prefix, values)
	if err != nil || count != 1 || len(values) != 2 || !bytes.Equal(values[prefix+"first"], large) || string(values[prefix+"second"]) != "second-snapshot" {
		t.Fatalf("fragmented audit replay: batches=%d values=%d err=%v", count, len(values), err)
	}
	damaged := bytes.Clone(encoded.Bytes())
	damaged[20] ^= 1
	if err = os.WriteFile(path, damaged, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = readAuditJournals(directory, prefix, map[string][]byte{}); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("damaged native journal accepted: %v", err)
	}
}

func TestImmutableAuditBatchRejectsChangedDeletedAndTruncatedSnapshots(t *testing.T) {
	prefix := "structuretemplate_mystructure:audit_snap_"
	for _, mode := range []string{"changed", "deleted", "truncated"} {
		t.Run(mode, func(t *testing.T) {
			batch := new(leveldb.Batch)
			if mode == "deleted" {
				batch.Delete([]byte(prefix + "one"))
			} else {
				batch.Put([]byte(prefix+"one"), []byte("new-value"))
			}
			data := auditTestBatch(batch)
			values := map[string][]byte{}
			if mode == "changed" {
				values[prefix+"one"] = []byte("old-value")
			}
			if mode == "truncated" {
				data = data[:len(data)-1]
			}
			if err := readAuditBatch(data, prefix, values); err == nil {
				t.Fatal("invalid immutable audit batch accepted")
			}
		})
	}
}
