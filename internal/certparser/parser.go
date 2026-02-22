package certparser

import (
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/TheArqsz/ct-hulhu/internal/ctlog"
)

type Parser struct {
	domainFilter      []string
	domainFilterBytes [][]byte
}

func New(domains []string) *Parser {
	lower := make([]string, len(domains))
	lowerBytes := make([][]byte, len(domains))
	for i, d := range domains {
		lower[i] = strings.ToLower(strings.TrimPrefix(d, "."))
		lowerBytes[i] = []byte(lower[i])
	}
	return &Parser{
		domainFilter:      lower,
		domainFilterBytes: lowerBytes,
	}
}

func (p *Parser) ParseEntry(entry ctlog.RawEntry, index int64, logURL string) (*ctlog.CertResult, error) {
	leafBytes, err := base64.StdEncoding.DecodeString(entry.LeafInput)
	if err != nil {
		return nil, fmt.Errorf("decoding leaf_input: %w", err)
	}

	if len(p.domainFilter) > 0 && !p.rawBytesMatchDomain(leafBytes) {
		return nil, nil
	}

	certInfo, err := p.parseMerkleTreeLeaf(leafBytes)
	if err != nil {
		return nil, err
	}

	if certInfo == nil || certInfo.Cert == nil {
		return nil, nil
	}
	certInfo.Index = index

	result := p.buildResult(certInfo, logURL)

	if len(p.domainFilter) > 0 && !p.resultMatchesDomain(result) {
		return nil, nil
	}

	return result, nil
}

func (p *Parser) rawBytesMatchDomain(data []byte) bool {
	for _, domainBytes := range p.domainFilterBytes {
		if containsFoldASCII(data, domainBytes) {
			return true
		}
	}
	return false
}

func containsFoldASCII(data, pattern []byte) bool {
	n := len(pattern)
	if n == 0 {
		return true
	}
	if len(data) < n {
		return false
	}

	const primeRK uint32 = 16777619
	var hashPat, hashData, pow uint32 = 0, 0, 1

	limit := len(data) - n
	for i := range n {
		pow *= primeRK

		pb := pattern[i]
		if pb >= 'A' && pb <= 'Z' {
			pb += 0x20
		}
		hashPat = hashPat*primeRK + uint32(pb)

		db := data[i]
		if db >= 'A' && db <= 'Z' {
			db += 0x20
		}
		hashData = hashData*primeRK + uint32(db)
	}

	for i := 0; i <= limit; i++ {
		if hashData == hashPat {
			match := true
			for j := 0; j < n; j++ {
				db := data[i+j]
				if db >= 'A' && db <= 'Z' {
					db += 0x20
				}
				pb := pattern[j]
				if pb >= 'A' && pb <= 'Z' {
					pb += 0x20
				}
				if db != pb {
					match = false
					break
				}
			}
			if match {
				return true
			}
		}

		if i < limit {
			oldByte := data[i]
			if oldByte >= 'A' && oldByte <= 'Z' {
				oldByte += 0x20
			}
			newByte := data[i+n]
			if newByte >= 'A' && newByte <= 'Z' {
				newByte += 0x20
			}

			hashData *= primeRK
			hashData += uint32(newByte)
			hashData -= pow * uint32(oldByte)
		}
	}
	return false
}

// Version (1) | MerkleLeafType (1) | Timestamp (8) | LogEntryType (2) | Entry data...
func (p *Parser) parseMerkleTreeLeaf(data []byte) (*ctlog.CertInfo, error) {
	if len(data) < 12 {
		return nil, fmt.Errorf("leaf data too short: %d bytes", len(data))
	}

	timestamp := binary.BigEndian.Uint64(data[2:10])
	if timestamp > math.MaxInt64 {
		return nil, fmt.Errorf("timestamp overflow: %d", timestamp)
	}
	ts := time.UnixMilli(int64(timestamp))
	entryType := binary.BigEndian.Uint16(data[10:12])

	switch entryType {
	case 0: // x509_entry
		return p.parseX509Entry(data[12:], ts)
	case 1: // precert_entry
		return p.parsePrecertEntry(data[12:], ts)
	default:
		return nil, fmt.Errorf("unknown entry type: %d", entryType)
	}
}

func (p *Parser) parseX509Entry(data []byte, timestamp time.Time) (*ctlog.CertInfo, error) {
	if len(data) < 3 {
		return nil, fmt.Errorf("x509 entry data too short")
	}

	certLen := int(data[0])<<16 | int(data[1])<<8 | int(data[2])
	data = data[3:]

	if len(data) < certLen {
		return nil, fmt.Errorf("certificate data truncated: need %d, have %d", certLen, len(data))
	}

	cert, err := x509.ParseCertificate(data[:certLen])
	if err != nil {
		return nil, nil
	}

	return &ctlog.CertInfo{
		Cert:      cert,
		IsPrecert: false,
		Timestamp: timestamp,
	}, nil
}

func (p *Parser) parsePrecertEntry(data []byte, timestamp time.Time) (*ctlog.CertInfo, error) {
	if len(data) < 32+3 {
		return nil, fmt.Errorf("precert entry data too short")
	}

	// skip issuer_key_hash (32 bytes)
	data = data[32:]

	tbsLen := int(data[0])<<16 | int(data[1])<<8 | int(data[2])
	data = data[3:]

	if len(data) < tbsLen {
		return nil, fmt.Errorf("TBS certificate data truncated")
	}

	cert, err := x509.ParseCertificate(data[:tbsLen])
	if err != nil {
		return nil, nil
	}

	return &ctlog.CertInfo{
		Cert:      cert,
		IsPrecert: true,
		Timestamp: timestamp,
	}, nil
}

func (p *Parser) buildResult(info *ctlog.CertInfo, logURL string) *ctlog.CertResult {
	cert := info.Cert

	domainSet := make(map[string]struct{})
	if cert.Subject.CommonName != "" {
		domainSet[strings.ToLower(cert.Subject.CommonName)] = struct{}{}
	}
	for _, name := range cert.DNSNames {
		domainSet[strings.ToLower(name)] = struct{}{}
	}

	domains := make([]string, 0, len(domainSet))
	for d := range domainSet {
		domains = append(domains, d)
	}

	ips := make([]string, 0, len(cert.IPAddresses))
	for _, ip := range cert.IPAddresses {
		ips = append(ips, ip.String())
	}

	emails := make([]string, 0, len(cert.EmailAddresses))
	emails = append(emails, cert.EmailAddresses...)

	issuer := cert.Issuer.CommonName
	if issuer == "" && len(cert.Issuer.Organization) > 0 {
		issuer = cert.Issuer.Organization[0]
	}

	serial := ""
	if cert.SerialNumber != nil {
		serial = fmt.Sprintf("%x", cert.SerialNumber)
	}

	return &ctlog.CertResult{
		Index:      info.Index,
		Timestamp:  info.Timestamp,
		Domains:    domains,
		IPs:        ips,
		Emails:     emails,
		CommonName: cert.Subject.CommonName,
		Issuer:     issuer,
		NotBefore:  cert.NotBefore,
		NotAfter:   cert.NotAfter,
		IsPrecert:  info.IsPrecert,
		LogURL:     logURL,
		Serial:     serial,
	}
}

func (p *Parser) resultMatchesDomain(result *ctlog.CertResult) bool {
	for _, domain := range result.Domains {
		for _, filter := range p.domainFilter {
			if matchesDomain(domain, filter) {
				return true
			}
		}
	}
	for _, ip := range result.IPs {
		for _, filter := range p.domainFilter {
			if ip == filter {
				return true
			}
		}
	}
	return false
}

func matchesDomain(domain, filter string) bool {
	if domain == filter {
		return true
	}

	if strings.HasSuffix(domain, "."+filter) {
		return true
	}

	if strings.HasPrefix(domain, "*.") {
		baseDomain := domain[2:]
		if baseDomain == filter || strings.HasSuffix(baseDomain, "."+filter) {
			return true
		}
	}

	return false
}
