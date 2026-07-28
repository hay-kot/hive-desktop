package httpapi

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/invopop/jsonschema"

	"github.com/hay-kot/hive-desktop/internal/web"
)

// openAPISpecVersion is the OpenAPI Specification version the document declares
// conformance to. 3.2 is the newest the doc's structure and validator support;
// its Schema Object is JSON Schema 2020-12, which is exactly what the reflector
// emits, so reflected schemas embed unchanged.
const openAPISpecVersion = "3.2.0"

// buildOpenAPI serializes the operations table into an OpenAPI document. Body
// schemas are reflected from the Go request/response structs with the same
// reflector connector configs use, so the document cannot describe a field the
// struct lacks. The caller sets `servers`.
func buildOpenAPI(ops []Op) (map[string]any, error) {
	paths := map[string]any{}
	for _, op := range ops {
		item, ok := paths[op.Path].(map[string]any)
		if !ok {
			item = map[string]any{}
			paths[op.Path] = item
		}
		operation, err := openAPIOperation(op)
		if err != nil {
			return nil, fmt.Errorf("%s %s: %w", op.Method, op.Path, err)
		}
		item[strings.ToLower(op.Method)] = operation
	}

	return map[string]any{
		"openapi": openAPISpecVersion,
		"info": map[string]any{
			"title":       "Hive Desktop agent API",
			"version":     specVersion(),
			"description": "Loopback control surface over the running Hive Desktop app, served under " + PathPrefix + ". Every error shares the shape {kind, message, fields?}.",
		},
		"paths": paths,
	}, nil
}

func openAPIOperation(op Op) (map[string]any, error) {
	out := map[string]any{"summary": op.Summary}

	params := pathParameters(op.Path)
	if op.Query != nil {
		qp, err := queryParameters(op.Query)
		if err != nil {
			return nil, err
		}
		params = append(params, qp...)
	}
	if len(params) > 0 {
		out["parameters"] = params
	}

	if op.Request != nil {
		body, err := requestBody(op.Request)
		if err != nil {
			return nil, err
		}
		out["requestBody"] = body
	}

	responses, err := responsesFor(op)
	if err != nil {
		return nil, err
	}
	out["responses"] = responses
	return out, nil
}

func requestBody(req any) (map[string]any, error) {
	if rb, ok := req.(RawBinary); ok {
		body := map[string]any{"required": true, "content": binaryContent(rb.Media)}
		if rb.Note != "" {
			body["description"] = rb.Note
		}
		return body, nil
	}
	schema, err := reflectSchema(req)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"required": true,
		"content":  map[string]any{"application/json": map[string]any{"schema": schema}},
	}, nil
}

func responsesFor(op Op) (map[string]any, error) {
	status := strconv.Itoa(op.successStatus())
	responses := map[string]any{}

	switch resp := op.Response.(type) {
	case nil:
		responses[status] = map[string]any{"description": op.Summary}
	case RawBinary:
		responses[status] = map[string]any{"description": op.Summary, "content": binaryContent(resp.Media)}
	default:
		schema, err := reflectSchema(resp)
		if err != nil {
			return nil, err
		}
		responses[status] = map[string]any{
			"description": op.Summary,
			"content":     map[string]any{"application/json": map[string]any{"schema": schema}},
		}
	}

	errSchema, err := reflectSchema(web.ErrorBody{})
	if err != nil {
		return nil, err
	}
	errContent := map[string]any{"application/json": map[string]any{"schema": errSchema}}
	for _, e := range op.Errors {
		responses[strconv.Itoa(e.Status)] = map[string]any{"description": e.When, "content": errContent}
	}
	responses["default"] = map[string]any{
		"description": "Error response.",
		"content":     errContent,
	}
	return responses, nil
}

func binaryContent(media []string) map[string]any {
	content := map[string]any{}
	for _, m := range media {
		content[m] = map[string]any{"schema": map[string]any{"type": "string", "format": "binary"}}
	}
	return content
}

// reflectSchema reflects a Go value's JSON shape into a self-contained schema,
// mirroring connector.Schema so both surfaces read the same way.
func reflectSchema(v any) (any, error) {
	r := &jsonschema.Reflector{
		ExpandedStruct:             true,
		DoNotReference:             true,
		RequiredFromJSONSchemaTags: false,
	}
	schema := r.Reflect(v)
	schema.Version = ""

	raw, err := json.Marshal(schema)
	if err != nil {
		return nil, err
	}
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func pathParameters(path string) []any {
	var out []any
	for seg := range strings.SplitSeq(path, "/") {
		if len(seg) >= 2 && seg[0] == '{' && seg[len(seg)-1] == '}' {
			out = append(out, map[string]any{
				"name": seg[1 : len(seg)-1], "in": "path", "required": true,
				"schema": map[string]any{"type": "string"},
			})
		}
	}
	return out
}

func queryParameters(v any) ([]any, error) {
	t := reflect.TypeOf(v)
	if t.Kind() != reflect.Struct {
		return nil, fmt.Errorf("query type %T is not a struct", v)
	}
	var out []any
	for _, f := range reflect.VisibleFields(t) {
		name := f.Tag.Get("schema")
		if name == "" || name == "-" {
			continue
		}
		// Required-ness and prose live on struct tags because the actual rules
		// are imperative criterio in each Validate(); a conditional requirement
		// (profile is required only when feed is set) stays required:false and is
		// spelled out in the field's desc instead.
		param := map[string]any{
			"name": name, "in": "query",
			"required": f.Tag.Get("required") == "true",
			"schema":   map[string]any{"type": openAPIScalar(f.Type.Kind())},
		}
		if d := f.Tag.Get("desc"); d != "" {
			param["description"] = d
		}
		if ex := f.Tag.Get("example"); ex != "" {
			param["example"] = ex
		}
		out = append(out, param)
	}
	return out, nil
}

func openAPIScalar(k reflect.Kind) string {
	switch k {
	case reflect.Bool:
		return "boolean"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "integer"
	case reflect.Float32, reflect.Float64:
		return "number"
	default:
		return "string"
	}
}

func specVersion() string {
	b := web.ReadBuild()
	if b.Revision == "" {
		return "dev"
	}
	rev := b.Revision
	if len(rev) > 12 {
		rev = rev[:12]
	}
	if b.Modified {
		rev += "-dirty"
	}
	return rev
}
