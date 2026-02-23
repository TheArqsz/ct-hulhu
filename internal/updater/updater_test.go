package updater

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseVersion(t *testing.T) {
	tests := []struct {
		input string
		want  [3]int
	}{
		{"1.2.3", [3]int{1, 2, 3}},
		{"v1.2.3", [3]int{1, 2, 3}},
		{"0.1.0", [3]int{0, 1, 0}},
		{"10.20.30", [3]int{10, 20, 30}},
		{"dev", [3]int{0, 0, 0}},
		{"", [3]int{0, 0, 0}},
		{"1.0", [3]int{1, 0, 0}},
		{"1", [3]int{1, 0, 0}},
		{"vv0.1.1", [3]int{0, 1, 1}},
	}

	for _, tt := range tests {
		got := parseVersion(tt.input)
		if got != tt.want {
			t.Errorf("parseVersion(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestIsNewer(t *testing.T) {
	tests := []struct {
		current string
		remote  string
		want    bool
	}{
		{"0.1.0", "0.2.0", true},
		{"0.1.0", "0.1.1", true},
		{"0.1.0", "1.0.0", true},
		{"0.2.0", "0.1.0", false},
		{"0.1.0", "0.1.0", false},
		{"1.0.0", "0.9.9", false},
		{"dev", "0.1.0", true},
		{"0.1.0", "dev", false},
	}

	for _, tt := range tests {
		got := IsNewer(tt.current, tt.remote)
		if got != tt.want {
			t.Errorf("IsNewer(%q, %q) = %v, want %v", tt.current, tt.remote, got, tt.want)
		}
	}
}

func TestFetchExpectedChecksum(t *testing.T) {
	const checksumBody = `abc123def456  ct-hulhu_1.0.0_linux_amd64.tar.gz
fedcba987654  ct-hulhu_1.0.0_darwin_arm64.tar.gz
`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(checksumBody))
	}))
	defer srv.Close()

	tests := []struct {
		asset   string
		want    string
		wantErr bool
	}{
		{"ct-hulhu_1.0.0_linux_amd64.tar.gz", "abc123def456", false},
		{"ct-hulhu_1.0.0_darwin_arm64.tar.gz", "fedcba987654", false},
		{"ct-hulhu_1.0.0_windows_amd64.zip", "", true},
	}

	for _, tt := range tests {
		got, err := fetchExpectedChecksum(context.Background(), srv.URL, tt.asset)
		if tt.wantErr {
			if err == nil {
				t.Errorf("fetchExpectedChecksum(%q) expected error, got %q", tt.asset, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("fetchExpectedChecksum(%q) unexpected error: %v", tt.asset, err)
			continue
		}
		if got != tt.want {
			t.Errorf("fetchExpectedChecksum(%q) = %q, want %q", tt.asset, got, tt.want)
		}
	}
}

func makeTarGz(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	for name, content := range files {
		hdr := &tar.Header{
			Name: name,
			Mode: 0o755,
			Size: int64(len(content)),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	tw.Close()
	gw.Close()
	return buf.Bytes()
}

func makeZip(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		fw, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fw.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	zw.Close()
	return buf.Bytes()
}

func TestExtractFromTarGz(t *testing.T) {
	payload := []byte("fake-binary-content")
	archive := makeTarGz(t, map[string][]byte{"ct-hulhu": payload})

	got, err := extractFromTarGz(archive, "ct-hulhu")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Errorf("got %q, want %q", got, payload)
	}
}

func TestExtractFromTarGzNestedPath(t *testing.T) {
	payload := []byte("nested-binary")
	archive := makeTarGz(t, map[string][]byte{"ct-hulhu_0.4.0_linux_amd64/ct-hulhu": payload})

	got, err := extractFromTarGz(archive, "ct-hulhu")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Errorf("got %q, want %q", got, payload)
	}
}

func TestExtractFromTarGzNotFound(t *testing.T) {
	archive := makeTarGz(t, map[string][]byte{"other-file": []byte("data")})

	_, err := extractFromTarGz(archive, "ct-hulhu")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestExtractFromZip(t *testing.T) {
	payload := []byte("fake-windows-binary")
	archive := makeZip(t, map[string][]byte{"ct-hulhu.exe": payload})

	got, err := extractFromZip(archive, "ct-hulhu.exe")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Errorf("got %q, want %q", got, payload)
	}
}

func TestExtractFromZipNotFound(t *testing.T) {
	archive := makeZip(t, map[string][]byte{"other.exe": []byte("data")})

	_, err := extractFromZip(archive, "ct-hulhu.exe")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestFetchExpectedChecksumServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	_, err := fetchExpectedChecksum(context.Background(), srv.URL, "anything.tar.gz")
	if err == nil {
		t.Error("expected error for HTTP 404, got nil")
	}
}

func TestIsNewer_DevVersion(t *testing.T) {
	if !IsNewer("dev", "0.0.1") {
		t.Error("dev should be older than 0.0.1")
	}
	if IsNewer("1.0.0", "1.0.0") {
		t.Error("same version should not be newer")
	}
}

func TestParseVersion_Edge(t *testing.T) {
	v := parseVersion("v1.2.3-rc1")
	if v[0] != 1 || v[1] != 2 {
		t.Errorf("expected major=1 minor=2, got %v", v)
	}
}
