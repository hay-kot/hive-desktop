// Package extractors decodes and validates request input: Body and Query
// decode into a typed struct and run its Validate method when it has one.
package extractors

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/gorilla/schema"
	"github.com/hay-kot/criterio"

	"github.com/hay-kot/hive-desktop/internal/web"
)

const maxBodyBytes = 1 << 20

// Validator is the optional contract a request struct implements to be
// validated after decoding.
type Validator interface {
	Validate() error
}

// Body decodes a JSON request body into T, capped at 1 MiB with unknown
// fields rejected, then validates it.
func Body[T any](w http.ResponseWriter, r *http.Request) (T, error) {
	var v T
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&v); err != nil {
		return v, &web.BadRequestError{Msg: "invalid request body", Err: err}
	}
	return v, validate(v)
}

var queryDecoder = newQueryDecoder()

func newQueryDecoder() *schema.Decoder {
	d := schema.NewDecoder()
	d.IgnoreUnknownKeys(true)
	return d
}

// Query decodes r's query string into T by `schema` tags, then validates it.
// Conversion failures surface as field errors on the 422 path.
func Query[T any](r *http.Request) (T, error) {
	var v T
	if err := queryDecoder.Decode(&v, r.URL.Query()); err != nil {
		var multi schema.MultiError
		if errors.As(err, &multi) {
			var b criterio.FieldErrorsBuilder
			for key, fieldErr := range multi {
				b = b.Append(key, fieldErr)
			}
			return v, b.ToError()
		}
		return v, err
	}
	return v, validate(v)
}

func validate(v any) error {
	if val, ok := v.(Validator); ok {
		return val.Validate()
	}
	return nil
}
