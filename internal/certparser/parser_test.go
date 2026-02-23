package certparser

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/binary"
	"math/big"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/TheArqsz/ct-hulhu/internal/ctlog"
)

func TestMatchesDomain(t *testing.T) {
	tests := []struct {
		domain string
		filter string
		want   bool
	}{
		{"example.com", "example.com", true},

		{"sub.example.com", "example.com", true},
		{"deep.sub.example.com", "example.com", true},
		{"notexample.com", "example.com", false},

		{"*.example.com", "example.com", true},
		{"*.sub.example.com", "example.com", true},
		{"*.other.com", "example.com", false},

		{"example.com.evil.com", "example.com", false},
		{"test.com", "example.com", false},

		{"192.168.1.1", "192.168.1.1", true},
		{"192.168.1.1", "192.168.1.2", false},
	}

	for _, tt := range tests {
		got := matchesDomain(tt.domain, tt.filter)
		if got != tt.want {
			t.Errorf("matchesDomain(%q, %q) = %v, want %v", tt.domain, tt.filter, got, tt.want)
		}
	}
}

func TestNewParser_DomainNormalization(t *testing.T) {
	p := New([]string{"Example.COM", ".sub.Example.COM"})

	if len(p.domainFilter) != 2 {
		t.Fatalf("expected 2 filters, got %d", len(p.domainFilter))
	}
	if p.domainFilter[0] != "example.com" {
		t.Errorf("expected lowercase domain, got %q", p.domainFilter[0])
	}
	if p.domainFilter[1] != "sub.example.com" {
		t.Errorf("expected leading dot stripped, got %q", p.domainFilter[1])
	}

	if len(p.domainFilterBytes) != 2 {
		t.Fatalf("expected 2 byte filters, got %d", len(p.domainFilterBytes))
	}
	if string(p.domainFilterBytes[0]) != "example.com" {
		t.Errorf("expected byte filter to match string filter, got %q", string(p.domainFilterBytes[0]))
	}
	if string(p.domainFilterBytes[1]) != "sub.example.com" {
		t.Errorf("expected byte filter to match string filter, got %q", string(p.domainFilterBytes[1]))
	}
}

func TestRawBytesMatchDomain(t *testing.T) {
	p := New([]string{"example.com"})

	if !p.rawBytesMatchDomain([]byte("CN=test.example.com")) {
		t.Error("expected match for bytes containing domain")
	}
	if p.rawBytesMatchDomain([]byte("CN=test.other.com")) {
		t.Error("expected no match for unrelated bytes")
	}
}

func TestContainsFoldASCII(t *testing.T) {
	tests := []struct {
		name    string
		data    string
		pattern string
		want    bool
	}{
		{"exact match", "example.com", "example.com", true},
		{"subdomain match", "CN=test.example.com", "example.com", true},
		{"mixed case data", "CN=test.EXAMPLE.COM", "example.com", true},
		{"camel case data", "CN=test.Example.Com", "example.com", true},
		{"no match", "CN=test.other.com", "example.com", false},
		{"data shorter than pattern", "short", "longerpattern", false},
		{"empty data", "", "example.com", false},
		{"empty pattern", "anything", "", true},
		{"both empty", "", "", true},

		{"partial match at start", "examplx.com", "example.com", false},
		{"partial match rolls into full match", "exampl.example.com", "example.com", true},
		{"repeating characters", "eeeeexample.com", "example.com", true},
		{"case fold at sliding window boundary", "xAmPlE.cOm", "example.com", false},
		{"case fold sliding into match", "eXaMple.coexample.com", "example.com", true},
		{"single character match", "A", "a", true},
		{"single character mismatch", "B", "a", false},

		{"uppercase pattern lowercase data", "CN=test.example.com", "EXAMPLE.COM", true},
		{"uppercase both sides", "CN=test.EXAMPLE.COM", "EXAMPLE.COM", true},
		{"mixed case pattern", "CN=test.Example.Com", "Example.Com", true},
	}

	for _, tt := range tests {
		got := containsFoldASCII([]byte(tt.data), []byte(tt.pattern))
		if got != tt.want {
			t.Errorf("containsFoldASCII(%q, %q) = %v, want %v", tt.data, tt.pattern, got, tt.want)
		}
	}
}

func BenchmarkRawBytesMatchDomain(b *testing.B) {
	p := New([]string{"example.com", "test.org", "mysite.io"})
	data := []byte("some random DER data with CN=app.example.com embedded in it, plus extra padding bytes to simulate realistic leaf size")

	for b.Loop() {
		p.rawBytesMatchDomain(data)
	}
}

func makeMerkleLeaf(t *testing.T, entryType uint16, certDER []byte) string {
	t.Helper()
	var buf []byte
	buf = append(buf, 0)
	buf = append(buf, 0)
	ts := make([]byte, 8)
	binary.BigEndian.PutUint64(ts, uint64(time.Now().UnixMilli()))
	buf = append(buf, ts...)
	et := make([]byte, 2)
	binary.BigEndian.PutUint16(et, entryType)
	buf = append(buf, et...)

	if entryType == 0 {
		certLen := len(certDER)
		buf = append(buf, byte(certLen>>16), byte(certLen>>8), byte(certLen))
		buf = append(buf, certDER...)
	} else {
		buf = append(buf, make([]byte, 32)...)
		certLen := len(certDER)
		buf = append(buf, byte(certLen>>16), byte(certLen>>8), byte(certLen))
		buf = append(buf, certDER...)
	}
	return base64.StdEncoding.EncodeToString(buf)
}

func makeTestCert(t *testing.T, cn string, dnsNames []string, ips []net.IP, emails []string) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:   big.NewInt(12345),
		Subject:        pkix.Name{CommonName: cn},
		NotBefore:      time.Now().Add(-time.Hour),
		NotAfter:       time.Now().Add(time.Hour),
		DNSNames:       dnsNames,
		IPAddresses:    ips,
		EmailAddresses: emails,
		Issuer: pkix.Name{
			CommonName:   "Test CA",
			Organization: []string{"Test Org"},
		},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return der
}

func TestParseMerkleTreeLeaf_TooShort(t *testing.T) {
	p := New(nil)
	_, err := p.parseMerkleTreeLeaf([]byte{0, 0, 0})
	if err == nil {
		t.Fatal("expected error for short data")
	}
}

func TestParseMerkleTreeLeaf_UnknownEntryType(t *testing.T) {
	p := New(nil)
	data := make([]byte, 12)
	binary.BigEndian.PutUint16(data[10:], 99)
	_, err := p.parseMerkleTreeLeaf(data)
	if err == nil || !strings.Contains(err.Error(), "unknown entry type") {
		t.Fatalf("expected unknown entry type error, got: %v", err)
	}
}

func TestParseX509Entry_TooShort(t *testing.T) {
	p := New(nil)
	_, err := p.parseX509Entry([]byte{0, 0}, time.Now())
	if err == nil {
		t.Fatal("expected error for short x509 entry")
	}
}

func TestParseX509Entry_Truncated(t *testing.T) {
	p := New(nil)
	_, err := p.parseX509Entry([]byte{0, 3, 0xe8, 1, 2, 3, 4, 5}, time.Now())
	if err == nil {
		t.Fatal("expected error for truncated data")
	}
}

func TestParseX509Entry_InvalidDER(t *testing.T) {
	p := New(nil)
	info, err := p.parseX509Entry([]byte{0, 0, 5, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}, time.Now())
	if err != nil {
		t.Fatalf("expected nil error for unparseable cert, got: %v", err)
	}
	if info != nil {
		t.Error("expected nil for unparseable certificate")
	}
}

func TestParsePrecertEntry_TooShort(t *testing.T) {
	p := New(nil)
	_, err := p.parsePrecertEntry(make([]byte, 10), time.Now())
	if err == nil {
		t.Fatal("expected error for short precert entry")
	}
}

func TestParseEntry_X509(t *testing.T) {
	der := makeTestCert(t, "test.example.com",
		[]string{"test.example.com", "www.example.com"},
		[]net.IP{net.ParseIP("10.0.0.1")},
		[]string{"admin@example.com"})
	leaf := makeMerkleLeaf(t, 0, der)

	p := New(nil)
	result, err := p.ParseEntry(ctlog.RawEntry{LeafInput: leaf}, 42, "https://ct.example.com/")
	if err != nil {
		t.Fatalf("ParseEntry error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Index != 42 {
		t.Errorf("Index = %d, want 42", result.Index)
	}
	if result.LogURL != "https://ct.example.com/" {
		t.Errorf("LogURL = %q", result.LogURL)
	}
	if result.Serial == "" {
		t.Error("expected non-empty serial")
	}
	if result.Issuer == "" {
		t.Error("expected non-empty issuer")
	}
	if len(result.IPs) != 1 || result.IPs[0] != "10.0.0.1" {
		t.Errorf("IPs = %v, want [10.0.0.1]", result.IPs)
	}
	if len(result.Emails) != 1 || result.Emails[0] != "admin@example.com" {
		t.Errorf("Emails = %v", result.Emails)
	}
}

func TestParseEntry_Precert(t *testing.T) {
	der := makeTestCert(t, "precert.example.com", []string{"precert.example.com"}, nil, nil)
	leaf := makeMerkleLeaf(t, 1, der)

	p := New(nil)
	result, err := p.ParseEntry(ctlog.RawEntry{LeafInput: leaf}, 7, "https://log.example.com/")
	if err != nil {
		t.Fatalf("ParseEntry error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if !result.IsPrecert {
		t.Error("expected IsPrecert = true")
	}
}

func TestParseEntry_InvalidBase64(t *testing.T) {
	p := New(nil)
	_, err := p.ParseEntry(ctlog.RawEntry{LeafInput: "!!!invalid!!!"}, 0, "")
	if err == nil {
		t.Fatal("expected error for invalid base64")
	}
}

func TestParseEntry_DomainFilter_Match(t *testing.T) {
	der := makeTestCert(t, "app.example.com", []string{"app.example.com"}, nil, nil)
	leaf := makeMerkleLeaf(t, 0, der)

	p := New([]string{"example.com"})
	result, err := p.ParseEntry(ctlog.RawEntry{LeafInput: leaf}, 0, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected result for matching domain")
	}
}

func TestParseEntry_DomainFilter_NoMatch(t *testing.T) {
	der := makeTestCert(t, "app.other.com", []string{"app.other.com"}, nil, nil)
	leaf := makeMerkleLeaf(t, 0, der)

	p := New([]string{"example.com"})
	result, err := p.ParseEntry(ctlog.RawEntry{LeafInput: leaf}, 0, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Error("expected nil result for non-matching domain")
	}
}

func TestResultMatchesDomain(t *testing.T) {
	p := New([]string{"example.com"})

	tests := []struct {
		name   string
		result *ctlog.CertResult
		want   bool
	}{
		{"domain match", &ctlog.CertResult{Domains: []string{"sub.example.com"}}, true},
		{"no match", &ctlog.CertResult{Domains: []string{"other.com"}}, false},
		{"ip match", &ctlog.CertResult{IPs: []string{"example.com"}}, true},
		{"empty", &ctlog.CertResult{}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := p.resultMatchesDomain(tt.result)
			if got != tt.want {
				t.Errorf("resultMatchesDomain() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBuildResult_IssuerFallback(t *testing.T) {
	der := makeTestCert(t, "test.com", nil, nil, nil)
	cert, _ := x509.ParseCertificate(der)
	p := New(nil)
	info := &ctlog.CertInfo{Cert: cert, Index: 1, Timestamp: time.Now()}
	result := p.buildResult(info, "https://log/")
	if result.Issuer == "" {
		t.Error("expected non-empty issuer")
	}
}

func TestContainsFoldASCII_WorstCase(t *testing.T) {
	data := bytes.Repeat([]byte("A"), 1_000_000)

	patternStr := strings.Repeat("a", 249) + "b"
	pattern := []byte(patternStr)

	got := containsFoldASCII(data, pattern)
	if got != false {
		t.Errorf("expected false, got %v", got)
	}
}
