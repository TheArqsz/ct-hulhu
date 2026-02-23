package runner

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/TheArqsz/ct-hulhu/internal/certparser"
	"github.com/TheArqsz/ct-hulhu/internal/ctlog"
	"github.com/TheArqsz/ct-hulhu/internal/loglist"
	"github.com/TheArqsz/ct-hulhu/internal/output"
	"github.com/TheArqsz/ct-hulhu/internal/updater"
)

type Runner struct {
	opts *Options
}

func New(opts *Options) *Runner {
	return &Runner{opts: opts}
}

func (r *Runner) Run() error {
	if !r.opts.Silent {
		showBanner()
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt)
	go func() {
		<-sigChan
		log.Warning("interrupt received, shutting down gracefully...")
		cancel()
	}()

	if r.opts.Update {
		return updater.Update(ctx, getVersion())
	}

	if !r.opts.DisableUpdateCheck && !r.opts.Silent {
		go func() {
			latest, err := updater.GetLatestVersion(ctx)
			if err != nil {
				return
			}
			if updater.IsNewer(getVersion(), latest) {
				log.Info("new version available: %s (current: %s) - run with -up to update", latest, getVersion())
			}
		}()
	}

	if r.opts.ListLogs {
		return r.listLogs(ctx)
	}
	if r.opts.Monitor {
		return r.monitor(ctx)
	}
	return r.scrape(ctx)
}

func (r *Runner) listLogs(ctx context.Context) error {
	timeout := time.Duration(r.opts.Timeout) * time.Second
	fetcher := loglist.NewFetcher(timeout)

	log.Info("fetching CT log list...")

	logList, err := fetcher.FetchDefault(ctx)
	if err != nil {
		return fmt.Errorf("fetching log list: %w", err)
	}

	logs := loglist.FilterLogs(logList, r.opts.LogState)

	if r.opts.JSON {
		for _, l := range logs {
			data, err := json.Marshal(map[string]any{
				"operator":    output.Sanitize(l.Operator),
				"description": output.Sanitize(l.Log.Description),
				"url":         l.Log.FullURL(),
				"state":       l.Log.CurrentState(),
				"mmd":         l.Log.MMD,
			})
			if err != nil {
				log.Debug("json marshal error: %v", err)
				continue
			}
			fmt.Println(string(data))
		}
	} else {
		fmt.Printf("%-12s %-50s %-45s %s\n", "STATE", "DESCRIPTION", "URL", "OPERATOR")
		fmt.Println(strings.Repeat("-", 140))
		for _, l := range logs {
			fmt.Printf("%-12s %-50s %-45s %s\n",
				l.Log.CurrentState(),
				truncate(output.Sanitize(l.Log.Description), 48),
				truncate(l.Log.FullURL(), 43),
				output.Sanitize(l.Operator),
			)
		}
		fmt.Printf("\nTotal: %d logs\n", len(logs))
	}

	return nil
}

func (r *Runner) scrape(ctx context.Context) error {
	domains := r.collectDomains()

	logURLs, err := r.resolveLogURLs(ctx)
	if err != nil {
		return err
	}
	if len(logURLs) == 0 {
		return fmt.Errorf("no CT logs to scrape - use -lu <url> to specify a log or omit to auto-discover")
	}

	writer, err := output.NewWriter(r.opts.Output, r.opts.JSON, r.opts.Fields)
	if err != nil {
		return err
	}
	defer writer.Close()

	parser := certparser.New(domains)

	if len(domains) > 0 {
		log.Info("filtering for domains: %s", strings.Join(domains, ", "))
	}

	for _, logURL := range logURLs {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := r.scrapeLog(ctx, logURL, parser, writer); err != nil {
			log.Warning("error scraping %s: %v", logURL, err)
			continue
		}
	}

	log.Success("done - %d unique results written", writer.Stats())
	return nil
}

func (r *Runner) scrapeLog(ctx context.Context, logURL string, parser *certparser.Parser, writer *output.Writer) error {
	timeout := time.Duration(r.opts.Timeout) * time.Second
	client := ctlog.NewClient(logURL, timeout, r.opts.Retries)

	log.Info("connecting to %s", logURL)

	sth, err := client.GetSTH(ctx)
	if err != nil {
		return fmt.Errorf("getting STH: %w", err)
	}

	treeSize := sth.TreeSize
	log.Info("tree size: %d entries", treeSize)

	start, end := r.calculateRange(treeSize)
	if start >= end {
		log.Info("no entries to process")
		return nil
	}

	totalEntries := end - start

	if r.opts.Resume {
		progress := r.loadProgress(logURL)
		if progress != nil && progress.LastIndex > start {
			start = progress.LastIndex + 1
			if start >= end {
				log.Info("resume: all entries already processed for this log")
				return nil
			}
			totalEntries = end - start
			log.Info("resuming from entry %d (%d entries remaining)", start, totalEntries)
		} else {
			log.Info("resume: no saved state found for this log, starting fresh")
		}
	}

	log.Info("scraping entries %d to %d (%d entries) with %d workers",
		start, end-1, totalEntries, r.opts.Workers)

	pool := ctlog.NewWorkerPool(client, r.opts.BatchSize, r.opts.Workers, r.opts.RateLimit)
	pool.SetDebugLog(log.Debug)
	results := make(chan ctlog.EntryBatch, r.opts.Workers*2)

	var processed atomic.Int64
	startTime := time.Now()

	stopProgress := make(chan struct{})
	progressDone := make(chan struct{})
	go func() {
		defer close(progressDone)
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-stopProgress:
				return
			case <-ticker.C:
				done := processed.Load()
				if done == 0 {
					continue
				}
				elapsed := time.Since(startTime)
				rate := float64(done) / elapsed.Seconds()
				pct := float64(done) / float64(totalEntries) * 100
				log.Info("progress: %d/%d (%.1f%%) - %.0f entries/sec - %d results",
					done, totalEntries, pct, rate, writer.Stats())
			}
		}
	}()

	fetchErr := make(chan error, 1)
	go func() {
		fetchErr <- pool.FetchRange(ctx, start, end, results)
	}()

	parseSem := r.newParseSem()

	var lastSaveCount int64
	for batch := range results {
		r.parseBatch(batch, parser, writer, logURL, parseSem, &processed)
		writer.Flush()

		if r.opts.Resume {
			current := processed.Load()
			if current-lastSaveCount >= 10000 {
				lastIdx := batch.StartIndex + int64(len(batch.Entries)) - 1
				r.saveProgress(logURL, treeSize, lastIdx, current)
				lastSaveCount = current
			}
		}
	}

	close(stopProgress)
	<-progressDone

	writer.Flush()

	if r.opts.Resume {
		r.saveProgress(logURL, treeSize, end-1, processed.Load())
		log.Info("resume state saved to %s", r.stateFilePath(logURL))
	}

	if err := <-fetchErr; err != nil {
		return err
	}

	elapsed := time.Since(startTime)
	done := processed.Load()
	rate := float64(done) / elapsed.Seconds()
	log.Success("completed %s: %d entries in %v (%.0f entries/sec)",
		logURL, done, elapsed.Round(time.Second), rate)
	if dropped := pool.DroppedEntries(); dropped > 0 {
		log.Warning("dropped %d entries due to fetch errors (%.1f%% of requested range)",
			dropped, float64(dropped)/float64(totalEntries)*100)
	}
	log.Debug("fetch stats: %s", pool.ErrorInfo())

	return nil
}

func (r *Runner) monitor(ctx context.Context) error {
	domains := r.collectDomains()

	logURLs, err := r.resolveLogURLs(ctx)
	if err != nil {
		return err
	}
	if len(logURLs) == 0 {
		return fmt.Errorf("no CT logs to monitor - use -lu <url> to specify a log or omit to auto-discover")
	}

	writer, err := output.NewWriter(r.opts.Output, r.opts.JSON, r.opts.Fields)
	if err != nil {
		return err
	}
	defer writer.Close()

	parser := certparser.New(domains)

	if len(domains) > 0 {
		log.Info("monitoring for domains: %s", strings.Join(domains, ", "))
	}

	pollInterval := time.Duration(r.opts.PollInterval) * time.Second

	var treeMu sync.Mutex
	lastTreeSize := make(map[string]int64)
	timeout := time.Duration(r.opts.Timeout) * time.Second
	clients := make(map[string]*ctlog.Client)

	var initWg sync.WaitGroup
	for _, logURL := range logURLs {
		initWg.Add(1)
		go func(logURL string) {
			defer initWg.Done()
			client := ctlog.NewClient(logURL, timeout, r.opts.Retries)
			sth, err := client.GetSTH(ctx)
			if err != nil {
				log.Warning("skipping %s: %v", logURL, err)
				return
			}
			treeMu.Lock()
			clients[logURL] = client
			lastTreeSize[logURL] = sth.TreeSize
			treeMu.Unlock()
			log.Debug("[%s] starting at tree size %d", truncate(logURL, 50), sth.TreeSize)
		}(logURL)
	}
	initWg.Wait()

	if len(lastTreeSize) == 0 {
		return fmt.Errorf("could not connect to any CT logs")
	}

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	log.Info("connected to %d log(s), polling every %vs (Ctrl+C to stop)", len(lastTreeSize), r.opts.PollInterval)

	poll := func() {
		if ctx.Err() != nil {
			return
		}

		var wg sync.WaitGroup
		monitorSem := make(chan struct{}, r.opts.Workers)
		treeMu.Lock()
		snapshot := make(map[string]int64, len(lastTreeSize))
		maps.Copy(snapshot, lastTreeSize)
		treeMu.Unlock()
		for logURL, prevSize := range snapshot {
			wg.Add(1)
			monitorSem <- struct{}{}
			go func(logURL string, prevSize int64) {
				defer func() { <-monitorSem }()
				defer wg.Done()

				client := clients[logURL]
				sth, err := client.GetSTH(ctx)
				if err != nil {
					log.Debug("poll error for %s: %v", logURL, err)
					return
				}

				newSize := sth.TreeSize
				if newSize <= prevSize {
					return
				}

				delta := newSize - prevSize
				log.Info("[%s] %d new entries (tree %d -> %d)",
					truncate(logURL, 40), delta, prevSize, newSize)

				r.fetchAndProcess(ctx, client, logURL, prevSize, newSize, parser, writer)
				writer.Flush()
				treeMu.Lock()
				lastTreeSize[logURL] = newSize
				treeMu.Unlock()
			}(logURL, prevSize)
		}
		wg.Wait()
	}

	poll()
	for {
		select {
		case <-ctx.Done():
			log.Success("monitor stopped - %d unique results written", writer.Stats())
			return nil
		case <-ticker.C:
			if err := ctx.Err(); err != nil {
				return nil
			}
			poll()
		}
	}
}

func (r *Runner) fetchAndProcess(
	ctx context.Context,
	client *ctlog.Client,
	logURL string,
	start, end int64,
	parser *certparser.Parser,
	writer *output.Writer,
) {
	pool := ctlog.NewWorkerPool(client, r.opts.BatchSize, r.opts.Workers, r.opts.RateLimit)
	pool.SetDebugLog(log.Debug)
	results := make(chan ctlog.EntryBatch, r.opts.Workers*2)

	fetchErr := make(chan error, 1)
	go func() {
		fetchErr <- pool.FetchRange(ctx, start, end, results)
	}()

	parseSem := r.newParseSem()

	for batch := range results {
		r.parseBatch(batch, parser, writer, logURL, parseSem, nil)
	}

	if err := <-fetchErr; err != nil {
		log.Debug("fetch error for %s: %v", logURL, err)
	}
}

func (r *Runner) newParseSem() chan struct{} {
	n := r.opts.ParseWorkers
	if n <= 0 {
		n = runtime.GOMAXPROCS(0)
	}
	return make(chan struct{}, n)
}

func (r *Runner) parseBatch(batch ctlog.EntryBatch, parser *certparser.Parser, writer *output.Writer, logURL string, parseSem chan struct{}, counter *atomic.Int64) {
	var wg sync.WaitGroup
	for i, entry := range batch.Entries {
		wg.Add(1)
		parseSem <- struct{}{}
		go func(e ctlog.RawEntry, idx int64) {
			defer func() { <-parseSem }()
			defer wg.Done()
			if counter != nil {
				defer counter.Add(1)
			}
			result, err := parser.ParseEntry(e, idx, logURL)
			if err != nil {
				log.Debug("parse error at entry %d: %v", idx, err)
				return
			}
			if result != nil {
				writer.WriteResult(result)
			}
		}(entry, batch.StartIndex+int64(i))
	}
	wg.Wait()
}

func (r *Runner) collectDomains() []string {
	var domains []string
	domains = append(domains, r.opts.Domain...)

	if r.opts.DomainFile != "" {
		fileDomains, err := readLinesFromFile(r.opts.DomainFile)
		if err != nil {
			log.Warning("reading domain file: %v", err)
		} else {
			domains = append(domains, fileDomains...)
		}
	}

	if hasStdin() {
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line != "" {
				domains = append(domains, line)
			}
		}
	}

	return domains
}

func (r *Runner) resolveLogURLs(ctx context.Context) ([]string, error) {
	if len(r.opts.LogURL) > 0 {
		urls := make([]string, len(r.opts.LogURL))
		for i, u := range r.opts.LogURL {
			switch {
			case strings.HasPrefix(u, "https://"):
				//
			case strings.HasPrefix(u, "http://"):
				log.Warning("upgrading %s to HTTPS", u)
				u = "https://" + strings.TrimPrefix(u, "http://")
			default:
				u = "https://" + u
			}
			urls[i] = u
		}
		return urls, nil
	}

	log.Info("auto-discovering CT logs...")

	timeout := time.Duration(r.opts.Timeout) * time.Second
	fetcher := loglist.NewFetcher(timeout)

	logList, err := fetcher.FetchDefault(ctx)
	if err != nil {
		return nil, fmt.Errorf("fetching log list: %w", err)
	}

	logs := loglist.FilterLogs(logList, r.opts.LogState)
	if len(logs) == 0 {
		return nil, fmt.Errorf("no logs found matching state filter '%s'", r.opts.LogState)
	}

	log.Info("found %d %s CT logs", len(logs), r.opts.LogState)

	urls := make([]string, len(logs))
	for i, l := range logs {
		urls[i] = l.Log.FullURL()
	}

	return urls, nil
}

func (r *Runner) calculateRange(treeSize int64) (start, end int64) {
	if r.opts.FromEnd {
		end = treeSize
		if r.opts.Count > 0 {
			start = max(end-r.opts.Count, 0)
		} else if r.opts.Start >= 0 {
			start = r.opts.Start
		} else {
			start = max(0, treeSize-10000)
		}
	} else {
		if r.opts.Start >= 0 {
			start = r.opts.Start
		}
		if r.opts.Count > 0 {
			end = min(start+r.opts.Count, treeSize)
		} else {
			end = treeSize
		}
	}
	return start, end
}

func (r *Runner) loadProgress(logURL string) *ctlog.ScrapeProgress {
	path := r.stateFilePath(logURL)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	var progress ctlog.ScrapeProgress
	if err := json.Unmarshal(data, &progress); err != nil {
		return nil
	}
	return &progress
}

func (r *Runner) saveProgress(logURL string, treeSize, lastIndex, entriesDone int64) {
	progress := ctlog.ScrapeProgress{
		LogURL:      logURL,
		TreeSize:    treeSize,
		LastIndex:   lastIndex,
		EntriesDone: entriesDone,
		LastUpdated: time.Now(),
	}

	data, err := json.Marshal(progress)
	if err != nil {
		log.Debug("failed to marshal progress: %v", err)
		return
	}

	if err := os.MkdirAll(r.opts.StateDir, 0o700); err != nil {
		log.Debug("failed to create state directory: %v", err)
		return
	}

	path := r.stateFilePath(logURL)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		log.Debug("failed to write state file: %v", err)
	}
}

func (r *Runner) stateFilePath(logURL string) string {
	safe := strings.NewReplacer(
		"https://", "",
		"http://", "",
		"/", "_",
		":", "_",
	).Replace(logURL)
	return filepath.Join(r.opts.StateDir, safe+".state.json")
}

func readLinesFromFile(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" && !strings.HasPrefix(line, "#") {
			lines = append(lines, line)
		}
	}
	return lines, scanner.Err()
}

func hasStdin() bool {
	stat, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (stat.Mode() & os.ModeCharDevice) == 0
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen < 3 {
		return s[:maxLen]
	}
	return s[:maxLen-2] + ".."
}
