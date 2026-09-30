//go:build testhooks

package youtube

import "time"

const HookAvailabilityCacheCapacity = availabilityCacheCapacity
const HookAvailabilityCacheTTL = availabilityCacheTTL
const HookCircuitHalfOpen = circuitHalfOpen
const HookCircuitOpen = circuitOpen

type HookCircuitBreaker = circuitBreaker

type HookCircuitBreakerFields struct {
	State           circuitState
	LastFailureTime time.Time
}

type HookInnertubeClientFields struct {
	ClientName string
}

var HookAvailabilityCacheEntries = &availabilityCacheEntries
var HookAvailabilityCacheMutex = &availabilityCacheMutex
var HookAvailabilityCacheOrder = &availabilityCacheOrder
var HookCheckAvailabilityViaInnertube = &checkAvailabilityViaInnertube
var HookCircuitCooldownPeriod = &circuitCooldownPeriod
var HookCircuitOpenThreshold = &circuitOpenThreshold
var HookExtractVideoID = extractVideoID
var HookGetInnertubeClient = getInnertubeClient
var HookInnertubeClient = &innertubeClient
var HookInnertubeInit = &innertubeInit
var HookInnertubeOnce = &innertubeOnce
var HookLoadAvailability = loadAvailability
var HookNewAvailabilityPool = newAvailabilityPool
var HookParseYouTubeURL = parseYouTubeURL
var HookResetAvailabilityCache = resetAvailabilityCache
var HookRunYtDlpAvailability = &runYtDlpAvailability
var HookSaveAvailability = saveAvailability
var HookSaveVersionResult = saveVersionResult
var HookStreamPipeArgs = streamPipeArgs
var HookYtCircuitBreaker = &ytCircuitBreaker

func HookBuildCircuitBreaker(fields HookCircuitBreakerFields) *circuitBreaker {
	return &circuitBreaker{state: fields.State, lastFailureTime: fields.LastFailureTime}
}

func HookBuildInnertubeClient(fields HookInnertubeClientFields) *InnertubeClient {
	return &InnertubeClient{clientName: fields.ClientName}
}

func (a *availabilityCacheEntry) HookTimestamp() *time.Time {
	return &a.timestamp
}

func (p *AvailabilityPool) HookCheckFn() *func(string) (bool, bool, error) {
	return &p.checkFn
}

func (p *AvailabilityPool) HookMaxRetries() *int {
	return &p.maxRetries
}

func (p *AvailabilityPool) HookMaxRetryDelay() *time.Duration {
	return &p.maxRetryDelay
}

func (p *AvailabilityPool) HookRetryDelay() *time.Duration {
	return &p.retryDelay
}

func (p *AvailabilityPool) HookBackoffDelay(retryCount int) time.Duration {
	return p.backoffDelay(retryCount)
}

func (p *AvailabilityPool) HookStart() {
	p.start()
}

func (p *AvailabilityPool) HookSubmit(jobs []BatchJob, batchResults chan BatchResult) int {
	return p.submit(jobs, batchResults)
}

func (cb *circuitBreaker) HookCanAttempt() error {
	return cb.canAttempt()
}

func (cb *circuitBreaker) HookRecordFailure(err error) {
	cb.recordFailure(err)
}

func (cb *circuitBreaker) HookRecordSuccess() {
	cb.recordSuccess()
}
