package chp

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchCHPFrom(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"text":"测试文案"}}`))
	}))
	defer server.Close()

	text, err := fetchCHPFrom(server.Client(), server.URL)
	if err != nil {
		t.Fatalf("fetchCHPFrom returned error: %v", err)
	}
	if text != "测试文案" {
		t.Fatalf("unexpected text: %q", text)
	}
}

func TestFetchCHPFromEmptyPayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"text":"  "}}`))
	}))
	defer server.Close()

	if _, err := fetchCHPFrom(server.Client(), server.URL); err == nil {
		t.Fatalf("expected empty payload to fail")
	}
}

func TestFetchCHPFromStatusCode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()

	if _, err := fetchCHPFrom(server.Client(), server.URL); err == nil {
		t.Fatalf("expected bad status code to fail")
	}
}
