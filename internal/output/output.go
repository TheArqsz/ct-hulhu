package output

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/TheArqsz/ct-hulhu/internal/ctlog"
)

type Writer struct {
	mu       sync.Mutex
	bw       *bufio.Writer
	closer   io.Closer
	jsonMode bool
	fields   string
	seen     map[string]struct{}
}

func NewWriter(outputPath string, jsonMode bool, fields string) (*Writer, error) {
	w := &Writer{
		jsonMode: jsonMode,
		fields:   fields,
		seen:     make(map[string]struct{}),
	}

	if outputPath != "" {
		f, err := os.Create(outputPath)
		if err != nil {
			return nil, fmt.Errorf("creating output file: %w", err)
		}
		w.bw = bufio.NewWriter(io.MultiWriter(f, os.Stdout))
		w.closer = f
	} else {
		w.bw = bufio.NewWriter(os.Stdout)
	}

	return w, nil
}

func (w *Writer) WriteResult(result *ctlog.CertResult) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.jsonMode {
		w.writeJSON(result)
		return
	}

	switch w.fields {
	case "domains":
		w.writeDomains(result)
	case "ips":
		w.writeIPs(result)
	case "emails":
		w.writeEmails(result)
	case "certs":
		w.writeCertLine(result)
	case "all":
		w.writeDomains(result)
		w.writeIPs(result)
		w.writeEmails(result)
	default:
		w.writeDomains(result)
	}
}

func (w *Writer) writeUnique(prefix string, items []string, sanitize bool) {
	for _, item := range items {
		key := prefix + item
		if _, exists := w.seen[key]; exists {
			continue
		}
		w.seen[key] = struct{}{}
		if sanitize {
			item = Sanitize(item)
		}
		fmt.Fprintln(w.bw, item)
	}
}

func (w *Writer) writeDomains(result *ctlog.CertResult) { w.writeUnique("d:", result.Domains, true) }
func (w *Writer) writeIPs(result *ctlog.CertResult)     { w.writeUnique("i:", result.IPs, false) }
func (w *Writer) writeEmails(result *ctlog.CertResult)   { w.writeUnique("e:", result.Emails, true) }

func (w *Writer) writeCertLine(result *ctlog.CertResult) {
	key := fmt.Sprintf("c:%s:%d", result.LogURL, result.Index)
	if _, exists := w.seen[key]; exists {
		return
	}
	w.seen[key] = struct{}{}

	domains := Sanitize(strings.Join(result.Domains, ","))
	fmt.Fprintf(w.bw, "[%s] %s issuer=%s domains=%s\n",
		result.NotAfter.Format("2006-01-02"),
		Sanitize(result.CommonName),
		Sanitize(result.Issuer),
		domains,
	)
}

type JSONResult struct {
	Domains    []string `json:"domains,omitempty"`
	IPs        []string `json:"ips,omitempty"`
	Emails     []string `json:"emails,omitempty"`
	CommonName string   `json:"cn,omitempty"`
	Issuer     string   `json:"issuer,omitempty"`
	NotBefore  string   `json:"not_before,omitempty"`
	NotAfter   string   `json:"not_after,omitempty"`
	Serial     string   `json:"serial,omitempty"`
	IsPrecert  bool     `json:"is_precert"`
	LogURL     string   `json:"log_url,omitempty"`
	Index      int64    `json:"index"`
}

func (w *Writer) writeJSON(result *ctlog.CertResult) {
	id := result.Serial
	if id == "" {
		id = fmt.Sprintf("idx:%d", result.Index)
	}
	key := fmt.Sprintf("j:%s:%s", id, result.LogURL)
	if _, exists := w.seen[key]; exists {
		return
	}
	w.seen[key] = struct{}{}

	jr := JSONResult{
		Domains:    sanitizeSlice(result.Domains),
		IPs:        result.IPs,
		Emails:     sanitizeSlice(result.Emails),
		CommonName: Sanitize(result.CommonName),
		Issuer:     Sanitize(result.Issuer),
		NotBefore:  result.NotBefore.Format("2006-01-02T15:04:05Z"),
		NotAfter:   result.NotAfter.Format("2006-01-02T15:04:05Z"),
		Serial:     result.Serial,
		IsPrecert:  result.IsPrecert,
		LogURL:     result.LogURL,
		Index:      result.Index,
	}

	data, err := json.Marshal(jr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[ERR] json marshal: %v\n", err)
		return
	}
	fmt.Fprintln(w.bw, string(data))
}

func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.bw.Flush(); err != nil {
		return err
	}
	if w.closer != nil {
		return w.closer.Close()
	}
	return nil
}

func (w *Writer) Flush() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.bw.Flush()
}

func (w *Writer) Stats() (total int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.seen)
}

func sanitizeSlice(ss []string) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = Sanitize(s)
	}
	return out
}

func Sanitize(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	i := 0
	for i < len(s) {
		if s[i] == 0x1b { // ESC
			i++
			if i >= len(s) {
				continue
			}
			switch s[i] {
			case '[': // CSI sequence: ESC [ params final_byte
				i++
				for i < len(s) && s[i] >= 0x20 && s[i] <= 0x3f {
					i++
				}
				if i < len(s) {
					i++
				}
			case ']': // OSC sequence: ESC ] ... (BEL | ESC \)
				i++
				for i < len(s) && s[i] != 0x07 {
					if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '\\' {
						i += 2
						break
					}
					i++
				}
				if i < len(s) && s[i] == 0x07 {
					i++
				}
			case 'P', 'X', '^', '_': // DCS, SOS, PM, APC: ESC x ... ST (ESC \)
				i++
				for i < len(s) {
					if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '\\' {
						i += 2
						break
					}
					i++
				}
			}
			continue
		}
		if s[i] < 0x20 {
			i++
			continue
		}
		if s[i] == 0x7f { // DEL
			i++
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}
