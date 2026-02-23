package loglist

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestLogMatchesState(t *testing.T) {
	tests := []struct {
		state  LogState
		filter string
		want   bool
	}{
		{LogState{Usable: &StateInfo{}}, "usable", true},
		{LogState{Usable: &StateInfo{}}, "readonly", false},
		{LogState{Usable: &StateInfo{}}, "all", true},
		{LogState{ReadOnly: &ReadOnlyInfo{}}, "readonly", true},
		{LogState{ReadOnly: &ReadOnlyInfo{}}, "usable", false},
		{LogState{Retired: &StateInfo{}}, "retired", true},
		{LogState{Qualified: &StateInfo{}}, "qualified", true},
		{LogState{}, "usable", false},
		{LogState{}, "all", true},
	}

	for _, tt := range tests {
		log := Log{State: tt.state}
		got := log.MatchesState(tt.filter)
		if got != tt.want {
			t.Errorf("MatchesState(%+v, %q) = %v, want %v", tt.state, tt.filter, got, tt.want)
		}
	}
}

func TestLogCurrentState(t *testing.T) {
	tests := []struct {
		state LogState
		want  string
	}{
		{LogState{Usable: &StateInfo{}}, "usable"},
		{LogState{ReadOnly: &ReadOnlyInfo{}}, "readonly"},
		{LogState{Retired: &StateInfo{}}, "retired"},
		{LogState{Qualified: &StateInfo{}}, "qualified"},
		{LogState{Pending: &StateInfo{}}, "pending"},
		{LogState{Rejected: &StateInfo{}}, "rejected"},
		{LogState{}, "unknown"},
	}

	for _, tt := range tests {
		log := Log{State: tt.state}
		got := log.CurrentState()
		if got != tt.want {
			t.Errorf("CurrentState(%+v) = %q, want %q", tt.state, got, tt.want)
		}
	}
}

func TestLogFullURL(t *testing.T) {
	tests := []struct {
		url  string
		want string
	}{
		{"ct.googleapis.com/logs/us1/argon2025h1/", "https://ct.googleapis.com/logs/us1/argon2025h1/"},
		{"ct.googleapis.com/logs/us1/argon2025h1", "https://ct.googleapis.com/logs/us1/argon2025h1/"},
	}

	for _, tt := range tests {
		log := Log{URL: tt.url}
		got := log.FullURL()
		if got != tt.want {
			t.Errorf("FullURL(%q) = %q, want %q", tt.url, got, tt.want)
		}
	}
}

func TestFilterLogs(t *testing.T) {
	logList := &LogList{
		Operators: []Operator{
			{
				Name: "Google",
				Logs: []Log{
					{Description: "Argon", State: LogState{Usable: &StateInfo{}}},
					{Description: "Retired", State: LogState{Retired: &StateInfo{}}},
				},
			},
		},
	}

	usable := FilterLogs(logList, "usable")
	if len(usable) != 1 || usable[0].Log.Description != "Argon" {
		t.Errorf("expected 1 usable log (Argon), got %d", len(usable))
	}

	all := FilterLogs(logList, "all")
	if len(all) != 2 {
		t.Errorf("expected 2 logs for 'all', got %d", len(all))
	}

	retired := FilterLogs(logList, "retired")
	if len(retired) != 1 || retired[0].Log.Description != "Retired" {
		t.Errorf("expected 1 retired log, got %d", len(retired))
	}
}

func TestFetch(t *testing.T) {
	const logListJSON = `{
		"version": "3",
		"operators": [{
			"name": "TestOp",
			"email": ["test@example.com"],
			"logs": [{
				"description": "TestLog",
				"log_id": "abc123",
				"url": "ct.example.com/log/",
				"mmd": 86400,
				"state": {"usable": {"timestamp": "2025-01-01T00:00:00Z"}}
			}]
		}]
	}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(logListJSON))
	}))
	defer srv.Close()

	fetcher := NewFetcher(5 * time.Second)
	logList, err := fetcher.Fetch(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("Fetch error: %v", err)
	}
	if logList.Version != "3" {
		t.Errorf("Version = %q, want '3'", logList.Version)
	}
	if len(logList.Operators) != 1 {
		t.Fatalf("expected 1 operator, got %d", len(logList.Operators))
	}
	if logList.Operators[0].Name != "TestOp" {
		t.Errorf("operator name = %q", logList.Operators[0].Name)
	}
	if len(logList.Operators[0].Logs) != 1 {
		t.Fatalf("expected 1 log, got %d", len(logList.Operators[0].Logs))
	}
	if logList.Operators[0].Logs[0].Description != "TestLog" {
		t.Errorf("log description = %q", logList.Operators[0].Logs[0].Description)
	}
}

func TestFetch_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	fetcher := NewFetcher(5 * time.Second)
	_, err := fetcher.Fetch(context.Background(), srv.URL)
	if err == nil {
		t.Fatal("expected error for HTTP 500")
	}
}

func TestFetch_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not json"))
	}))
	defer srv.Close()

	fetcher := NewFetcher(5 * time.Second)
	_, err := fetcher.Fetch(context.Background(), srv.URL)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestNewFetcher(t *testing.T) {
	f := NewFetcher(10 * time.Second)
	if f.client == nil {
		t.Fatal("expected non-nil client")
	}
	if f.client.Timeout != 10*time.Second {
		t.Errorf("timeout = %v, want 10s", f.client.Timeout)
	}
}
