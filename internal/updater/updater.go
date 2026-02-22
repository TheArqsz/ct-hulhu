package updater

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	owner          = "TheArqsz"
	repo           = "ct-hulhu"
	module         = "github.com/TheArqsz/ct-hulhu/cmd/ct-hulhu"
	timeout        = 5 * time.Second
	maxAPIResponse = 2 << 20
	maxBinarySize  = 64 << 20
)

var httpClient = &http.Client{
	Timeout: 30 * time.Second,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return fmt.Errorf("too many redirects")
		}
		return nil
	},
}

type githubRelease struct {
	TagName string  `json:"tag_name"`
	Assets  []asset `json:"assets"`
}

type asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

func GetLatestVersion(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", owner, repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "ct-hulhu-updater")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub API returned %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxAPIResponse))
	if err != nil {
		return "", err
	}

	var release githubRelease
	if err := json.Unmarshal(body, &release); err != nil {
		return "", err
	}

	return strings.TrimPrefix(release.TagName, "v"), nil
}

func IsNewer(current, remote string) bool {
	cur := parseVersion(current)
	rem := parseVersion(remote)

	for i := 0; i < 3; i++ {
		if rem[i] > cur[i] {
			return true
		}
		if rem[i] < cur[i] {
			return false
		}
	}
	return false
}

func parseVersion(v string) [3]int {
	var result [3]int
	for i, p := range strings.SplitN(strings.TrimPrefix(v, "v"), ".", 3) {
		fmt.Sscanf(p, "%d", &result[i])
	}
	return result
}

func Update(ctx context.Context, currentVersion string) error {
	fmt.Fprintf(os.Stderr, "checking for updates...\n")

	latest, err := GetLatestVersion(ctx)
	if err != nil {
		return goInstallFallback()
	}

	if !IsNewer(currentVersion, latest) {
		fmt.Fprintf(os.Stderr, "already at latest version v%s\n", currentVersion)
		return nil
	}

	fmt.Fprintf(os.Stderr, "updating v%s -> v%s\n", currentVersion, latest)

	if err := downloadBinary(ctx, latest); err == nil {
		fmt.Fprintf(os.Stderr, "updated to v%s\n", latest)
		return nil
	}

	return goInstallFallback()
}

func downloadBinary(ctx context.Context, version string) error {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/tags/v%s", owner, repo, version)

	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "ct-hulhu-updater")

	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("release not found")
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxAPIResponse))
	if err != nil {
		return err
	}

	var release githubRelease
	if err := json.Unmarshal(body, &release); err != nil {
		return err
	}

	target := fmt.Sprintf("%s_%s", runtime.GOOS, runtime.GOARCH)
	var binaryAssetName string
	var downloadURL string
	var checksumURL string
	for _, a := range release.Assets {
		nameLower := strings.ToLower(a.Name)
		if strings.Contains(nameLower, target) {
			binaryAssetName = a.Name
			downloadURL = a.BrowserDownloadURL
		}
		if nameLower == "checksums.txt" {
			checksumURL = a.BrowserDownloadURL
		}
	}

	if downloadURL == "" {
		return fmt.Errorf("no binary for %s", target)
	}

	if checksumURL == "" {
		return fmt.Errorf("release missing checksums.txt, refusing to install unverified binary")
	}
	expectedHash, err := fetchExpectedChecksum(ctx, checksumURL, binaryAssetName)
	if err != nil {
		return fmt.Errorf("fetching checksums: %w", err)
	}

	binReq, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return err
	}

	binResp, err := httpClient.Do(binReq)
	if err != nil {
		return err
	}
	defer binResp.Body.Close()

	execPath, err := os.Executable()
	if err != nil {
		return err
	}

	hasher := sha256.New()
	limited := io.LimitReader(binResp.Body, maxBinarySize)
	archiveData, err := io.ReadAll(io.TeeReader(limited, hasher))
	if err != nil {
		return err
	}

	gotHash := hex.EncodeToString(hasher.Sum(nil))
	if gotHash != expectedHash {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", expectedHash, gotHash)
	}

	binaryName := "ct-hulhu"
	if runtime.GOOS == "windows" {
		binaryName = "ct-hulhu.exe"
	}

	var binaryData []byte
	if strings.HasSuffix(binaryAssetName, ".tar.gz") {
		binaryData, err = extractFromTarGz(archiveData, binaryName)
	} else if strings.HasSuffix(binaryAssetName, ".zip") {
		binaryData, err = extractFromZip(archiveData, binaryName)
	} else {
		return fmt.Errorf("unsupported archive format: %s", binaryAssetName)
	}
	if err != nil {
		return fmt.Errorf("extracting binary: %w", err)
	}

	tmpPath := execPath + ".tmp"
	if err := os.WriteFile(tmpPath, binaryData, 0o755); err != nil {
		return err
	}
	defer os.Remove(tmpPath)

	return os.Rename(tmpPath, execPath)
}

func extractFromTarGz(data []byte, binaryName string) ([]byte, error) {
	gr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer gr.Close()

	tr := tar.NewReader(gr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if filepath.Base(hdr.Name) == binaryName && hdr.Typeflag == tar.TypeReg {
			return io.ReadAll(io.LimitReader(tr, maxBinarySize))
		}
	}
	return nil, fmt.Errorf("binary %s not found in archive", binaryName)
}

func extractFromZip(data []byte, binaryName string) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}

	for _, f := range zr.File {
		if filepath.Base(f.Name) == binaryName && !f.FileInfo().IsDir() {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()
			return io.ReadAll(io.LimitReader(rc, maxBinarySize))
		}
	}
	return nil, fmt.Errorf("binary %s not found in archive", binaryName)
}

func fetchExpectedChecksum(ctx context.Context, checksumURL, assetName string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, checksumURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "ct-hulhu-updater")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("checksums file returned HTTP %d", resp.StatusCode)
	}

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 2 && fields[1] == assetName {
			return fields[0], nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}

	return "", fmt.Errorf("no checksum found for %s", assetName)
}

func goInstallFallback() error {
	goPath, err := exec.LookPath("go")
	if err != nil {
		return fmt.Errorf("go not found in PATH - install manually from GitHub releases")
	}

	fmt.Fprintf(os.Stderr, "downloading latest via go install...\n")

	cmd := exec.Command(goPath, "install", module+"@latest")
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("go install failed: %w", err)
	}

	fmt.Fprintf(os.Stderr, "updated successfully\n")
	return nil
}
