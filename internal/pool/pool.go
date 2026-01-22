package pool

import (
	"crypto/tls"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// PoolConfig holds configuration for the connection pool
type PoolConfig struct {
	MaxIdleConns        int           `mapstructure:"max_idle_conns"`
	MaxIdleConnsPerHost int           `mapstructure:"max_idle_conns_per_host"`
	IdleConnTimeout     time.Duration `mapstructure:"idle_conn_timeout"`
	MaxClients          int           `mapstructure:"max_clients"`
	ClientIdleTTL       time.Duration `mapstructure:"client_idle_ttl"`
	ResponseTimeout     time.Duration `mapstructure:"response_timeout"`
}

// DefaultPoolConfig returns the default pool configuration
func DefaultPoolConfig() PoolConfig {
	return PoolConfig{
		MaxIdleConns:        240,
		MaxIdleConnsPerHost: 120,
		IdleConnTimeout:     90 * time.Second,
		MaxClients:          5000,
		ClientIdleTTL:       15 * time.Minute,
		ResponseTimeout:     10 * time.Minute,
	}
}

// Pool manages HTTP clients with connection pooling
type Pool interface {
	// GetClient returns an HTTP client for the given account
	GetClient(accountID string) *http.Client
	// Do executes a request using the appropriate client
	Do(req *http.Request, accountID string) (*http.Response, error)
	// Stats returns pool statistics
	Stats() PoolStats
	// Close closes all clients in the pool
	Close()
}

// PoolStats contains pool statistics
type PoolStats struct {
	TotalClients int `json:"total_clients"`
	ActiveConns  int `json:"active_conns"`
	IdleConns    int `json:"idle_conns"`
}

// clientEntry represents a cached client with metadata
type clientEntry struct {
	client     *http.Client
	transport  *http.Transport
	accountID  string
	createdAt  time.Time
	lastUsedAt time.Time
}

// HTTPPool implements Pool with LRU eviction
type HTTPPool struct {
	config  PoolConfig
	clients map[string]*clientEntry
	order   []string // LRU order tracking
	mu      sync.RWMutex
	closed  bool

	// Shared transport for accounts without specific config
	sharedTransport *http.Transport
	sharedClient    *http.Client
}

// NewHTTPPool creates a new HTTP connection pool
func NewHTTPPool(config PoolConfig) *HTTPPool {
	// Create shared transport with HTTP/2 support
	sharedTransport := createTransport(config)

	pool := &HTTPPool{
		config:          config,
		clients:         make(map[string]*clientEntry),
		order:           make([]string, 0),
		sharedTransport: sharedTransport,
		sharedClient: &http.Client{
			Transport: sharedTransport,
			Timeout:   config.ResponseTimeout,
		},
	}

	// Start cleanup goroutine
	go pool.cleanup()

	return pool
}

// createTransport creates an HTTP transport with HTTP/2 support
func createTransport(config PoolConfig) *http.Transport {
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true, // Enables HTTP/2 automatically for HTTPS
		MaxIdleConns:          config.MaxIdleConns,
		MaxIdleConnsPerHost:   config.MaxIdleConnsPerHost,
		IdleConnTimeout:       config.IdleConnTimeout,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
		},
	}

	return transport
}

// GetClient returns an HTTP client for the given account
func (p *HTTPPool) GetClient(accountID string) *http.Client {
	if accountID == "" {
		return p.sharedClient
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return p.sharedClient
	}

	// Check if client exists
	if entry, ok := p.clients[accountID]; ok {
		entry.lastUsedAt = time.Now()
		p.moveToFront(accountID)
		return entry.client
	}

	// Create new client
	transport := createTransport(p.config)
	client := &http.Client{
		Transport: transport,
		Timeout:   p.config.ResponseTimeout,
	}

	entry := &clientEntry{
		client:     client,
		transport:  transport,
		accountID:  accountID,
		createdAt:  time.Now(),
		lastUsedAt: time.Now(),
	}

	// Evict if at capacity
	for len(p.clients) >= p.config.MaxClients && len(p.order) > 0 {
		p.evictOldest()
	}

	p.clients[accountID] = entry
	p.order = append([]string{accountID}, p.order...)

	log.Debug().Str("account_id", accountID).Int("pool_size", len(p.clients)).Msg("created new client")

	return client
}

// Do executes a request using the appropriate client
func (p *HTTPPool) Do(req *http.Request, accountID string) (*http.Response, error) {
	client := p.GetClient(accountID)
	return client.Do(req)
}

// Stats returns pool statistics
func (p *HTTPPool) Stats() PoolStats {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return PoolStats{
		TotalClients: len(p.clients),
	}
}

// Close closes all clients in the pool
func (p *HTTPPool) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.closed = true

	for _, entry := range p.clients {
		entry.transport.CloseIdleConnections()
	}

	p.sharedTransport.CloseIdleConnections()
	p.clients = make(map[string]*clientEntry)
	p.order = nil

	log.Info().Msg("connection pool closed")
}

// moveToFront moves an account to the front of the LRU list
func (p *HTTPPool) moveToFront(accountID string) {
	for i, id := range p.order {
		if id == accountID {
			// Remove from current position
			p.order = append(p.order[:i], p.order[i+1:]...)
			// Add to front
			p.order = append([]string{accountID}, p.order...)
			return
		}
	}
}

// evictOldest removes the oldest entry from the pool
func (p *HTTPPool) evictOldest() {
	if len(p.order) == 0 {
		return
	}

	// Get oldest (last in list)
	oldestID := p.order[len(p.order)-1]
	p.order = p.order[:len(p.order)-1]

	if entry, ok := p.clients[oldestID]; ok {
		entry.transport.CloseIdleConnections()
		delete(p.clients, oldestID)
		log.Debug().Str("account_id", oldestID).Msg("evicted client from pool")
	}
}

// cleanup periodically removes idle clients
func (p *HTTPPool) cleanup() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		p.mu.Lock()
		if p.closed {
			p.mu.Unlock()
			return
		}

		now := time.Now()
		var toEvict []string

		for id, entry := range p.clients {
			if now.Sub(entry.lastUsedAt) > p.config.ClientIdleTTL {
				toEvict = append(toEvict, id)
			}
		}

		for _, id := range toEvict {
			if entry, ok := p.clients[id]; ok {
				entry.transport.CloseIdleConnections()
				delete(p.clients, id)
			}
			// Remove from order
			for i, orderID := range p.order {
				if orderID == id {
					p.order = append(p.order[:i], p.order[i+1:]...)
					break
				}
			}
		}

		if len(toEvict) > 0 {
			log.Debug().Int("evicted", len(toEvict)).Int("remaining", len(p.clients)).Msg("cleaned up idle clients")
		}

		p.mu.Unlock()
	}
}
