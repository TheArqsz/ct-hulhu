package ctlog

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewWorkerPool(t *testing.T) {
	client := NewClient("https://example.com", 5*time.Second, 0)
	pool := NewWorkerPool(client, 256, 4, 0)

	if pool.client != client {
		t.Error("client not set")
	}
	if pool.batchSize != 256 {
		t.Errorf("batchSize = %d, want 256", pool.batchSize)
	}
	if pool.maxWorkers != 4 {
		t.Errorf("maxWorkers = %d, want 4", pool.maxWorkers)
	}
}

func TestDroppedEntries_Initial(t *testing.T) {
	client := NewClient("https://example.com", 5*time.Second, 0)
	pool := NewWorkerPool(client, 256, 4, 0)

	if pool.DroppedEntries() != 0 {
		t.Errorf("DroppedEntries() = %d, want 0", pool.DroppedEntries())
	}
}

func TestErrorInfo_NoRequests(t *testing.T) {
	client := NewClient("https://example.com", 5*time.Second, 0)
	pool := NewWorkerPool(client, 256, 4, 0)

	got := pool.ErrorInfo()
	if got != "no requests made" {
		t.Errorf("ErrorInfo() = %q, want 'no requests made'", got)
	}
}

func TestFetchRange_EmptyRange(t *testing.T) {
	client := NewClient("https://example.com", 5*time.Second, 0)
	pool := NewWorkerPool(client, 256, 1, 0)

	results := make(chan EntryBatch, 1)
	err := pool.FetchRange(context.Background(), 10, 10, results)
	if err != nil {
		t.Fatalf("expected nil error for empty range, got: %v", err)
	}

	_, ok := <-results
	if ok {
		t.Error("expected closed channel for empty range")
	}
}

func TestFetchRange_Basic(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Write([]byte(`{"entries":[{"leaf_input":"dGVzdA==","extra_data":""}]}`))
	}))
	defer srv.Close()

	client := NewClient(srv.URL, 5*time.Second, 0)
	pool := NewWorkerPool(client, 10, 1, 0)

	results := make(chan EntryBatch, 10)
	err := pool.FetchRange(context.Background(), 0, 5, results)
	if err != nil {
		t.Fatalf("FetchRange error: %v", err)
	}

	var batches []EntryBatch
	for b := range results {
		batches = append(batches, b)
	}
	if len(batches) == 0 {
		t.Fatal("expected at least one batch")
	}
}

func TestFetchRange_ContextCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.Write([]byte(`{"entries":[]}`))
	}))
	defer srv.Close()

	client := NewClient(srv.URL, 5*time.Second, 0)
	pool := NewWorkerPool(client, 10, 1, 0)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	results := make(chan EntryBatch, 10)
	err := pool.FetchRange(ctx, 0, 1000, results)
	if err == nil {
		t.Fatal("expected context cancellation error")
	}
}

func TestFetchRange_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := NewClient(srv.URL, 5*time.Second, 0)
	pool := NewWorkerPool(client, 10, 1, 0)

	results := make(chan EntryBatch, 10)
	err := pool.FetchRange(context.Background(), 0, 5, results)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if pool.DroppedEntries() == 0 {
		t.Error("expected dropped entries after server errors")
	}
}

func TestSetDebugLog(t *testing.T) {
	client := NewClient("https://example.com", 5*time.Second, 0)
	pool := NewWorkerPool(client, 256, 4, 0)

	var called bool
	pool.SetDebugLog(func(format string, args ...any) {
		called = true
	})
	pool.debug("test %d", 1)
	if !called {
		t.Error("debug log function was not called")
	}
}

func TestDebug_NilHandler(t *testing.T) {
	client := NewClient("https://example.com", 5*time.Second, 0)
	pool := NewWorkerPool(client, 256, 4, 0)
	pool.debug("test %d", 1)
}

func TestErrorInfo_WithStats(t *testing.T) {
	client := NewClient("https://example.com", 5*time.Second, 0)
	pool := NewWorkerPool(client, 256, 4, 0)
	pool.errCount.Add(2)
	pool.successCount.Add(8)

	got := pool.ErrorInfo()
	want := fmt.Sprintf("2 errors / 10 total requests (20.0%% error rate)")
	if got != want {
		t.Errorf("ErrorInfo() = %q, want %q", got, want)
	}
}

func TestFetchRange_RateLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"entries":[{"leaf_input":"dGVzdA==","extra_data":""}]}`))
	}))
	defer srv.Close()

	client := NewClient(srv.URL, 5*time.Second, 0)
	pool := NewWorkerPool(client, 10, 1, 100)

	results := make(chan EntryBatch, 10)
	err := pool.FetchRange(context.Background(), 0, 5, results)
	if err != nil {
		t.Fatalf("FetchRange with rate limit error: %v", err)
	}

	for range results {
	}
}
