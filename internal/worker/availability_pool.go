package worker

import (
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"noraegaori/internal/youtube"
	"noraegaori/pkg/logger"
)

var batchIDCounter uint64

type AvailabilityJob struct {
	URL         string
	Index       int
	RetryCount  int
	Priority    bool
	BatchID     uint64
	ResultsChan chan AvailabilityResult
}

type AvailabilityResult struct {
	URL         string
	Index       int
	Available   bool
	IsLive      bool
	Error       string
	ShouldRetry bool
	BatchID     uint64
}

type AvailabilityWorkerPool struct {
	workerCount   int
	jobs          chan AvailabilityJob
	retryJobs     chan AvailabilityJob
	results       chan AvailabilityResult
	wg            sync.WaitGroup
	stopping      bool
	stoppingMu    sync.RWMutex
	maxRetries    int
	retryDelay    time.Duration
	maxRetryDelay time.Duration
}

func NewAvailabilityWorkerPool(workerCount int) *AvailabilityWorkerPool {
	pool := &AvailabilityWorkerPool{
		workerCount:   workerCount,
		jobs:          make(chan AvailabilityJob, workerCount*2),
		retryJobs:     make(chan AvailabilityJob, workerCount*2),
		results:       make(chan AvailabilityResult, workerCount*2),
		stopping:      false,
		maxRetries:    5,
		retryDelay:    1 * time.Second,
		maxRetryDelay: 30 * time.Second,
	}

	for i := 0; i < workerCount; i++ {
		pool.wg.Add(1)
		go pool.worker(i)
	}

	return pool
}

func (p *AvailabilityWorkerPool) worker(id int) {
	defer p.wg.Done()

	for {

		p.stoppingMu.RLock()
		stopping := p.stopping
		p.stoppingMu.RUnlock()

		if stopping {
			return
		}

		var job AvailabilityJob
		var ok bool

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

				delay := time.Duration(1<<uint(job.RetryCount)) * p.retryDelay
				if delay > p.maxRetryDelay {
					delay = p.maxRetryDelay
				}

				logger.Debugf("[Worker %d] Scheduling retry for %s in %v (attempt %d/%d)",
					id, job.URL, delay, job.RetryCount+1, p.maxRetries)

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

				continue
			} else if job.RetryCount >= p.maxRetries {
				logger.Errorf("[Worker %d] Max retries exceeded for: %s", id, job.URL)
			}
		}

		if job.ResultsChan != nil {
			job.ResultsChan <- result
		} else {
			p.results <- result
		}
	}
}

func (p *AvailabilityWorkerPool) CheckBatch(jobs []AvailabilityJob) []AvailabilityResult {
	if len(jobs) == 0 {
		return []AvailabilityResult{}
	}

	batchID := atomic.AddUint64(&batchIDCounter, 1)

	batchResults := make(chan AvailabilityResult, len(jobs))

	for i := range jobs {
		jobs[i].BatchID = batchID
		jobs[i].ResultsChan = batchResults
		p.jobs <- jobs[i]
	}

	results := make([]AvailabilityResult, 0, len(jobs))
	for i := 0; i < len(jobs); i++ {
		result := <-batchResults
		results = append(results, result)
	}

	close(batchResults)

	sortedResults := make([]AvailabilityResult, len(results))
	for _, result := range results {
		sortedResults[result.Index] = result
	}

	return sortedResults
}

func (p *AvailabilityWorkerPool) CheckBatchImmediate(jobs []AvailabilityJob, callback func(AvailabilityResult)) {
	if len(jobs) == 0 {
		return
	}

	batchID := atomic.AddUint64(&batchIDCounter, 1)

	batchResults := make(chan AvailabilityResult, len(jobs))

	for i := range jobs {
		jobs[i].BatchID = batchID
		jobs[i].ResultsChan = batchResults
		p.jobs <- jobs[i]
	}

	for i := 0; i < len(jobs); i++ {
		result := <-batchResults
		callback(result)
	}

	close(batchResults)
}

func (p *AvailabilityWorkerPool) Close() {
	p.stoppingMu.Lock()
	p.stopping = true
	p.stoppingMu.Unlock()

	close(p.jobs)
	close(p.retryJobs)
	p.wg.Wait()
	close(p.results)

	logger.Infof("[WorkerPool] Shut down successfully")
}

var (
	globalPool   *AvailabilityWorkerPool
	globalPoolMu sync.Mutex
)

func GetWorkerPool() *AvailabilityWorkerPool {
	globalPoolMu.Lock()
	defer globalPoolMu.Unlock()

	if globalPool == nil {

		globalPool = NewAvailabilityWorkerPool(10)
		logger.Debugf("[WorkerPool] Created global worker pool with 10 workers")
	}

	return globalPool
}

func CloseGlobalPool() {
	globalPoolMu.Lock()
	pool := globalPool
	globalPool = nil
	globalPoolMu.Unlock()

	if pool != nil {
		pool.Close()
	} else {
		logger.Debug("[WorkerPool] No global worker pool to close")
	}
}
