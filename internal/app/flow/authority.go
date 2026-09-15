package flow

import (
	"context"
	"os"
	"path/filepath"
	"sort"

	"github.com/hay-kot/hive-desktop/internal/app/configwatch"
)

type authority struct{ dir string }

// NewAuthority describes the direct flow files configuration detection reads.
func NewAuthority(dir string) configwatch.Authority { return authority{dir: filepath.Clean(dir)} }

func (a authority) Topology(ctx context.Context) (configwatch.Topology, error) {
	if err := ctx.Err(); err != nil {
		return configwatch.Topology{}, err
	}
	topology := configwatch.Topology{Directories: []string{a.dir}}
	entries, err := os.ReadDir(a.dir)
	if err != nil {
		return configwatch.Topology{}, err
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return configwatch.Topology{}, err
		}
		if entry.IsDir() || !isFlowFile(entry.Name()) {
			continue
		}
		topology.Files = append(topology.Files, configwatch.AuthorityFile{Key: entry.Name(), Path: filepath.Join(a.dir, entry.Name())})
	}
	sort.Slice(topology.Files, func(i, j int) bool { return topology.Files[i].Key < topology.Files[j].Key })
	return topology, nil
}

func (a authority) Match(change configwatch.Change) configwatch.Match {
	path := filepath.Clean(change.Path)
	if path == a.dir && (change.Operation == configwatch.Rename || change.Operation == configwatch.Remove) {
		return configwatch.Match{Dirty: true, RefreshWatches: true}
	}
	if filepath.Dir(path) == a.dir && isFlowFile(path) {
		return configwatch.Match{Dirty: true}
	}
	return configwatch.Match{}
}
