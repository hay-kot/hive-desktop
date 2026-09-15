package settings

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

// NewAuthority describes the settings file configuration detection reads.
func NewAuthority(path string) configwatch.Authority {
	cleaned := filepath.Clean(path)
	return authority{path: cleaned, dir: filepath.Dir(cleaned)}
}

func (a authority) Topology(ctx context.Context) (configwatch.Topology, error) {
	if err := ctx.Err(); err != nil {
		return configwatch.Topology{}, err
	}
	if _, err := os.ReadDir(a.dir); err != nil {
		return configwatch.Topology{}, err
	}
	return configwatch.Topology{
		Directories: []string{a.dir},
		Files:       []configwatch.AuthorityFile{{Key: filepath.Base(a.path), Path: a.path}},
	}, nil
}

func (a authority) Match(change configwatch.Change) configwatch.Match {
	path := filepath.Clean(change.Path)
	if path == a.path {
		return configwatch.Match{Dirty: true}
	}
	if path == a.dir && (change.Operation == configwatch.Rename || change.Operation == configwatch.Remove) {
		return configwatch.Match{Dirty: true, RefreshWatches: true}
	}
	return configwatch.Match{}
}
