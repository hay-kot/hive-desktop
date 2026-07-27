// Package webtools holds the transport-neutral HTTP helpers shared by every
// JSON API in this repo (the devserver control API and the app's httpapi
// adapter), so version, build identity, and JSON writing stay one shape across
// them. It knows nothing about app vocabulary — Kind-to-status mapping stays in
// the adapter that owns the vocabulary.
package webtools

import (
	"encoding/json"
	"net/http"
	"runtime/debug"
)

// WriteJSON writes payload as JSON with status.
func WriteJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

// Build is the VCS identity Go stamps into a binary. Revision is empty under
// -buildvcs=false, which reads as an unknown build.
type Build struct {
	Revision string `json:"revision"`
	Modified bool   `json:"modified"`
	Time     string `json:"time"`
	Go       string `json:"go"`
}

// ReadBuild reads the build identity from the running binary.
func ReadBuild() Build {
	var b Build
	if info, ok := debug.ReadBuildInfo(); ok {
		b.Go = info.GoVersion
		for _, s := range info.Settings {
			switch s.Key {
			case "vcs.revision":
				b.Revision = s.Value
			case "vcs.modified":
				b.Modified = s.Value == "true"
			case "vcs.time":
				b.Time = s.Value
			}
		}
	}
	return b
}

// VersionHandler serves {service, ...Build} so a caller can confirm which build
// is running.
func VersionHandler(service string) http.HandlerFunc {
	type resp struct {
		Service string `json:"service"`
		Build   Build  `json:"build"`
	}

	build := ReadBuild()

	return func(w http.ResponseWriter, _ *http.Request) {
		WriteJSON(w, http.StatusOK, resp{Service: service, Build: build})
	}
}
