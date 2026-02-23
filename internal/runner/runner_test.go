package runner

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/TheArqsz/ct-hulhu/internal/ctlog"
)

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

func TestReadLinesFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "domains.txt")
	content := "example.com\n# comment\n  sub.example.com  \n\nother.com\n"
	os.WriteFile(path, []byte(content), 0o644)

	lines, err := readLinesFromFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d: %v", len(lines), lines)
	}
	if lines[0] != "example.com" || lines[1] != "sub.example.com" || lines[2] != "other.com" {
		t.Errorf("unexpected lines: %v", lines)
	}
}

func TestReadLinesFromFile_NotFound(t *testing.T) {
	_, err := readLinesFromFile("/nonexistent/file.txt")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestLoadSaveProgress(t *testing.T) {
	dir := t.TempDir()
	r := &Runner{opts: &Options{StateDir: dir}}
	logURL := "https://ct.example.com/log/"

	p := r.loadProgress(logURL)
	if p != nil {
		t.Fatal("expected nil progress initially")
	}

	r.saveProgress(logURL, 100000, 50000, 50001)

	p = r.loadProgress(logURL)
	if p == nil {
		t.Fatal("expected non-nil progress after save")
	}
	if p.LogURL != logURL {
		t.Errorf("LogURL = %q, want %q", p.LogURL, logURL)
	}
	if p.TreeSize != 100000 {
		t.Errorf("TreeSize = %d, want 100000", p.TreeSize)
	}
	if p.LastIndex != 50000 {
		t.Errorf("LastIndex = %d, want 50000", p.LastIndex)
	}
	if p.EntriesDone != 50001 {
		t.Errorf("EntriesDone = %d, want 50001", p.EntriesDone)
	}
}

func TestLoadProgress_CorruptFile(t *testing.T) {
	dir := t.TempDir()
	r := &Runner{opts: &Options{StateDir: dir}}

	path := r.stateFilePath("https://ct.example.com/log/")
	os.WriteFile(path, []byte("not json"), 0o600)

	p := r.loadProgress("https://ct.example.com/log/")
	if p != nil {
		t.Error("expected nil for corrupt state file")
	}
}

func TestSaveProgress_CreatesDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "state")
	r := &Runner{opts: &Options{StateDir: dir}}

	r.saveProgress("https://ct.example.com/", 1000, 500, 501)

	data, err := os.ReadFile(r.stateFilePath("https://ct.example.com/"))
	if err != nil {
		t.Fatalf("expected state file to exist: %v", err)
	}

	var progress ctlog.ScrapeProgress
	if err := json.Unmarshal(data, &progress); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if progress.LastIndex != 500 {
		t.Errorf("LastIndex = %d, want 500", progress.LastIndex)
	}
}

func TestCollectDomains_FromOptions(t *testing.T) {
	configureLogger(true, false, true)
	r := &Runner{opts: &Options{Domain: stringSlice{"example.com", "other.com"}}}
	domains := r.collectDomains()
	if len(domains) != 2 {
		t.Fatalf("expected 2 domains, got %d", len(domains))
	}
}

func TestCollectDomains_FromFile(t *testing.T) {
	configureLogger(true, false, true)
	dir := t.TempDir()
	path := filepath.Join(dir, "domains.txt")
	os.WriteFile(path, []byte("file-domain.com\n"), 0o644)

	r := &Runner{opts: &Options{
		Domain:     stringSlice{"cli-domain.com"},
		DomainFile: path,
	}}
	domains := r.collectDomains()
	if len(domains) != 2 {
		t.Fatalf("expected 2 domains, got %d: %v", len(domains), domains)
	}
}

func TestStringSlice_Set(t *testing.T) {
	var s stringSlice
	s.Set("a.com, b.com, c.com")
	if len(s) != 3 {
		t.Fatalf("expected 3 values, got %d: %v", len(s), s)
	}
	if s[0] != "a.com" || s[1] != "b.com" || s[2] != "c.com" {
		t.Errorf("unexpected values: %v", s)
	}
}

func TestStringSlice_SetEmpty(t *testing.T) {
	var s stringSlice
	s.Set(",, ,")
	if len(s) != 0 {
		t.Errorf("expected 0 values for empty input, got %d: %v", len(s), s)
	}
}

func TestStringSlice_String(t *testing.T) {
	s := stringSlice{"a.com", "b.com"}
	if s.String() != "a.com,b.com" {
		t.Errorf("String() = %q", s.String())
	}
}
