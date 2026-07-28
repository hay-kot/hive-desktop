package httpapi

import (
	"net/http"
	"strings"

	"github.com/hay-kot/httpkit/server"
)

const openAPIPath = "/api/openapi.json"

type apiIndex struct {
	Service string      `json:"service"`
	Version string      `json:"version"`
	OpenAPI string      `json:"openapi"`
	Routes  []routeInfo `json:"routes"`
}

type routeInfo struct {
	Method  string `json:"method"`
	Path    string `json:"path"`
	Summary string `json:"summary"`
	Request string `json:"request,omitempty"`
}

// APIIndex answers GET /api with the whole route table so an agent discovers
// the surface in one call instead of reverse-engineering the binary.
func (ctrl *Controller) APIIndex(w http.ResponseWriter, _ *http.Request) error {
	ops := ctrl.operations()
	routes := make([]routeInfo, 0, len(ops))
	for _, op := range ops {
		routes = append(routes, routeInfo{
			Method:  op.Method,
			Path:    op.Path,
			Summary: op.Summary,
			Request: requestNote(op.Request),
		})
	}
	return server.JSON(w, http.StatusOK, apiIndex{
		Service: "hive.desktop.api",
		Version: specVersion(),
		OpenAPI: openAPIPath,
		Routes:  routes,
	})
}

// OpenAPI answers GET /api/openapi.json, built from the same table, with
// servers set to the address the request arrived on so the document is usable
// as fetched.
func (ctrl *Controller) OpenAPI(w http.ResponseWriter, r *http.Request) error {
	doc, err := buildOpenAPI(ctrl.operations())
	if err != nil {
		return err
	}
	doc["servers"] = []any{map[string]any{"url": "http://" + r.Host}}
	return server.JSON(w, http.StatusOK, doc)
}

func requestNote(req any) string {
	switch v := req.(type) {
	case nil:
		return ""
	case RawBinary:
		if v.Note != "" {
			return v.Note
		}
		return "raw bytes (" + strings.Join(v.Media, ", ") + ")"
	default:
		return "application/json"
	}
}
