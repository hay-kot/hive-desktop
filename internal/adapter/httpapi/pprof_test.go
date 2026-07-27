package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPprofHandlerServesProfiles(t *testing.T) {
	handler := PprofHandler()

	for _, path := range []string{"/debug/pprof/", "/debug/pprof/goroutine?debug=1"} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		assert.Equal(t, http.StatusOK, rec.Code, "%s serves", path)
	}
}
