package ctlog

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestGetSTH(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ct/v1/get-sth" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Write([]byte(`{"tree_size":123456,"timestamp":1700000000000}`))
	}))
	defer srv.Close()

	client := NewClient(srv.URL, 5*time.Second, 0)
	sth, err := client.GetSTH(context.Background())
	if err != nil {
		t.Fatalf("GetSTH() error: %v", err)
	}
	if sth.TreeSize != 123456 {
		t.Errorf("TreeSize = %d, want 123456", sth.TreeSize)
	}
}

func TestGetSTH_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	client := NewClient(srv.URL, 5*time.Second, 0)
	_, err := client.GetSTH(context.Background())
	if err == nil {
		t.Fatal("expected error for HTTP 503")
	}
}

func TestGetRawEntries(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ct/v1/get-entries" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		start := r.URL.Query().Get("start")
		end := r.URL.Query().Get("end")
		if start != "0" || end != "1" {
			t.Errorf("unexpected params: start=%s end=%s", start, end)
		}
		w.Write([]byte(`{"entries":[{"leaf_input":"dGVzdA==","extra_data":""}]}`))
	}))
	defer srv.Close()

	client := NewClient(srv.URL, 5*time.Second, 0)
	resp, err := client.GetRawEntries(context.Background(), 0, 1)
	if err != nil {
		t.Fatalf("GetRawEntries() error: %v", err)
	}
	if len(resp.Entries) != 1 {
		t.Errorf("got %d entries, want 1", len(resp.Entries))
	}
	if resp.Entries[0].LeafInput != "dGVzdA==" {
		t.Errorf("LeafInput = %q, want %q", resp.Entries[0].LeafInput, "dGVzdA==")
	}
}

func TestDoRequestWithRetry(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Write([]byte(`{"tree_size":100}`))
	}))
	defer srv.Close()

	client := NewClient(srv.URL, 5*time.Second, 3)
	sth, err := client.GetSTH(context.Background())
	if err != nil {
		t.Fatalf("expected success after retries, got: %v", err)
	}
	if sth.TreeSize != 100 {
		t.Errorf("TreeSize = %d, want 100", sth.TreeSize)
	}
	if attempts != 3 {
		t.Errorf("attempts = %d, want 3", attempts)
	}
}

func TestDoRequestWithRetry_AllFail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	client := NewClient(srv.URL, 5*time.Second, 1)
	_, err := client.GetSTH(context.Background())
	if err == nil {
		t.Fatal("expected error after all retries exhausted")
	}
}

func TestDoRequest_ContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	client := NewClient(srv.URL, 10*time.Second, 0)
	_, err := client.GetSTH(ctx)
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

func TestNewClient_TrailingSlash(t *testing.T) {
	c1 := NewClient("https://example.com/log", 5*time.Second, 0)
	if c1.baseURL != "https://example.com/log/" {
		t.Errorf("expected trailing slash, got %q", c1.baseURL)
	}

	c2 := NewClient("https://example.com/log/", 5*time.Second, 0)
	if c2.baseURL != "https://example.com/log/" {
		t.Errorf("expected unchanged URL, got %q", c2.baseURL)
	}
}

func TestDoRequest_ResponseSizeLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data := make([]byte, 1024)
		for i := range data {
			data[i] = 'A'
		}
		w.Write(data)
	}))
	defer srv.Close()

	client := NewClient(srv.URL, 5*time.Second, 0)
	body, err := client.doRequest(context.Background(), srv.URL+"/test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(body) != 1024 {
		t.Errorf("body length = %d, want 1024", len(body))
	}
}
