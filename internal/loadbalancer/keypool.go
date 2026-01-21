package loadbalancer

import (
	"math/rand"
	"sync"
	"sync/atomic"
	"time"
)

type Strategy string

const (
	StrategyRoundRobin Strategy = "round_robin"
	StrategyRandom     Strategy = "random"
)

type KeyStats struct {
	Key          string    `json:"key"`
	RequestCount int64     `json:"request_count"`
	ErrorCount   int64     `json:"error_count"`
	LastUsed     time.Time `json:"last_used"`
	LastError    time.Time `json:"last_error,omitempty"`
	IsHealthy    bool      `json:"is_healthy"`
}

type keyState struct {
	key          string
	requestCount int64
	errorCount   int64
	lastUsed     time.Time
	lastError    time.Time
	isHealthy    bool
}

type KeyPool struct {
	keys     []*keyState
	strategy Strategy
	current  uint64
	mu       sync.RWMutex
	rng      *rand.Rand
}

func NewKeyPool(keys []string, strategy Strategy) *KeyPool {
	states := make([]*keyState, len(keys))
	for i, key := range keys {
		states[i] = &keyState{
			key:       key,
			isHealthy: true,
		}
	}

	return &KeyPool{
		keys:     states,
		strategy: strategy,
		rng:      rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

func (p *KeyPool) Get() string {
	if len(p.keys) == 0 {
		return ""
	}

	p.mu.RLock()
	defer p.mu.RUnlock()

	// Try to find a healthy key
	healthyKeys := make([]*keyState, 0, len(p.keys))
	for _, k := range p.keys {
		if k.isHealthy {
			healthyKeys = append(healthyKeys, k)
		}
	}

	// If no healthy keys, try all keys
	if len(healthyKeys) == 0 {
		healthyKeys = p.keys
	}

	var selected *keyState

	switch p.strategy {
	case StrategyRandom:
		selected = healthyKeys[p.rng.Intn(len(healthyKeys))]
	case StrategyRoundRobin:
		fallthrough
	default:
		idx := atomic.AddUint64(&p.current, 1) % uint64(len(healthyKeys))
		selected = healthyKeys[idx]
	}

	if selected != nil {
		atomic.AddInt64(&selected.requestCount, 1)
		selected.lastUsed = time.Now()
		return selected.key
	}

	return ""
}

func (p *KeyPool) ReportSuccess(key string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, k := range p.keys {
		if k.key == key {
			k.isHealthy = true
			return
		}
	}
}

func (p *KeyPool) ReportError(key string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, k := range p.keys {
		if k.key == key {
			atomic.AddInt64(&k.errorCount, 1)
			k.lastError = time.Now()
			// Mark as unhealthy after consecutive errors
			if k.errorCount > 3 {
				k.isHealthy = false
			}
			return
		}
	}
}

func (p *KeyPool) MarkHealthy(key string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, k := range p.keys {
		if k.key == key {
			k.isHealthy = true
			k.errorCount = 0
			return
		}
	}
}

func (p *KeyPool) MarkUnhealthy(key string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, k := range p.keys {
		if k.key == key {
			k.isHealthy = false
			return
		}
	}
}

func (p *KeyPool) GetStats() []KeyStats {
	p.mu.RLock()
	defer p.mu.RUnlock()

	stats := make([]KeyStats, len(p.keys))
	for i, k := range p.keys {
		// Mask the key for security (show only last 8 chars)
		maskedKey := k.key
		if len(maskedKey) > 12 {
			maskedKey = "..." + maskedKey[len(maskedKey)-8:]
		}

		stats[i] = KeyStats{
			Key:          maskedKey,
			RequestCount: atomic.LoadInt64(&k.requestCount),
			ErrorCount:   atomic.LoadInt64(&k.errorCount),
			LastUsed:     k.lastUsed,
			LastError:    k.lastError,
			IsHealthy:    k.isHealthy,
		}
	}

	return stats
}

func (p *KeyPool) Size() int {
	return len(p.keys)
}

func (p *KeyPool) HealthyCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()

	count := 0
	for _, k := range p.keys {
		if k.isHealthy {
			count++
		}
	}
	return count
}

// ResetHealth resets all keys to healthy state (useful for periodic health checks)
func (p *KeyPool) ResetHealth() {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, k := range p.keys {
		k.isHealthy = true
		k.errorCount = 0
	}
}
