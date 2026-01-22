package metrics

import (
	"encoding/json"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
)

// MetricsConfig holds metrics configuration
type MetricsConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	Path    string `mapstructure:"path"`
}

// DefaultMetricsConfig returns the default metrics configuration
func DefaultMetricsConfig() MetricsConfig {
	return MetricsConfig{
		Enabled: true,
		Path:    "/metrics",
	}
}

// Metrics holds all metrics (simple in-memory implementation)
type Metrics struct {
	config MetricsConfig

	// Request metrics
	requestsTotal     map[string]*int64 // mode:model:status -> count
	requestsDuration  map[string]*durationMetric // mode:model -> duration stats
	requestsInFlight  map[string]*int64 // mode -> count
	ttft              map[string]*durationMetric // mode:model -> ttft stats

	// Account metrics
	accountRequests map[string]*int64 // account_id -> count
	accountErrors   map[string]*int64 // account_id -> count
	accountHealth   map[string]bool   // account_id -> healthy

	// Rate limit metrics
	rateLimitHits map[string]*int64 // type -> count

	// Retry metrics
	retryAttempts   int64
	retrySuccesses  int64
	accountSwitches map[string]*int64 // reason -> count

	// Pool metrics
	poolClients int64

	// Concurrency metrics
	waitDuration map[string]*durationMetric // type -> duration stats

	mu sync.RWMutex
}

type durationMetric struct {
	count   int64
	sumMs   int64 // sum in milliseconds
	minMs   int64
	maxMs   int64
}

// NewMetrics creates a new metrics instance
func NewMetrics(config MetricsConfig) *Metrics {
	if !config.Enabled {
		return nil
	}

	return &Metrics{
		config:           config,
		requestsTotal:    make(map[string]*int64),
		requestsDuration: make(map[string]*durationMetric),
		requestsInFlight: make(map[string]*int64),
		ttft:             make(map[string]*durationMetric),
		accountRequests:  make(map[string]*int64),
		accountErrors:    make(map[string]*int64),
		accountHealth:    make(map[string]bool),
		rateLimitHits:    make(map[string]*int64),
		accountSwitches:  make(map[string]*int64),
		waitDuration:     make(map[string]*durationMetric),
	}
}

// Handler returns the HTTP handler for metrics
func (m *Metrics) Handler() gin.HandlerFunc {
	return func(c *gin.Context) {
		if m == nil {
			c.JSON(http.StatusOK, gin.H{"error": "metrics disabled"})
			return
		}

		m.mu.RLock()
		defer m.mu.RUnlock()

		stats := m.getStats()
		c.JSON(http.StatusOK, stats)
	}
}

func (m *Metrics) getStats() map[string]interface{} {
	stats := make(map[string]interface{})

	// Request stats
	requestStats := make(map[string]int64)
	for k, v := range m.requestsTotal {
		if v != nil {
			requestStats[k] = atomic.LoadInt64(v)
		}
	}
	stats["requests_total"] = requestStats

	// In-flight requests
	inFlight := make(map[string]int64)
	for k, v := range m.requestsInFlight {
		if v != nil {
			inFlight[k] = atomic.LoadInt64(v)
		}
	}
	stats["requests_in_flight"] = inFlight

	// Duration stats
	durationStats := make(map[string]interface{})
	for k, v := range m.requestsDuration {
		if v != nil {
			durationStats[k] = map[string]interface{}{
				"count":    v.count,
				"sum_ms":   v.sumMs,
				"min_ms":   v.minMs,
				"max_ms":   v.maxMs,
				"avg_ms":   safeDivide(v.sumMs, v.count),
			}
		}
	}
	stats["request_duration"] = durationStats

	// TTFT stats
	ttftStats := make(map[string]interface{})
	for k, v := range m.ttft {
		if v != nil {
			ttftStats[k] = map[string]interface{}{
				"count":    v.count,
				"sum_ms":   v.sumMs,
				"min_ms":   v.minMs,
				"max_ms":   v.maxMs,
				"avg_ms":   safeDivide(v.sumMs, v.count),
			}
		}
	}
	stats["ttft"] = ttftStats

	// Account stats
	accountStats := make(map[string]interface{})
	for k, v := range m.accountRequests {
		if v != nil {
			accountStats[k] = map[string]interface{}{
				"requests": atomic.LoadInt64(v),
				"errors":   func() int64 { if e := m.accountErrors[k]; e != nil { return atomic.LoadInt64(e) }; return 0 }(),
				"healthy":  m.accountHealth[k],
			}
		}
	}
	stats["accounts"] = accountStats

	// Rate limit stats
	rateLimitStats := make(map[string]int64)
	for k, v := range m.rateLimitHits {
		if v != nil {
			rateLimitStats[k] = atomic.LoadInt64(v)
		}
	}
	stats["rate_limit_hits"] = rateLimitStats

	// Retry stats
	stats["retry"] = map[string]interface{}{
		"attempts":  atomic.LoadInt64(&m.retryAttempts),
		"successes": atomic.LoadInt64(&m.retrySuccesses),
	}

	// Account switches
	switchStats := make(map[string]int64)
	for k, v := range m.accountSwitches {
		if v != nil {
			switchStats[k] = atomic.LoadInt64(v)
		}
	}
	stats["account_switches"] = switchStats

	// Pool stats
	stats["pool_clients"] = atomic.LoadInt64(&m.poolClients)

	return stats
}

func safeDivide(a, b int64) int64 {
	if b == 0 {
		return 0
	}
	return a / b
}

// RecordRequest records a completed request
func (m *Metrics) RecordRequest(mode, model string, status int, duration time.Duration) {
	if m == nil {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Total count
	key := mode + ":" + model + ":" + string(rune(status))
	if m.requestsTotal[key] == nil {
		var zero int64
		m.requestsTotal[key] = &zero
	}
	atomic.AddInt64(m.requestsTotal[key], 1)

	// Duration
	durationKey := mode + ":" + model
	if m.requestsDuration[durationKey] == nil {
		m.requestsDuration[durationKey] = &durationMetric{minMs: int64(^uint64(0) >> 1)}
	}
	dm := m.requestsDuration[durationKey]
	ms := duration.Milliseconds()
	dm.count++
	dm.sumMs += ms
	if ms < dm.minMs {
		dm.minMs = ms
	}
	if ms > dm.maxMs {
		dm.maxMs = ms
	}
}

// RecordTTFT records time to first token
func (m *Metrics) RecordTTFT(mode, model string, duration time.Duration) {
	if m == nil {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	key := mode + ":" + model
	if m.ttft[key] == nil {
		m.ttft[key] = &durationMetric{minMs: int64(^uint64(0) >> 1)}
	}
	dm := m.ttft[key]
	ms := duration.Milliseconds()
	dm.count++
	dm.sumMs += ms
	if ms < dm.minMs {
		dm.minMs = ms
	}
	if ms > dm.maxMs {
		dm.maxMs = ms
	}
}

// RecordWait records wait time for a slot
func (m *Metrics) RecordWait(slotType string, duration time.Duration) {
	if m == nil {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.waitDuration[slotType] == nil {
		m.waitDuration[slotType] = &durationMetric{minMs: int64(^uint64(0) >> 1)}
	}
	dm := m.waitDuration[slotType]
	ms := duration.Milliseconds()
	dm.count++
	dm.sumMs += ms
	if ms < dm.minMs {
		dm.minMs = ms
	}
	if ms > dm.maxMs {
		dm.maxMs = ms
	}
}

// RecordAccountRequest records a request for an account
func (m *Metrics) RecordAccountRequest(accountID string) {
	if m == nil {
		return
	}

	m.mu.Lock()
	if m.accountRequests[accountID] == nil {
		var zero int64
		m.accountRequests[accountID] = &zero
	}
	m.mu.Unlock()

	atomic.AddInt64(m.accountRequests[accountID], 1)
}

// RecordAccountError records an error for an account
func (m *Metrics) RecordAccountError(accountID string) {
	if m == nil {
		return
	}

	m.mu.Lock()
	if m.accountErrors[accountID] == nil {
		var zero int64
		m.accountErrors[accountID] = &zero
	}
	m.mu.Unlock()

	atomic.AddInt64(m.accountErrors[accountID], 1)
}

// SetAccountHealth sets the health status for an account
func (m *Metrics) SetAccountHealth(accountID string, healthy bool) {
	if m == nil {
		return
	}

	m.mu.Lock()
	m.accountHealth[accountID] = healthy
	m.mu.Unlock()
}

// SetAccountCircuit sets the circuit breaker state for an account (not used in simple impl)
func (m *Metrics) SetAccountCircuit(accountID string, state int) {
	// Not implemented in simple version
}

// RecordRateLimitHit records a rate limit hit
func (m *Metrics) RecordRateLimitHit(limitType string) {
	if m == nil {
		return
	}

	m.mu.Lock()
	if m.rateLimitHits[limitType] == nil {
		var zero int64
		m.rateLimitHits[limitType] = &zero
	}
	m.mu.Unlock()

	atomic.AddInt64(m.rateLimitHits[limitType], 1)
}

// RecordRetry records a retry attempt
func (m *Metrics) RecordRetry(success bool) {
	if m == nil {
		return
	}

	atomic.AddInt64(&m.retryAttempts, 1)
	if success {
		atomic.AddInt64(&m.retrySuccesses, 1)
	}
}

// RecordAccountSwitch records an account switch
func (m *Metrics) RecordAccountSwitch(reason string) {
	if m == nil {
		return
	}

	m.mu.Lock()
	if m.accountSwitches[reason] == nil {
		var zero int64
		m.accountSwitches[reason] = &zero
	}
	m.mu.Unlock()

	atomic.AddInt64(m.accountSwitches[reason], 1)
}

// SetPoolClients sets the number of clients in pool
func (m *Metrics) SetPoolClients(count int) {
	if m == nil {
		return
	}

	atomic.StoreInt64(&m.poolClients, int64(count))
}

// RequestTracker tracks request metrics
type RequestTracker struct {
	metrics   *Metrics
	mode      string
	model     string
	startTime time.Time
	ttft      time.Time
	ttftSet   bool
	mu        sync.Mutex
}

// NewRequestTracker creates a new request tracker
func (m *Metrics) NewRequestTracker(mode, model string) *RequestTracker {
	if m == nil {
		return nil
	}

	m.mu.Lock()
	if m.requestsInFlight[mode] == nil {
		var zero int64
		m.requestsInFlight[mode] = &zero
	}
	m.mu.Unlock()

	atomic.AddInt64(m.requestsInFlight[mode], 1)

	return &RequestTracker{
		metrics:   m,
		mode:      mode,
		model:     model,
		startTime: time.Now(),
	}
}

// RecordTTFT records the time to first token
func (t *RequestTracker) RecordTTFT() {
	if t == nil {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.ttftSet {
		t.ttft = time.Now()
		t.ttftSet = true
		t.metrics.RecordTTFT(t.mode, t.model, t.ttft.Sub(t.startTime))
	}
}

// Finish finishes tracking and records metrics
func (t *RequestTracker) Finish(status int) {
	if t == nil {
		return
	}

	duration := time.Since(t.startTime)
	t.metrics.RecordRequest(t.mode, t.model, status, duration)

	t.metrics.mu.RLock()
	inFlight := t.metrics.requestsInFlight[t.mode]
	t.metrics.mu.RUnlock()

	if inFlight != nil {
		atomic.AddInt64(inFlight, -1)
	}
}

// MarshalJSON implements json.Marshaler for Metrics
func (m *Metrics) MarshalJSON() ([]byte, error) {
	if m == nil {
		return []byte("null"), nil
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	return json.Marshal(m.getStats())
}
