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
	retryWg       sync.WaitGroup
	retryMu       sync.Mutex
	done          chan struct{}
	closeOnce     sync.Once
	maxRetries    int
	retryDelay    time.Duration
	maxRetryDelay time.Duration
	checkFn       func(string) (bool, bool, error)
}

func newAvailabilityWorkerPool(workerCount int) *AvailabilityWorkerPool {
	return &AvailabilityWorkerPool{
		workerCount:   workerCount,
		jobs:          make(chan AvailabilityJob, workerCount*2),
		retryJobs:     make(chan AvailabilityJob, workerCount*2),
		results:       make(chan AvailabilityResult, workerCount*2),
		done:          make(chan struct{}),
		maxRetries:    5,
		retryDelay:    1 * time.Second,
		maxRetryDelay: 30 * time.Second,
		checkFn:       youtube.CheckAvailability,
	}
}

func (p *AvailabilityWorkerPool) start() {
	for i := 0; i < p.workerCount; i++ {
		p.wg.Add(1)
		go p.worker(i)
	}
}

func NewAvailabilityWorkerPool(workerCount int) *AvailabilityWorkerPool {
	pool := newAvailabilityWorkerPool(workerCount)
	pool.start()
	return pool
}

func (p *AvailabilityWorkerPool) nextJob() (AvailabilityJob, bool) {
	select {
	case <-p.done:
		return AvailabilityJob{}, false
	case job := <-p.retryJobs:
		return job, true
	default:
	}

	select {
	case <-p.done:
		return AvailabilityJob{}, false
	case job := <-p.retryJobs:
		return job, true
	case job := <-p.jobs:
		return job, true
	}
}

func (p *AvailabilityWorkerPool) worker(id int) {
	defer p.wg.Done()

	scope := logger.Scopef("Worker %d", id)

	for {
		job, ok := p.nextJob()
		if !ok {
			return
		}

		scope.Debugf("Checking availability: %s (index: %d, retry: %d, batch: %d)",
			job.URL, job.Index, job.RetryCount, job.BatchID)

		available, isLive, err := p.checkFn(job.URL)

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
				scope.Warnf("Rate limit detected for: %s (retry: %d/%d)",
					job.URL, job.RetryCount, p.maxRetries)
			} else if strings.Contains(err.Error(), "timeout") ||
				strings.Contains(err.Error(), "connection") {
				shouldRetry = true
				scope.Warnf("Network error for: %s (retry: %d/%d)",
					job.URL, job.RetryCount, p.maxRetries)
			}

			if shouldRetry && job.RetryCount < p.maxRetries {
				result.ShouldRetry = true

				delay := p.backoffDelay(job.RetryCount)

				scope.Debugf("Scheduling retry for %s in %v (attempt %d/%d)",
					job.URL, delay, job.RetryCount+1, p.maxRetries)

				p.scheduleRetry(AvailabilityJob{
					URL:         job.URL,
					Index:       job.Index,
					RetryCount:  job.RetryCount + 1,
					Priority:    true,
					BatchID:     job.BatchID,
					ResultsChan: job.ResultsChan,
				}, delay)

				continue
			} else if job.RetryCount >= p.maxRetries {
				scope.Errorf("Max retries exceeded for: %s", job.URL)
			}
		}

		target := p.results
		if job.ResultsChan != nil {
			target = job.ResultsChan
		}

		select {
		case target <- result:
		case <-p.done:
			return
		}
	}
}

func (p *AvailabilityWorkerPool) backoffDelay(retryCount int) time.Duration {
	delay := time.Duration(1<<uint(retryCount)) * p.retryDelay
	if delay > p.maxRetryDelay {
		delay = p.maxRetryDelay
	}
	return delay
}

func (p *AvailabilityWorkerPool) scheduleRetry(job AvailabilityJob, delay time.Duration) {
	p.retryMu.Lock()
	select {
	case <-p.done:
		p.retryMu.Unlock()
		return
	default:
	}
	p.retryWg.Add(1)
	p.retryMu.Unlock()

	go p.awaitRetry(job, delay)
}

func (p *AvailabilityWorkerPool) awaitRetry(job AvailabilityJob, delay time.Duration) {
	defer p.retryWg.Done()

	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-p.done:
		return
	case <-timer.C:
	}

	select {
	case <-p.done:
	case p.retryJobs <- job:
	}
}

func (p *AvailabilityWorkerPool) submit(jobs []AvailabilityJob, batchResults chan AvailabilityResult) int {
	batchID := atomic.AddUint64(&batchIDCounter, 1)

	submitted := 0
	for i := range jobs {
		jobs[i].BatchID = batchID
		jobs[i].ResultsChan = batchResults

		select {
		case p.jobs <- jobs[i]:
			submitted++
		case <-p.done:
			return submitted
		}
	}

	return submitted
}

func (p *AvailabilityWorkerPool) CheckBatch(jobs []AvailabilityJob) []AvailabilityResult {
	if len(jobs) == 0 {
		return []AvailabilityResult{}
	}

	batchResults := make(chan AvailabilityResult, len(jobs))
	submitted := p.submit(jobs, batchResults)

	sortedResults := make([]AvailabilityResult, len(jobs))
	for i := 0; i < submitted; i++ {
		select {
		case result := <-batchResults:
			if result.Index >= 0 && result.Index < len(sortedResults) {
				sortedResults[result.Index] = result
			}
		case <-p.done:
			return sortedResults
		}
	}

	return sortedResults
}

func (p *AvailabilityWorkerPool) CheckBatchImmediate(jobs []AvailabilityJob, callback func(AvailabilityResult)) {
	if len(jobs) == 0 {
		return
	}

	batchResults := make(chan AvailabilityResult, len(jobs))
	submitted := p.submit(jobs, batchResults)

	for i := 0; i < submitted; i++ {
		select {
		case result := <-batchResults:
			callback(result)
		case <-p.done:
			return
		}
	}
}

func (p *AvailabilityWorkerPool) Close() {
	p.closeOnce.Do(func() {
		p.retryMu.Lock()
		close(p.done)
		p.retryMu.Unlock()

		p.retryWg.Wait()
		p.wg.Wait()
		logger.Infof("Shut down successfully")
	})
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
		logger.Debugf("Created global worker pool with 10 workers")
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
		logger.Debug("No global worker pool to close")
	}
}
