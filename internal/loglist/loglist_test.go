package loglist

import "testing"

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
