package runtime

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLiveHandler(t *testing.T) {
	recorder := httptest.NewRecorder()
	liveHandler(recorder, httptest.NewRequest(http.MethodGet, "/health/live", nil))
	if recorder.Code != http.StatusOK || recorder.Body.String() != "{\"status\":\"ok\"}\n" {
		t.Fatalf("unexpected response: %d %q", recorder.Code, recorder.Body.String())
	}
}
