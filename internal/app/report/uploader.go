package report

import "context"

type Meta struct {
	ReportID string
	Version  string
	OS       string
	Arch     string
}

// Uploader ships a gzipped bundle to the ingest endpoint. It is a driven port:
// the concrete implementation and its endpoint live in the adapter.
type Uploader interface {
	Upload(ctx context.Context, gzipped []byte, meta Meta) error
}
