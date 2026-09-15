package agentws

import (
	"cmp"
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/hay-kot/hive-desktop/internal/app/configwatch"
)

type authority struct{ root string }

// NewAuthority describes authored workspace manifests and libraries.
func NewAuthority(root string) configwatch.Authority { return authority{root: filepath.Clean(root)} }

func (a authority) Topology(ctx context.Context) (configwatch.Topology, error) {
	if err := ctx.Err(); err != nil {
		return configwatch.Topology{}, err
	}
	topology := configwatch.Topology{Directories: []string{a.root}}
	entries, err := os.ReadDir(a.root)
	if err != nil {
		return configwatch.Topology{}, err
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return configwatch.Topology{}, err
		}
		path := filepath.Join(a.root, entry.Name())
		if !entry.IsDir() {
			if entry.Name() == libraryFileName || entry.Name() == skillLibraryFileName {
				topology.Files = append(topology.Files, configwatch.AuthorityFile{Key: entry.Name(), Path: path})
			}
			continue
		}
		if entry.Name() == sharedDirName {
			continue
		}
		topology.Directories = append(topology.Directories, path)
		manifest := filepath.Join(path, manifestFileName)
		if info, err := os.Stat(manifest); err == nil && !info.IsDir() {
			topology.Files = append(topology.Files, configwatch.AuthorityFile{Key: entry.Name() + "/" + manifestFileName, Path: manifest})
		} else if err != nil && !os.IsNotExist(err) {
			return configwatch.Topology{}, err
		}
	}
	slices.Sort(topology.Directories)
	slices.SortFunc(topology.Files, func(a, b configwatch.AuthorityFile) int { return cmp.Compare(a.Key, b.Key) })
	return topology, nil
}

func (a authority) Match(change configwatch.Change) configwatch.Match {
	path := filepath.Clean(change.Path)
	shared := filepath.Join(a.root, sharedDirName)
	if path == shared || strings.HasPrefix(path, shared+string(filepath.Separator)) {
		return configwatch.Match{}
	}
	if path == a.root && (change.Operation == configwatch.Rename || change.Operation == configwatch.Remove) {
		return configwatch.Match{Dirty: true, RefreshWatches: true}
	}
	if filepath.Dir(path) == a.root {
		name := filepath.Base(path)
		if name == libraryFileName || name == skillLibraryFileName {
			return configwatch.Match{Dirty: true}
		}
		if change.Operation == configwatch.Create || change.Operation == configwatch.Rename || change.Operation == configwatch.Remove {
			return configwatch.Match{Dirty: true, RefreshWatches: true}
		}
	}
	if filepath.Base(path) == manifestFileName && filepath.Dir(filepath.Dir(path)) == a.root {
		return configwatch.Match{Dirty: true}
	}
	return configwatch.Match{}
}
