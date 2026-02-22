
<div align="center">
<img src="assets/ct-hulhu.png" alt="ct-hulhu logo" width="20%"/>
<h1>ct-hulhu</h1>

<p>Simple Certificate Transparency log parser built for recon.</p>
</div>

## Why?

Most subdomain enumeration tools query third-party services (crt.sh, certspotter API, etc.) for CT data. This works until:

- The service is down or rate-limited
- You don't want to reveal your target to third parties
- The service doesn't index the log you need
- You need real-time monitoring, not cached results

`ct-hulhu` talks directly to CT logs. You get raw, unfiltered access to certificate data the moment it's logged.

## Install

```bash
go install -v github.com/TheArqsz/ct-hulhu/cmd/ct-hulhu@latest
```

Or build from source:

```bash
git clone https://github.com/TheArqsz/ct-hulhu.git
cd ct-hulhu
make build
```

Zero external dependencies. The binary is a single static file.

## Usage

### List available CT logs

```bash
ct-hulhu -ls
ct-hulhu -ls -log-state all    # include retired/readonly logs
ct-hulhu -ls -json              # JSON output for scripting
```

### Scrape a specific log

```bash
# Last 10k entries from a log, filter for your target
ct-hulhu -lu https://ct.googleapis.com/logs/us1/argon2025h1/ -d example.com -from-end -n 10000

# First 50k entries, all domains, JSON output
ct-hulhu -lu https://ct.googleapis.com/logs/us1/argon2025h1/ -n 50000 -json
```

### Auto-discover logs

When you don't specify `-lu`, `ct-hulhu` fetches [Google's CT log list](https://www.gstatic.com/ct/log_list/v3/log_list.json) and scrapes all usable logs:

```bash
ct-hulhu -d example.com -n 1000
```

### Monitor mode

Watch CT logs for new certificates in real-time:

```bash
# Monitor a log, output new domains matching example.com
ct-hulhu -m -d example.com -lu https://ct.googleapis.com/logs/us1/argon2025h1/

# Poll every 5 seconds, silent mode for piping
ct-hulhu -m -d example.com -pi 5 -silent

# Feed new domains directly into your pipeline
ct-hulhu -m -d example.com -silent | httpx -silent | nuclei -t cves/
```

Monitor starts at the current tree position (no history replay) and polls `get-sth` for tree size changes. When new entries appear, only the delta is fetched and processed.

### Pipeline integration

ct-hulhu follows simple rule: data goes to stdout, everything else goes to stderr. Use `-silent` for clean piping.

```bash
# Subdomain enum -> HTTP probe -> vuln scan
ct-hulhu -d example.com -n 100000 -silent | sort -u | httpx -silent | nuclei -t cves/

# Domains from stdin
echo "example.com" | ct-hulhu -silent

# Domain list from file
ct-hulhu -df targets.txt -n 50000 -silent -o domains.txt

# JSON output with full cert metadata
ct-hulhu -lu <log-url> -d example.com -json -n 10000
```

### Resume interrupted scrapes

```bash
ct-hulhu -lu <log-url> -d example.com -resume
# Ctrl+C anytime, run the same command again to continue
```

State is saved to `~/.ct-hulhu/` per log URL.

## Flags

```
TARGET:
  -d,  -domain string[]        target domain(s) to filter (comma-separated)
  -df                          file containing target domains (one per line)

LOG SELECTION:
  -lu, -log-url string[]      CT log URL(s) to scrape
  -ls, -list-logs             list available CT logs and exit
       -log-state string      filter logs by state: usable/readonly/retired/qualified/all (default: usable)

SCRAPING:
  -w,  -workers int           concurrent fetch workers (default: 4)
  -pw, -parse-workers int     concurrent parse workers, 0 = auto (default: 0)
  -bs, -batch-size int        entries per request (default: 256)
  -rl, -rate-limit int        max requests/sec, 0 = unlimited (default: 0)
  -to, -timeout int           HTTP timeout in seconds (default: 30)
       -retries int           retries per failed request (default: 3)
       -start int             start entry index (default: auto)
  -n,  -count int             entries to fetch, 0 = all (default: 0)
       -from-end              start from newest entries

MONITOR:
  -m,  -monitor               continuous monitoring mode
  -pi, -poll-interval int     seconds between polls (default: 10)

OUTPUT:
  -o,  -output string         output file path
  -j,  -json                  JSON line output
  -f,  -fields string         output fields: domains/ips/emails/certs/all (default: domains)
  -s,  -silent                only output results (no banner, no progress)
  -v,  -verbose               verbose/debug output
  -nc, -no-color              disable color output

UPDATE:
  -up, -update                update ct-hulhu to latest version
  -duc, -disable-update-check disable automatic update check

STATE:
       -resume                resume from last saved position
       -state-dir string      state file directory (default: ~/.ct-hulhu)
```

## How it works

### CT log protocol ([RFC 6962](https://datatracker.ietf.org/doc/html/rfc6962))

CT logs are append-only Merkle trees of TLS certificates. Every publicly trusted CA must submit certificates before issuance ([Chrome CT policy](https://googlechrome.github.io/CertificateTransparency/ct_policy.html)). The protocol exposes two endpoints we care about:

- **`get-sth`** - Returns the [Signed Tree Head](https://datatracker.ietf.org/doc/html/rfc6962#section-4.3) (current tree size + root hash). This is how we know how many entries exist and detect new ones.
- **`get-entries?start=N&end=M`** - Returns raw log entries ([MerkleTreeLeaf](https://datatracker.ietf.org/doc/html/rfc6962#section-3.4) structures containing DER-encoded certificates).

### Scraping pipeline

1. Query `get-sth` to get the tree size
2. Generate batch ranges based on `-start`, `-n`, `-from-end`
3. Adaptive worker pool fetches batches concurrently (starts with 1 worker, ramps up based on error rate)
4. Each entry's `leaf_input` is decoded from base64, then the structure is parsed to extract the DER certificate
5. **Fast-path filtering**: if `-d` is set, raw DER bytes are scanned for the target domain string *before* full X.509 parsing. Domain names appear as ASCII in DER-encoded certs (per [RFC 5280 encoding rules](https://datatracker.ietf.org/doc/html/rfc5280#section-4.2.1.6)), so this is a cheap pre-filter that avoids expensive ASN.1 parsing on non-matching entries.
6. Full [X.509](https://datatracker.ietf.org/doc/html/rfc5280) parse extracts Subject CN, DNS SANs, IP SANs, email SANs, issuer, serial, validity
7. Domain matching supports exact, subdomain (`.example.com` matches `sub.example.com`) and wildcard certs (`*.example.com`)
8. Output writer deduplicates results and streams to stdout/file

### Adaptive concurrency

The worker pool starts with 1 goroutine and ramps up every `500ms` if the error rate stays below `10%`. This avoids hammering logs that are slow to respond while maximizing throughput on fast ones. On errors, workers back off exponentially.

### Monitor mode

Polls `get-sth` at a configurable interval. When the tree size grows, fetches only the new entries (the delta between old and new tree size). Deduplication persists across the entire monitoring session.

## Output formats

**Plain text** (default) - one domain per line, deduplicated:
```
sub.example.com
api.example.com
staging.example.com
```

**JSON lines** (`-json`) - full certificate metadata per line:
```json
{"domains":["sub.example.com","*.example.com"],"cn":"sub.example.com","issuer":"Let's Encrypt","not_before":"2025-01-01T00:00:00Z","not_after":"2025-04-01T00:00:00Z","serial":"abc123","is_precert":true,"log_url":"https://ct.googleapis.com/logs/us1/argon2025h1/","index":12345}
```

**Other field modes** (`-f`):
- `domains` - DNS names from CN + SANs (default)
- `ips` - IP addresses from SANs
- `emails` - email addresses from SANs
- `certs` - one-line cert summaries
- `all` - domains + IPs + emails combined

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for build instructions, project structure, conventions and development workflow.

## References

- [RFC 6962](https://datatracker.ietf.org/doc/html/rfc6962) - Certificate Transparency v1 (protocol implemented by ct-hulhu)
- [RFC 5280](https://datatracker.ietf.org/doc/html/rfc5280) - X.509 PKI certificate format
- [Chrome CT Policy](https://googlechrome.github.io/CertificateTransparency/ct_policy.html) - Browser-enforced logging requirements
- [Google CT Log List](https://www.gstatic.com/ct/log_list/v3/log_list.json) - Public log discovery endpoint (v3 schema)

## License

[MIT](LICENSE.md)
