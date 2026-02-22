package output

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/TheArqsz/ct-hulhu/internal/ctlog"
)

func testResult(domains []string) *ctlog.CertResult {
	return &ctlog.CertResult{
		Index:      1,
		Timestamp:  time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		Domains:    domains,
		IPs:        []string{"1.2.3.4"},
		Emails:     []string{"admin@example.com"},
		CommonName: "example.com",
		Issuer:     "Test CA",
		NotBefore:  time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		NotAfter:   time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC),
		Serial:     "abc123",
		LogURL:     "https://ct.example.com/log/",
	}
}

func TestWriter_DomainDedup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.txt")
	w, err := NewWriter(path, false, "domains")
	if err != nil {
		t.Fatal(err)
	}

	r := testResult([]string{"example.com", "sub.example.com"})
	w.WriteResult(r)
	w.WriteResult(r)
	w.Close()

	data, _ := os.ReadFile(path)
	lines := nonEmptyLines(string(data))
	if len(lines) != 2 {
		t.Errorf("expected 2 deduplicated lines, got %d: %v", len(lines), lines)
	}
}

func TestWriter_IPOutput(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.txt")
	w, err := NewWriter(path, false, "ips")
	if err != nil {
		t.Fatal(err)
	}

	w.WriteResult(testResult([]string{"example.com"}))
	w.WriteResult(testResult([]string{"example.com"}))
	w.Close()

	data, _ := os.ReadFile(path)
	lines := nonEmptyLines(string(data))
	if len(lines) != 1 || lines[0] != "1.2.3.4" {
		t.Errorf("expected [1.2.3.4], got %v", lines)
	}
}

func TestWriter_EmailOutput(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.txt")
	w, err := NewWriter(path, false, "emails")
	if err != nil {
		t.Fatal(err)
	}

	w.WriteResult(testResult([]string{"example.com"}))
	w.Close()

	data, _ := os.ReadFile(path)
	lines := nonEmptyLines(string(data))
	if len(lines) != 1 || lines[0] != "admin@example.com" {
		t.Errorf("expected [admin@example.com], got %v", lines)
	}
}

func TestWriter_AllFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.txt")
	w, err := NewWriter(path, false, "all")
	if err != nil {
		t.Fatal(err)
	}

	w.WriteResult(testResult([]string{"example.com"}))
	w.Close()

	data, _ := os.ReadFile(path)
	lines := nonEmptyLines(string(data))
	// domains + ips + emails = 3
	if len(lines) != 3 {
		t.Errorf("expected 3 lines (domain+ip+email), got %d: %v", len(lines), lines)
	}
}

func TestWriter_CertLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.txt")
	w, err := NewWriter(path, false, "certs")
	if err != nil {
		t.Fatal(err)
	}

	w.WriteResult(testResult([]string{"example.com"}))
	w.Close()

	data, _ := os.ReadFile(path)
	content := string(data)
	if !strings.Contains(content, "issuer=Test CA") {
		t.Errorf("cert line missing issuer: %q", content)
	}
	if !strings.Contains(content, "2025-12-31") {
		t.Errorf("cert line missing date: %q", content)
	}
}

func TestWriter_JSONMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.json")
	w, err := NewWriter(path, true, "domains")
	if err != nil {
		t.Fatal(err)
	}

	w.WriteResult(testResult([]string{"example.com"}))
	w.WriteResult(testResult([]string{"example.com"}))
	w.Close()

	data, _ := os.ReadFile(path)
	lines := nonEmptyLines(string(data))
	if len(lines) != 1 {
		t.Errorf("expected 1 JSON line after dedup, got %d", len(lines))
	}

	var jr JSONResult
	if err := json.Unmarshal([]byte(lines[0]), &jr); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if jr.Serial != "abc123" {
		t.Errorf("Serial = %q, want %q", jr.Serial, "abc123")
	}
	if jr.Issuer != "Test CA" {
		t.Errorf("Issuer = %q, want %q", jr.Issuer, "Test CA")
	}
}

func TestWriter_JSONMode_EmptySerial(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.json")
	w, err := NewWriter(path, true, "domains")
	if err != nil {
		t.Fatal(err)
	}

	r1 := testResult([]string{"a.com"})
	r1.Serial = ""
	r1.Index = 100

	r2 := testResult([]string{"b.com"})
	r2.Serial = ""
	r2.Index = 200

	w.WriteResult(r1)
	w.WriteResult(r2)
	w.Close()

	data, _ := os.ReadFile(path)
	lines := nonEmptyLines(string(data))
	if len(lines) != 2 {
		t.Errorf("expected 2 JSON lines for different certs with empty serial, got %d", len(lines))
	}
}

func TestWriter_Stdout(t *testing.T) {
	w, err := NewWriter("", false, "domains")
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Errorf("Close() error: %v", err)
	}
}

func TestWriter_Stats(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.txt")
	w, err := NewWriter(path, false, "domains")
	if err != nil {
		t.Fatal(err)
	}

	if w.Stats() != 0 {
		t.Errorf("initial Stats() = %d, want 0", w.Stats())
	}

	w.WriteResult(testResult([]string{"a.com", "b.com"}))
	if w.Stats() != 2 {
		t.Errorf("Stats() = %d, want 2", w.Stats())
	}

	w.WriteResult(testResult([]string{"a.com"}))
	if w.Stats() != 2 {
		t.Errorf("Stats() after dedup = %d, want 2", w.Stats())
	}

	w.Close()
}

func TestWriter_FlushOnClose(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.txt")
	w, err := NewWriter(path, false, "domains")
	if err != nil {
		t.Fatal(err)
	}

	w.WriteResult(testResult([]string{"example.com"}))

	w.Close()

	data, _ := os.ReadFile(path)
	if !bytes.Contains(data, []byte("example.com")) {
		t.Error("expected flushed data after Close")
	}
}

func nonEmptyLines(s string) []string {
	var lines []string
	for _, line := range strings.Split(s, "\n") {
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}
