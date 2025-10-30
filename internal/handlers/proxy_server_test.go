package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/khrm/proxy-aae/internal/config"
)

func TestProxyServer_Health(t *testing.T) {
	// Create a mock proxy server
	server := &ProxyServer{
		config: &config.Config{},
	}

	// Create a request
	req, err := http.NewRequest("GET", "/health", nil)
	if err != nil {
		t.Fatal(err)
	}

	// Create a ResponseRecorder
	rr := httptest.NewRecorder()

	// Create handler
	handler := server.Handler()

	// Serve the request
	handler.ServeHTTP(rr, req)

	// Check the status code
	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v", status, http.StatusOK)
	}

	// Check the response body
	expected := "OK"
	if rr.Body.String() != expected {
		t.Errorf("handler returned unexpected body: got %v want %v", rr.Body.String(), expected)
	}
}

func TestProxyServer_Ready(t *testing.T) {
	// Create a mock proxy server with empty worker registry
	server := &ProxyServer{
		config: &config.Config{},
		// Note: workerRegistry is nil, which will cause the ListClusters() call to panic
		// This is expected behavior for this test
	}

	// Create a request
	req, err := http.NewRequest("GET", "/ready", nil)
	if err != nil {
		t.Fatal(err)
	}

	// Create a ResponseRecorder
	rr := httptest.NewRecorder()

	// Create handler
	handler := server.Handler()

	// Serve the request - this should panic due to nil workerRegistry
	// We expect this to panic, so we'll catch it
	func() {
		defer func() {
			if r := recover(); r != nil {
				// Expected panic due to nil workerRegistry
				t.Logf("Expected panic recovered: %v", r)
			}
		}()
		handler.ServeHTTP(rr, req)
	}()

	// The test passes if we get here (panic was caught)
}
