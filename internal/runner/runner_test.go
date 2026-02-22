package runner

import "testing"

func TestCalculateRange(t *testing.T) {
	tests := []struct {
		name      string
		opts      Options
		treeSize  int64
		wantStart int64
		wantEnd   int64
	}{
		{
			name:      "defaults fetch entire tree",
			opts:      Options{Start: -1},
			treeSize:  100000,
			wantStart: 0,
			wantEnd:   100000,
		},
		{
			name:      "count limits entries",
			opts:      Options{Start: -1, Count: 500},
			treeSize:  100000,
			wantStart: 0,
			wantEnd:   500,
		},
		{
			name:      "explicit start",
			opts:      Options{Start: 5000, Count: 1000},
			treeSize:  100000,
			wantStart: 5000,
			wantEnd:   6000,
		},
		{
			name:      "count clamped to tree size",
			opts:      Options{Start: -1, Count: 200000},
			treeSize:  100000,
			wantStart: 0,
			wantEnd:   100000,
		},
		{
			name:      "from-end with count",
			opts:      Options{Start: -1, FromEnd: true, Count: 5000},
			treeSize:  100000,
			wantStart: 95000,
			wantEnd:   100000,
		},
		{
			name:      "from-end count exceeds tree",
			opts:      Options{Start: -1, FromEnd: true, Count: 200000},
			treeSize:  100000,
			wantStart: 0,
			wantEnd:   100000,
		},
		{
			name:      "from-end default 10k",
			opts:      Options{Start: -1, FromEnd: true},
			treeSize:  100000,
			wantStart: 90000,
			wantEnd:   100000,
		},
		{
			name:      "from-end default clamped on small tree",
			opts:      Options{Start: -1, FromEnd: true},
			treeSize:  500,
			wantStart: 0,
			wantEnd:   500,
		},
		{
			name:      "from-end with explicit start",
			opts:      Options{Start: 80000, FromEnd: true},
			treeSize:  100000,
			wantStart: 80000,
			wantEnd:   100000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &Runner{opts: &tt.opts}
			start, end := r.calculateRange(tt.treeSize)
			if start != tt.wantStart || end != tt.wantEnd {
				t.Errorf("calculateRange(%d) = (%d, %d), want (%d, %d)",
					tt.treeSize, start, end, tt.wantStart, tt.wantEnd)
			}
		})
	}
}

func TestStateFilePath(t *testing.T) {
	r := &Runner{opts: &Options{StateDir: "/tmp/ct-hulhu"}}

	path := r.stateFilePath("https://ct.googleapis.com/logs/us1/argon2025h1/")
	want := "/tmp/ct-hulhu/ct.googleapis.com_logs_us1_argon2025h1_.state.json"
	if path != want {
		t.Errorf("stateFilePath() = %q, want %q", path, want)
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		input  string
		maxLen int
		want   string
	}{
		{"short", 10, "short"},
		{"exactly10!", 10, "exactly10!"},
		{"this is way too long", 10, "this is .."},
		{"ab", 2, "ab"},
		{"abc", 1, "a"},
		{"abc", 0, ""},
		{"abc", 2, "ab"},
	}

	for _, tt := range tests {
		got := truncate(tt.input, tt.maxLen)
		if got != tt.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
		}
	}
}
