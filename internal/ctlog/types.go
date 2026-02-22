package ctlog

import (
	"crypto/x509"
	"time"
)

type STH struct {
	TreeSize          int64  `json:"tree_size"`
	Timestamp         int64  `json:"timestamp"`
	SHA256RootHash    string `json:"sha256_root_hash"`
	TreeHeadSignature string `json:"tree_head_signature"`
}

type RawEntry struct {
	LeafInput string `json:"leaf_input"`
	ExtraData string `json:"extra_data"`
}

type GetEntriesResponse struct {
	Entries []RawEntry `json:"entries"`
}

type CertResult struct {
	Index      int64     `json:"index"`
	Timestamp  time.Time `json:"timestamp"`
	Domains    []string  `json:"domains"`
	IPs        []string  `json:"ips,omitempty"`
	Emails     []string  `json:"emails,omitempty"`
	CommonName string    `json:"common_name"`
	Issuer     string    `json:"issuer"`
	NotBefore  time.Time `json:"not_before"`
	NotAfter   time.Time `json:"not_after"`
	IsPrecert  bool      `json:"is_precert"`
	LogURL     string    `json:"log_url,omitempty"`
	Serial     string    `json:"serial,omitempty"`
}

type CertInfo struct {
	Cert      *x509.Certificate
	IsPrecert bool
	Index     int64
	Timestamp time.Time
}

type ScrapeProgress struct {
	LogURL      string    `json:"log_url"`
	TreeSize    int64     `json:"tree_size"`
	LastIndex   int64     `json:"last_index"`
	EntriesDone int64     `json:"entries_done"`
	LastUpdated time.Time `json:"last_updated"`
}
