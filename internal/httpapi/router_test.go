package httpapi_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"calliopoc/internal/httpapi"
)

func TestRouter(t *testing.T) {
	t.Parallel()

	files := fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<h1>CallioPoc</h1>")}}
	router := httpapi.NewRouter(files)

	for _, tc := range []struct {
		name   string
		method string
		path   string
		status int
		body   string
	}{
		{name: "health", method: http.MethodGet, path: "/health", status: http.StatusOK, body: "{\"status\":\"ok\"}"},
		{name: "health method not allowed", method: http.MethodPost, path: "/health", status: http.StatusMethodNotAllowed},
		{name: "test page", method: http.MethodGet, path: "/", status: http.StatusOK, body: "CallioPoc"},
		{name: "missing page", method: http.MethodGet, path: "/missing", status: http.StatusNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(tc.method, tc.path, nil)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != tc.status {
				t.Errorf("status = %d, want %d", response.Code, tc.status)
			}
			if tc.body != "" && !strings.Contains(response.Body.String(), tc.body) {
				t.Errorf("body = %q, want to contain %q", response.Body.String(), tc.body)
			}
		})
	}
}
