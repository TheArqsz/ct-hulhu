package runner

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type stringSlice []string

func (s *stringSlice) String() string { return strings.Join(*s, ",") }
func (s *stringSlice) Set(val string) error {
	for _, v := range strings.Split(val, ",") {
		v = strings.TrimSpace(v)
		if v != "" {
			*s = append(*s, v)
		}
	}
	return nil
}

type Options struct {
	Domain     stringSlice
	DomainFile string

	LogURL   stringSlice
	ListLogs bool
	LogState string

	Workers      int
	ParseWorkers int
	BatchSize    int
	RateLimit    int
	Timeout      int
	Retries      int
	Start        int64
	Count        int64
	FromEnd      bool

	Output  string
	JSON    bool
	Fields  string
	Silent  bool
	Verbose bool
	NoColor bool

	Monitor      bool
	PollInterval int

	Update             bool
	DisableUpdateCheck bool

	Resume   bool
	StateDir string
}

func ParseOptions() *Options {
	opts := &Options{}

	flag.Var(&opts.Domain, "d", "target domain(s) to filter (comma-separated, can be repeated)")
	flag.Var(&opts.Domain, "domain", "target domain(s) to filter (comma-separated, can be repeated)")
	flag.StringVar(&opts.DomainFile, "df", "", "file containing target domains (one per line)")

	flag.Var(&opts.LogURL, "lu", "CT log URL(s) to scrape (comma-separated, can be repeated)")
	flag.Var(&opts.LogURL, "log-url", "CT log URL(s) to scrape (comma-separated, can be repeated)")
	flag.BoolVar(&opts.ListLogs, "ls", false, "list available CT logs and exit")
	flag.BoolVar(&opts.ListLogs, "list-logs", false, "list available CT logs and exit")
	flag.StringVar(&opts.LogState, "log-state", "usable", "filter logs by state (usable/readonly/retired/qualified/all)")

	flag.IntVar(&opts.Workers, "w", 4, "number of concurrent fetch workers")
	flag.IntVar(&opts.Workers, "workers", 4, "number of concurrent fetch workers")
	flag.IntVar(&opts.ParseWorkers, "pw", 0, "number of concurrent parse workers (0 = auto)")
	flag.IntVar(&opts.ParseWorkers, "parse-workers", 0, "number of concurrent parse workers (0 = auto)")
	flag.IntVar(&opts.BatchSize, "bs", 256, "entries per batch request")
	flag.IntVar(&opts.BatchSize, "batch-size", 256, "entries per batch request")
	flag.IntVar(&opts.RateLimit, "rl", 0, "max requests per second (0 = unlimited)")
	flag.IntVar(&opts.RateLimit, "rate-limit", 0, "max requests per second (0 = unlimited)")
	flag.IntVar(&opts.Timeout, "to", 30, "HTTP request timeout in seconds")
	flag.IntVar(&opts.Timeout, "timeout", 30, "HTTP request timeout in seconds")
	flag.IntVar(&opts.Retries, "retries", 3, "number of retries per failed request")
	flag.Int64Var(&opts.Start, "start", -1, "start entry index (-1 = auto)")
	flag.Int64Var(&opts.Count, "n", 0, "number of entries to fetch (0 = all)")
	flag.Int64Var(&opts.Count, "count", 0, "number of entries to fetch (0 = all)")
	flag.BoolVar(&opts.FromEnd, "from-end", false, "start from newest entries")

	flag.StringVar(&opts.Output, "o", "", "output file path")
	flag.StringVar(&opts.Output, "output", "", "output file path")
	flag.BoolVar(&opts.JSON, "json", false, "JSON line output")
	flag.BoolVar(&opts.JSON, "j", false, "JSON line output")
	flag.StringVar(&opts.Fields, "f", "domains", "output fields (domains/ips/emails/certs/all)")
	flag.StringVar(&opts.Fields, "fields", "domains", "output fields (domains/ips/emails/certs/all)")
	flag.BoolVar(&opts.Silent, "s", false, "silent mode - only output results")
	flag.BoolVar(&opts.Silent, "silent", false, "silent mode - only output results")
	flag.BoolVar(&opts.Verbose, "v", false, "verbose output")
	flag.BoolVar(&opts.Verbose, "verbose", false, "verbose output")
	flag.BoolVar(&opts.NoColor, "nc", false, "disable color output")
	flag.BoolVar(&opts.NoColor, "no-color", false, "disable color output")

	flag.BoolVar(&opts.Monitor, "monitor", false, "continuous monitoring mode - watch for new entries")
	flag.BoolVar(&opts.Monitor, "m", false, "continuous monitoring mode - watch for new entries")
	flag.IntVar(&opts.PollInterval, "poll-interval", 10, "seconds between STH polls in monitor mode")
	flag.IntVar(&opts.PollInterval, "pi", 10, "seconds between STH polls in monitor mode")

	flag.BoolVar(&opts.Update, "up", false, "update ct-hulhu to latest version")
	flag.BoolVar(&opts.Update, "update", false, "update ct-hulhu to latest version")
	flag.BoolVar(&opts.DisableUpdateCheck, "duc", false, "disable automatic update check")
	flag.BoolVar(&opts.DisableUpdateCheck, "disable-update-check", false, "disable automatic update check")

	flag.BoolVar(&opts.Resume, "resume", false, "resume from last saved position")
	flag.StringVar(&opts.StateDir, "state-dir", defaultStateDir(), "directory for state files")

	flag.Usage = func() {
		showBanner()
		fmt.Fprintf(os.Stderr, "Usage:\n")
		fmt.Fprintf(os.Stderr, "  ct-hulhu [flags]\n\n")
		fmt.Fprintf(os.Stderr, "Examples:\n")
		fmt.Fprintf(os.Stderr, "  ct-hulhu -ls                                   	# List available CT logs\n")
		fmt.Fprintf(os.Stderr, "  ct-hulhu -d example.com -n 10000               	# Scrape 10k entries, filter for example.com\n")
		fmt.Fprintf(os.Stderr, "  ct-hulhu -lu <log-url> -d example.com           	# Scrape specific log\n")
		fmt.Fprintf(os.Stderr, "  ct-hulhu -lu <log-url> -from-end -n 5000 -json  	# Last 5k entries as JSON\n")
		fmt.Fprintf(os.Stderr, "  ct-hulhu -d example.com -silent | httpx          	# Pipeline to httpx\n")
		fmt.Fprintf(os.Stderr, "  echo example.com | ct-hulhu -silent              	# Domain from stdin\n")
		fmt.Fprintf(os.Stderr, "  ct-hulhu -m -d example.com -lu <log-url>         	# Monitor log for new certs\n")
		fmt.Fprintf(os.Stderr, "  ct-hulhu -m -d example.com -pi 5 -silent         	# Monitor, poll every 5s\n")
		fmt.Fprintf(os.Stderr, "  ct-hulhu -lu <log-url> -d example.com -resume    	# Resume interrupted scrape\n")
		printFlags()
	}

	flag.Parse()

	if flag.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "error: unrecognized arguments: %s\n", strings.Join(flag.Args(), " "))
		fmt.Fprintf(os.Stderr, "hint: all flags must appear before any positional arguments\n")
		fmt.Fprintf(os.Stderr, "      flags that take a value need it right after: -d example.com (not -d -silent)\n")
		os.Exit(1)
	}

	configureLogger(opts.Silent, opts.Verbose, opts.NoColor)
	opts.validate()

	return opts
}

func (o *Options) validate() {
	var errors []string

	if o.Workers < 1 || o.Workers > 128 {
		errors = append(errors, "-w/--workers must be between 1 and 128")
	}
	if o.ParseWorkers < 0 || o.ParseWorkers > 128 {
		errors = append(errors, "-pw/--parse-workers must be between 0 and 128")
	}
	if o.BatchSize < 1 || o.BatchSize > 10000 {
		errors = append(errors, "-bs/--batch-size must be between 1 and 10000")
	}
	if o.RateLimit < 0 {
		errors = append(errors, "-rl/--rate-limit must be >= 0")
	}
	if o.Timeout < 1 {
		errors = append(errors, "-to/--timeout must be >= 1")
	}
	if o.Retries < 0 || o.Retries > 10 {
		errors = append(errors, "--retries must be between 0 and 10")
	}
	if o.PollInterval < 1 {
		errors = append(errors, "-pi/--poll-interval must be >= 1")
	}

	validFields := map[string]bool{
		"domains": true, "ips": true, "emails": true, "certs": true, "all": true,
	}
	if !validFields[o.Fields] {
		errors = append(errors, fmt.Sprintf("-f/--fields must be one of: domains, ips, emails, certs, all (got %q)", o.Fields))
	}

	validStates := map[string]bool{
		"usable": true, "readonly": true, "qualified": true, "retired": true, "all": true,
	}
	if !validStates[o.LogState] {
		errors = append(errors, fmt.Sprintf("--log-state must be one of: usable, readonly, qualified, retired, all (got %q)", o.LogState))
	}

	if len(errors) > 0 {
		for _, e := range errors {
			fmt.Fprintf(os.Stderr, "error: %s\n", e)
		}
		os.Exit(1)
	}
}

func defaultStateDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".ct-hulhu"
	}
	return filepath.Join(home, ".ct-hulhu")
}

func printFlags() {
	w := os.Stderr
	fmt.Fprintf(w, "\nTARGET:\n")
	fmt.Fprintf(w, "  -d, -domain string[]        target domain(s) to filter (comma-separated)\n")
	fmt.Fprintf(w, "  -df string                  file containing target domains (one per line)\n")

	fmt.Fprintf(w, "\nLOG SELECTION:\n")
	fmt.Fprintf(w, "  -lu, -log-url string[]      CT log URL(s) to scrape\n")
	fmt.Fprintf(w, "  -ls, -list-logs             list available CT logs and exit\n")
	fmt.Fprintf(w, "  -log-state string           filter logs by state: usable/readonly/retired/qualified/all (default: usable)\n")

	fmt.Fprintf(w, "\nSCRAPING:\n")
	fmt.Fprintf(w, "  -w, -workers int            concurrent fetch workers (default: 4)\n")
	fmt.Fprintf(w, "  -pw, -parse-workers int     concurrent parse workers, 0 = auto (default: 0)\n")
	fmt.Fprintf(w, "  -bs, -batch-size int        entries per request (default: 256)\n")
	fmt.Fprintf(w, "  -rl, -rate-limit int        max requests/sec, 0 = unlimited (default: 0)\n")
	fmt.Fprintf(w, "  -to, -timeout int           HTTP timeout in seconds (default: 30)\n")
	fmt.Fprintf(w, "  -retries int                retries per failed request (default: 3)\n")
	fmt.Fprintf(w, "  -start int                  start entry index (default: auto)\n")
	fmt.Fprintf(w, "  -n, -count int              entries to fetch, 0 = all (default: 0)\n")
	fmt.Fprintf(w, "  -from-end                   start from newest entries\n")

	fmt.Fprintf(w, "\nMONITOR:\n")
	fmt.Fprintf(w, "  -m, -monitor                continuous monitoring mode\n")
	fmt.Fprintf(w, "  -pi, -poll-interval int     seconds between polls (default: 10)\n")

	fmt.Fprintf(w, "\nOUTPUT:\n")
	fmt.Fprintf(w, "  -o, -output string          output file path\n")
	fmt.Fprintf(w, "  -j, -json                   JSON line output\n")
	fmt.Fprintf(w, "  -f, -fields string          output fields: domains/ips/emails/certs/all (default: domains)\n")
	fmt.Fprintf(w, "  -s, -silent                 only output results (no banner, no progress)\n")
	fmt.Fprintf(w, "  -v, -verbose                verbose/debug output\n")
	fmt.Fprintf(w, "  -nc, -no-color              disable color output\n")

	fmt.Fprintf(w, "\nUPDATE:\n")
	fmt.Fprintf(w, "  -up, -update                update ct-hulhu to latest version\n")
	fmt.Fprintf(w, "  -duc, -disable-update-check disable automatic update check\n")

	fmt.Fprintf(w, "\nSTATE:\n")
	fmt.Fprintf(w, "  -resume                     resume from last saved position\n")
	fmt.Fprintf(w, "  -state-dir string           state file directory (default: ~/.ct-hulhu)\n")
}
