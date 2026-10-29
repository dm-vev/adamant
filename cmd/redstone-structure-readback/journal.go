package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/df-mc/goleveldb/leveldb/journal"
)

type auditJournalDropper struct{ err error }

func (d *auditJournalDropper) Drop(err error) {
	if d.err == nil {
		d.err = err
	}
}

// readAuditJournals is deliberately limited to immutable, uniquely named audit
// snapshots. It does not recover a mutable world database. Duplicate values
// must agree byte-for-byte; any deletion or conflicting value fails readback.
// Native WAL checksums, fragment assembly and complete batch lengths are all
// verified. No journal, manifest or table file is written.
func readAuditJournals(directory, prefix string, records map[string][]byte) (int, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return 0, err
	}
	type entry struct {
		name   string
		number uint64
	}
	var files []entry
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".log") {
			continue
		}
		n, err := strconv.ParseUint(strings.TrimSuffix(e.Name(), ".log"), 10, 64)
		if err != nil {
			return 0, fmt.Errorf("unexpected journal filename %s", e.Name())
		}
		files = append(files, entry{e.Name(), n})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].number < files[j].number })
	batches := 0
	for _, file := range files {
		n, err := readAuditJournal(filepath.Join(directory, file.name), prefix, records)
		if err != nil {
			return 0, fmt.Errorf("read %s: %w", file.name, err)
		}
		batches += n
	}
	return batches, nil
}

func readAuditJournal(path, prefix string, records map[string][]byte) (int, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer file.Close()
	dropper := &auditJournalDropper{}
	reader := journal.NewReader(file, dropper, true, true)
	count := 0
	for {
		record, err := reader.Next()
		if dropper.err != nil {
			return 0, dropper.err
		}
		if err == io.EOF {
			return count, nil
		}
		if err != nil {
			return 0, err
		}
		data, err := io.ReadAll(record)
		if dropper.err != nil {
			return 0, dropper.err
		}
		if err != nil {
			return 0, err
		}
		if err := readAuditBatch(data, prefix, records); err != nil {
			return 0, err
		}
		count++
	}
}

func readAuditBatch(data []byte, prefix string, records map[string][]byte) error {
	if len(data) < 12 {
		return fmt.Errorf("truncated write-batch header")
	}
	sequence, count := binary.LittleEndian.Uint64(data), binary.LittleEndian.Uint32(data[8:])
	if sequence > ^uint64(0)-uint64(count) {
		return fmt.Errorf("write-batch sequence overflow")
	}
	data = data[12:]
	readSlice := func() ([]byte, error) {
		size, n := binary.Uvarint(data)
		if n <= 0 || size > uint64(len(data)-n) {
			return nil, fmt.Errorf("invalid write-batch byte string")
		}
		value := data[n : n+int(size)]
		data = data[n+int(size):]
		return value, nil
	}
	for range count {
		if len(data) == 0 {
			return fmt.Errorf("truncated write-batch record")
		}
		kind := data[0]
		data = data[1:]
		if kind > 1 {
			return fmt.Errorf("unknown write-batch record type %d", kind)
		}
		key, err := readSlice()
		if err != nil {
			return err
		}
		var value []byte
		if kind == 1 {
			value, err = readSlice()
			if err != nil {
				return err
			}
		}
		if !bytes.HasPrefix(key, []byte(prefix)) {
			continue
		}
		if kind == 0 {
			return fmt.Errorf("immutable audit snapshot was deleted: %s", key)
		}
		if previous, exists := records[string(key)]; exists && !bytes.Equal(previous, value) {
			return fmt.Errorf("conflicting immutable audit snapshot: %s", key)
		}
		records[string(key)] = bytes.Clone(value)
	}
	if len(data) != 0 {
		return fmt.Errorf("trailing write-batch bytes")
	}
	return nil
}
