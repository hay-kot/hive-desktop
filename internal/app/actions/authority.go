package actions

import (
	"context"
	"os"
	"path/filepath"

	"github.com/hay-kot/hive-desktop/internal/app/configwatch"
)

type authority struct {
	path string
	dir  string
}

// NewAuthority describes the actions catalog files configuration detection reads.
func NewAuthority(path string) configwatch.Authority {
	return authority{path: filepath.Clean(path), dir: filepath.Dir(filepath.Clean(path))}
}

func (a authority) Topology(ctx context.Context) (configwatch.Topology, error) {
	if err := ctx.Err(); err != nil {
		return configwatch.Topology{}, err
	}
	if _, err := os.ReadDir(a.dir); err != nil {
		return configwatch.Topology{}, err
	}
	return configwatch.Topology{Directories: []string{a.dir}, Files: []configwatch.AuthorityFile{{Key: "actions.yml", Path: a.path}}}, nil
}

func (a authority) Match(change configwatch.Change) configwatch.Match {
	if filepath.Clean(change.Path) == a.path {
		return configwatch.Match{Dirty: true}
	}
	if filepath.Clean(change.Path) == a.dir && (change.Operation == configwatch.Rename || change.Operation == configwatch.Remove) {
		return configwatch.Match{Dirty: true, RefreshWatches: true}
	}
	return configwatch.Match{}
}
