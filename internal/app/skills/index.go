package skills

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"time"
)

// entryKey identifies one installed skill file: a (skill, target) pair.
type entryKey struct{ skill, target string }

// Entry records one installed skill file. Hash is the digest of what the app
// last wrote, which is what lets sync tell an app-side change (the file still
// matches Hash, but we would now render something else — safe to overwrite) from
// a user edit (the file no longer matches Hash — never clobber).
type Entry struct {
	Skill       string    `json:"skill"`
	Target      string    `json:"target"`
	Path        string    `json:"path"`
	Hash        string    `json:"hash"`
	InstalledAt time.Time `json:"installedAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// index is the persisted set of installed skill files, one JSON document beside
// the state dir. It is a cache of what the app wrote; the files on disk are the
// truth, and a file whose digest no longer matches its entry was edited outside
// the app. Guard access with the Installer's mutex.
type index struct {
	path    string
	entries map[entryKey]Entry
}

type indexDocument struct {
	Entries []Entry `json:"entries"`
}

func loadIndex(path string) (*index, error) {
	idx := &index{path: path, entries: map[entryKey]Entry{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return idx, nil
	}
	if err != nil {
		return nil, fmt.Errorf("skills: read index: %w", err)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return idx, nil
	}
	var doc indexDocument
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("skills: parse index: %w", err)
	}
	for _, e := range doc.Entries {
		idx.entries[entryKey{e.Skill, e.Target}] = e
	}
	return idx, nil
}

func (i *index) save() error {
	entries := make([]Entry, 0, len(i.entries))
	for _, e := range i.entries {
		entries = append(entries, e)
	}
	sort.Slice(entries, func(a, b int) bool {
		if entries[a].Skill != entries[b].Skill {
			return entries[a].Skill < entries[b].Skill
		}
		return entries[a].Target < entries[b].Target
	})
	data, err := json.MarshalIndent(indexDocument{Entries: entries}, "", "  ")
	if err != nil {
		return fmt.Errorf("skills: marshal index: %w", err)
	}
	return atomicWrite(i.path, append(data, '\n'))
}
