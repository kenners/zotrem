package remarkable

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/cockroachdb/errors"
)

type Entry struct {
	Hash     string
	ID       string
	Size     int64
	Subfiles int
	Type     string
}

type Index struct {
	Entries []Entry
	Schema  int
}

func (e Entry) IsCollection() bool { return e.Type == "80000000" || e.Subfiles > 0 }

func ParseIndex(text string) (Index, error) {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if len(lines) == 0 || lines[0] == "" {
		return Index{}, errors.New("empty index")
	}
	schema, err := strconv.Atoi(strings.TrimSpace(lines[0]))
	if err != nil || (schema != 3 && schema != 4) {
		return Index{}, errors.Newf("unsupported index schema %q", lines[0])
	}
	rest := lines[1:]
	if schema == 4 {
		if len(rest) == 0 {
			return Index{}, errors.New("schema 4 index missing info line")
		}
		info := strings.Split(rest[0], ":")
		if len(info) != 4 || info[0] != "0" {
			return Index{}, errors.Newf("malformed schema 4 info line %q", rest[0])
		}
		rest = rest[1:]
	}
	idx := Index{Schema: schema}
	for _, line := range rest {
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.Split(line, ":")
		if len(parts) != 5 {
			return Index{}, errors.Newf("malformed index line %q", line)
		}
		subfiles, err := strconv.Atoi(parts[3])
		if err != nil {
			return Index{}, errors.Wrapf(err, "index line %q", line)
		}
		size, err := strconv.ParseInt(parts[4], 10, 64)
		if err != nil {
			return Index{}, errors.Wrapf(err, "index line %q", line)
		}
		idx.Entries = append(idx.Entries, Entry{Hash: parts[0], ID: parts[2], Size: size, Subfiles: subfiles, Type: parts[1]})
	}
	return idx, nil
}

func EncodeIndex(id string, entries []Entry, schema int) ([]byte, Entry, error) {
	if schema != 3 && schema != 4 {
		return nil, Entry{}, errors.Newf("unsupported index schema %d", schema)
	}
	sorted := make([]Entry, len(entries))
	copy(sorted, entries)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })
	var size int64
	for _, e := range sorted {
		size += e.Size
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d\n", schema)
	if schema == 4 {
		name := id
		if id == "root" {
			name = "."
		}
		fmt.Fprintf(&b, "0:%s:%d:%d\n", name, len(sorted), size)
	}
	for _, e := range sorted {
		lineType := e.Type
		if schema == 4 {
			lineType = "0"
		}
		fmt.Fprintf(&b, "%s:%s:%s:%d:%d\n", e.Hash, lineType, e.ID, e.Subfiles, e.Size)
	}
	data := []byte(b.String())
	var hash string
	if schema == 3 {
		h := sha256.New()
		for _, e := range sorted {
			raw, err := hex.DecodeString(e.Hash)
			if err != nil {
				return nil, Entry{}, errors.Wrapf(err, "entry hash %q", e.Hash)
			}
			h.Write(raw)
		}
		hash = hex.EncodeToString(h.Sum(nil))
	} else {
		hash = HashBytes(data)
	}
	entryType := "80000000"
	if schema == 4 {
		entryType = "0"
	}
	return data, Entry{Hash: hash, ID: id, Size: size, Subfiles: len(sorted), Type: entryType}, nil
}

func FileEntry(id string, data []byte) Entry {
	return Entry{Hash: HashBytes(data), ID: id, Size: int64(len(data)), Type: "0"}
}
