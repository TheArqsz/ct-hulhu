package certparser

import (
	"bytes"
	"strings"
	"testing"
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

func TestContainsFoldASCII_WorstCase(t *testing.T) {
	data := bytes.Repeat([]byte("A"), 1_000_000)

	patternStr := strings.Repeat("a", 249) + "b"
	pattern := []byte(patternStr)

	got := containsFoldASCII(data, pattern)
	if got != false {
		t.Errorf("expected false, got %v", got)
	}
}
