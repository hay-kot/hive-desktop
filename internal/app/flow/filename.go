package flow

import (
	"path/filepath"
	"strings"
)

func isFlowYAMLFilename(name string) bool {
	ext := filepath.Ext(filepath.Base(name))
	return ext == ".yaml" || ext == ".yml"
}

func isFlowTempFilename(name string) bool {
	base := filepath.Base(name)
	return strings.HasSuffix(base, ".tmp.yaml") || strings.HasSuffix(base, ".tmp.yml")
}
