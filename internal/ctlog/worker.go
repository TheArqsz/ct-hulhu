package ctlog

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

type EntryBatch struct {
	StartIndex int64
	Entries    []RawEntry
}

type WorkerPool struct {
	client     *Client
	batchSize  int
	maxWorkers int
	rateLimit  int

	activeWorkers  atomic.Int32
	errCount       atomic.Int32
	successCount   atomic.Int32
	droppedEntries atomic.Int64
	debugLog       func(format string, args ...any)
}

func NewWorkerPool(client *Client, batchSize, maxWorkers, rateLimit int) *WorkerPool {
	return &WorkerPool{
		client:     client,
		batchSize:  batchSize,
		maxWorkers: maxWorkers,
		rateLimit:  rateLimit,
	}
}

func (wp *WorkerPool) SetDebugLog(fn func(format string, args ...any)) {
	wp.debugLog = fn
}

func (wp *WorkerPool) DroppedEntries() int64 {
	return wp.droppedEntries.Load()
}

func (wp *WorkerPool) debug(format string, args ...any) {
	if wp.debugLog != nil {
		wp.debugLog(format, args...)
	}
}

type workItem struct {
	start, end int64
}

const maxItemRetries = 3

func (wp *WorkerPool) FetchRange(ctx context.Context, start, end int64, results chan<- EntryBatch) error {
	defer close(results)

	if start >= end {
		return nil
	}

	work := make(chan workItem, wp.maxWorkers*2)

	go func() {
		defer close(work)
		for pos := start; pos < end; pos += int64(wp.batchSize) {
			batchEnd := pos + int64(wp.batchSize) - 1
			if batchEnd >= end {
				batchEnd = end - 1
			}
			select {
			case work <- workItem{start: pos, end: batchEnd}:
			case <-ctx.Done():
				return
			}
		}
	}()

	var rateLimiter <-chan time.Time
	if wp.rateLimit > 0 {
		ticker := time.NewTicker(time.Second / time.Duration(wp.rateLimit))
		defer ticker.Stop()
		rateLimiter = ticker.C
	}

	var wg sync.WaitGroup

	initialWorkers := min(1, wp.maxWorkers)

	workersDone := make(chan struct{})
	go func() {
		defer close(workersDone)

		for i := 0; i < initialWorkers; i++ {
			wg.Add(1)
			go wp.worker(ctx, work, results, rateLimiter, &wg)
			wp.activeWorkers.Add(1)
		}

		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				wg.Wait()
				return
			case <-ticker.C:
				current := int(wp.activeWorkers.Load())
				successes := wp.successCount.Load()
				errors := wp.errCount.Load()

				total := successes + errors
				if current < wp.maxWorkers && total > 0 {
					errorRate := float64(errors) / float64(total)
					if errorRate < 0.1 {
						wp.debug("ramping up: %d -> %d workers (error rate: %.1f%%)",
							current, current+1, errorRate*100)
						wg.Add(1)
						go wp.worker(ctx, work, results, rateLimiter, &wg)
						wp.activeWorkers.Add(1)
					}
				}

				if current == 0 {
					wg.Wait()
					return
				}
			}
		}
	}()

	select {
	case <-ctx.Done():
		<-workersDone
		return ctx.Err()
	case <-workersDone:
		wg.Wait()
		return nil
	}
}

func (wp *WorkerPool) worker(
	ctx context.Context,
	work <-chan workItem,
	results chan<- EntryBatch,
	rateLimiter <-chan time.Time,
	wg *sync.WaitGroup,
) {
	defer func() {
		wp.activeWorkers.Add(-1)
		wg.Done()
	}()

	for item := range work {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if rateLimiter != nil {
			select {
			case <-rateLimiter:
			case <-ctx.Done():
				return
			}
		}

		wp.fetchWithRetry(ctx, item, results)
	}
}

func (wp *WorkerPool) fetchWithRetry(ctx context.Context, item workItem, results chan<- EntryBatch) {
	currentStart := item.start

	for currentStart <= item.end {
		select {
		case <-ctx.Done():
			return
		default:
		}

		resp, err := wp.client.GetRawEntries(ctx, currentStart, item.end)
		if err != nil {
			wp.errCount.Add(1)
			dropped := item.end - currentStart + 1
			wp.droppedEntries.Add(dropped)
			wp.debug("batch [%d-%d] failed, dropping: %v", currentStart, item.end, err)
			return
		}

		wp.successCount.Add(1)

		if len(resp.Entries) == 0 {
			return
		}

		wp.debug("batch [%d-%d] fetched %d entries", currentStart, currentStart+int64(len(resp.Entries))-1, len(resp.Entries))
		select {
		case results <- EntryBatch{StartIndex: currentStart, Entries: resp.Entries}:
		case <-ctx.Done():
			return
		}

		currentStart += int64(len(resp.Entries))
	}
}

func (wp *WorkerPool) ErrorInfo() string {
	errors := wp.errCount.Load()
	successes := wp.successCount.Load()
	total := errors + successes
	if total == 0 {
		return "no requests made"
	}
	return fmt.Sprintf("%d errors / %d total requests (%.1f%% error rate)",
		errors, total, float64(errors)/float64(total)*100)
}
