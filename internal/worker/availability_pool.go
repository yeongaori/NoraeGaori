package worker

import (
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"noraegaori/internal/youtube"
	"noraegaori/pkg/logger"
)

// Global batch ID counter for unique batch identification
var batchIDCounter uint64

// AvailabilityJob represents a job to check video availability
type AvailabilityJob struct {
	URL         string
	Index       int
	RetryCount  int    // Number of retries attempted
	Priority    bool   // Priority jobs (retries) are processed first
	BatchID     uint64 // Unique ID to identify which batch this job belongs to
	ResultsChan chan AvailabilityResult // Channel to send result back to specific batch
}

// AvailabilityResult represents the result of an availability check
type AvailabilityResult struct {
	URL         string
	Index       int
	Available   bool
	IsLive      bool
	Error       string
	ShouldRetry bool   // Whether the job should be retried
	BatchID     uint64 // Unique ID to identify which batch this result belongs to
}

// AvailabilityWorkerPool manages parallel availability checking with retry logic
type AvailabilityWorkerPool struct {
	workerCount  int
	jobs         chan AvailabilityJob
	retryJobs    chan AvailabilityJob // Priority queue for retries
	results      chan AvailabilityResult
	wg           sync.WaitGroup
	stopping     bool
	stoppingMu   sync.RWMutex
	maxRetries   int
	retryDelay   time.Duration
	maxRetryDelay time.Duration
}

// NewAvailabilityWorkerPool creates a new worker pool
func NewAvailabilityWorkerPool(workerCount int) *AvailabilityWorkerPool {
	pool := &AvailabilityWorkerPool{
		workerCount:   workerCount,
		jobs:          make(chan AvailabilityJob, workerCount*2),
		retryJobs:     make(chan AvailabilityJob, workerCount*2),
		results:       make(chan AvailabilityResult, workerCount*2),
		stopping:      false,
		maxRetries:    5,                 // Match NodeJS maxRetries
		retryDelay:    1 * time.Second,   // Base delay for exponential backoff
		maxRetryDelay: 30 * time.Second,  // Max delay cap
	}

	// Start workers
	for i := 0; i < workerCount; i++ {
		pool.wg.Add(1)
		go pool.worker(i)
	}

	return pool
}

// worker processes availability check jobs with priority for retries
func (p *AvailabilityWorkerPool) worker(id int) {
	defer p.wg.Done()

	for {
		// Check if stopping
		p.stoppingMu.RLock()
		stopping := p.stopping
		p.stoppingMu.RUnlock()

		if stopping {
			return
		}

		var job AvailabilityJob
		var ok bool

		// Priority: Process retries first, then regular jobs
		select {
		case job, ok = <-p.retryJobs:
			if !ok {
				return
			}
		default:
			select {
			case job, ok = <-p.retryJobs:
				if !ok {
					return
				}
			case job, ok = <-p.jobs:
				if !ok {
					return
				}
			}
		}

		logger.Debugf("[Worker %d] Checking availability: %s (index: %d, retry: %d, batch: %d)",
			id, job.URL, job.Index, job.RetryCount, job.BatchID)

		available, isLive, err := youtube.CheckAvailability(job.URL)

		result := AvailabilityResult{
			URL:         job.URL,
			Index:       job.Index,
			Available:   available,
			IsLive:      isLive,
			ShouldRetry: false,
			BatchID:     job.BatchID,
		}

		if err != nil {
			result.Error = err.Error()

			// Detect rate limiting (HTTP 429) or network errors that should be retried
			shouldRetry := false
			if strings.Contains(err.Error(), "429") ||
			   strings.Contains(err.Error(), "Too Many Requests") ||
			   strings.Contains(err.Error(), "rate limit") {
				shouldRetry = true
				logger.Warnf("[Worker %d] Rate limit detected for: %s (retry: %d/%d)",
					id, job.URL, job.RetryCount, p.maxRetries)
			} else if strings.Contains(err.Error(), "timeout") ||
					  strings.Contains(err.Error(), "connection") {
				shouldRetry = true
				logger.Warnf("[Worker %d] Network error for: %s (retry: %d/%d)",
					id, job.URL, job.RetryCount, p.maxRetries)
			}

			if shouldRetry && job.RetryCount < p.maxRetries {
				result.ShouldRetry = true

				// Calculate exponential backoff delay: 2^retryCount * baseDelay
				delay := time.Duration(1<<uint(job.RetryCount)) * p.retryDelay
				if delay > p.maxRetryDelay {
					delay = p.maxRetryDelay
				}

				logger.Infof("[Worker %d] Scheduling retry for %s in %v (attempt %d/%d)",
					id, job.URL, delay, job.RetryCount+1, p.maxRetries)

				// Schedule retry with exponential backoff
				go func(retryJob AvailabilityJob, retryDelay time.Duration) {
					time.Sleep(retryDelay)
					p.retryJobs <- retryJob
				}(AvailabilityJob{
					URL:         job.URL,
					Index:       job.Index,
					RetryCount:  job.RetryCount + 1,
					Priority:    true,
					BatchID:     job.BatchID,
					ResultsChan: job.ResultsChan,
				}, delay)

				continue // Don't send result yet, waiting for retry
			} else if job.RetryCount >= p.maxRetries {
				logger.Errorf("[Worker %d] Max retries exceeded for: %s", id, job.URL)
			}
		}

		// Send result to batch-specific channel if provided, otherwise use global
		if job.ResultsChan != nil {
			job.ResultsChan <- result
		} else {
			p.results <- result
		}
	}
}

// CheckBatch checks availability for multiple URLs in parallel
// Returns results sorted by original index
// Uses per-batch result channels to prevent mixing results between concurrent batches
func (p *AvailabilityWorkerPool) CheckBatch(jobs []AvailabilityJob) []AvailabilityResult {
	if len(jobs) == 0 {
		return []AvailabilityResult{}
	}

	// Generate unique batch ID
	batchID := atomic.AddUint64(&batchIDCounter, 1)

	// Create batch-specific result channel
	batchResults := make(chan AvailabilityResult, len(jobs))

	// Send all jobs with batch ID and result channel
	for i := range jobs {
		jobs[i].BatchID = batchID
		jobs[i].ResultsChan = batchResults
		p.jobs <- jobs[i]
	}

	// Collect results from batch-specific channel
	results := make([]AvailabilityResult, 0, len(jobs))
	for i := 0; i < len(jobs); i++ {
		result := <-batchResults
		results = append(results, result)
	}

	// Close the batch channel
	close(batchResults)

	// Sort by index to maintain order
	sortedResults := make([]AvailabilityResult, len(results))
	for _, result := range results {
		sortedResults[result.Index] = result
	}

	return sortedResults
}

// CheckBatchImmediate checks availability and calls callback immediately for each result
// Results are returned as they complete, not in order
// Uses per-batch result channels to prevent mixing results between concurrent batches
func (p *AvailabilityWorkerPool) CheckBatchImmediate(jobs []AvailabilityJob, callback func(AvailabilityResult)) {
	if len(jobs) == 0 {
		return
	}

	// Generate unique batch ID
	batchID := atomic.AddUint64(&batchIDCounter, 1)

	// Create batch-specific result channel
	batchResults := make(chan AvailabilityResult, len(jobs))

	// Send all jobs with batch ID and result channel
	for i := range jobs {
		jobs[i].BatchID = batchID
		jobs[i].ResultsChan = batchResults
		p.jobs <- jobs[i]
	}

	// Process results as they come from batch-specific channel
	for i := 0; i < len(jobs); i++ {
		result := <-batchResults
		callback(result)
	}

	// Close the batch channel
	close(batchResults)
}

// Close shuts down the worker pool gracefully
func (p *AvailabilityWorkerPool) Close() {
	p.stoppingMu.Lock()
	p.stopping = true
	p.stoppingMu.Unlock()

	close(p.jobs)
	close(p.retryJobs)
	p.wg.Wait()
	close(p.results)

	logger.Infof("[WorkerPool] Shut down gracefully")
}

// Global worker pool instance
var (
	globalPool   *AvailabilityWorkerPool
	globalPoolMu sync.Mutex
)

// GetWorkerPool returns the global worker pool instance
func GetWorkerPool() *AvailabilityWorkerPool {
	globalPoolMu.Lock()
	defer globalPoolMu.Unlock()

	if globalPool == nil {
		// Create pool with 10 workers for parallel processing
		globalPool = NewAvailabilityWorkerPool(10)
		logger.Infof("[WorkerPool] Created global worker pool with 10 workers")
	}

	return globalPool
}
