package service

import (
	"context"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	"ccproxy/internal/store"
)

const (
	DefaultAggregationInterval = 24 * time.Hour
)

type StatsAggregator struct {
	store    *store.Store
	interval time.Duration
	ticker   *time.Ticker
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	mu       sync.Mutex
	running  bool
}

// NewStatsAggregator creates a new statistics aggregator
func NewStatsAggregator(store *store.Store, interval time.Duration) *StatsAggregator {
	if interval <= 0 {
		interval = DefaultAggregationInterval
	}

	return &StatsAggregator{
		store:    store,
		interval: interval,
	}
}

// Start starts the stats aggregator background task
func (sa *StatsAggregator) Start(ctx context.Context) error {
	sa.mu.Lock()
	defer sa.mu.Unlock()

	if sa.running {
		return nil
	}

	sa.ctx, sa.cancel = context.WithCancel(ctx)
	sa.ticker = time.NewTicker(sa.interval)
	sa.running = true

	// Run aggregation immediately on start for yesterday's data
	go func() {
		if err := sa.runAggregation(); err != nil {
			log.Error().Err(err).Msg("Initial stats aggregation failed")
		}
	}()

	// Start background worker
	sa.wg.Add(1)
	go sa.worker()

	log.Info().Dur("interval", sa.interval).Msg("Stats aggregator started")
	return nil
}

// Stop stops the stats aggregator
func (sa *StatsAggregator) Stop() error {
	sa.mu.Lock()
	if !sa.running {
		sa.mu.Unlock()
		return nil
	}
	sa.running = false
	sa.mu.Unlock()

	log.Info().Msg("Stopping stats aggregator...")

	// Cancel context and stop ticker
	sa.cancel()
	sa.ticker.Stop()

	// Wait for worker to finish
	sa.wg.Wait()

	log.Info().Msg("Stats aggregator stopped")
	return nil
}

// worker runs the aggregation task periodically
func (sa *StatsAggregator) worker() {
	defer sa.wg.Done()

	for {
		select {
		case <-sa.ticker.C:
			if err := sa.runAggregation(); err != nil {
				log.Error().Err(err).Msg("Stats aggregation failed")
			}
		case <-sa.ctx.Done():
			return
		}
	}
}

// runAggregation aggregates statistics for yesterday
func (sa *StatsAggregator) runAggregation() error {
	start := time.Now()

	// Aggregate yesterday's data
	yesterday := time.Now().AddDate(0, 0, -1).Truncate(24 * time.Hour)
	yesterdayStr := yesterday.Format("2006-01-02")

	log.Info().Str("date", yesterdayStr).Msg("Running stats aggregation")

	// Aggregation query
	query := `
		INSERT OR REPLACE INTO usage_stats_daily (
			stat_date, token_id, account_id, mode, model,
			request_count, success_count, error_count,
			total_prompt_tokens, total_completion_tokens, total_tokens,
			avg_duration_ms, avg_ttft_ms, created_at
		)
		SELECT
			DATE(request_at) as stat_date,
			token_id,
			account_id,
			mode,
			model,
			COUNT(*) as request_count,
			SUM(CASE WHEN success = 1 THEN 1 ELSE 0 END) as success_count,
			SUM(CASE WHEN success = 0 THEN 1 ELSE 0 END) as error_count,
			SUM(prompt_tokens) as total_prompt_tokens,
			SUM(completion_tokens) as total_completion_tokens,
			SUM(total_tokens) as total_tokens,
			AVG(duration_ms) as avg_duration_ms,
			AVG(ttft_ms) as avg_ttft_ms,
			datetime('now') as created_at
		FROM request_logs
		WHERE DATE(request_at) = ?
		GROUP BY DATE(request_at), token_id, account_id, mode, model
	`

	result, err := sa.store.GetDB().Exec(query, yesterdayStr)
	if err != nil {
		return err
	}

	rowsAffected, _ := result.RowsAffected()
	duration := time.Since(start)

	log.Info().
		Str("date", yesterdayStr).
		Int64("rows_affected", rowsAffected).
		Dur("duration", duration).
		Msg("Stats aggregation completed")

	return nil
}

// AggregateDate aggregates statistics for a specific date (manual trigger)
func (sa *StatsAggregator) AggregateDate(date time.Time) error {
	dateStr := date.Format("2006-01-02")
	log.Info().Str("date", dateStr).Msg("Running manual stats aggregation")

	query := `
		INSERT OR REPLACE INTO usage_stats_daily (
			stat_date, token_id, account_id, mode, model,
			request_count, success_count, error_count,
			total_prompt_tokens, total_completion_tokens, total_tokens,
			avg_duration_ms, avg_ttft_ms, created_at
		)
		SELECT
			DATE(request_at) as stat_date,
			token_id,
			account_id,
			mode,
			model,
			COUNT(*) as request_count,
			SUM(CASE WHEN success = 1 THEN 1 ELSE 0 END) as success_count,
			SUM(CASE WHEN success = 0 THEN 1 ELSE 0 END) as error_count,
			SUM(prompt_tokens) as total_prompt_tokens,
			SUM(completion_tokens) as total_completion_tokens,
			SUM(total_tokens) as total_tokens,
			AVG(duration_ms) as avg_duration_ms,
			AVG(ttft_ms) as avg_ttft_ms,
			datetime('now') as created_at
		FROM request_logs
		WHERE DATE(request_at) = ?
		GROUP BY DATE(request_at), token_id, account_id, mode, model
	`

	result, err := sa.store.GetDB().Exec(query, dateStr)
	if err != nil {
		return err
	}

	rowsAffected, _ := result.RowsAffected()
	log.Info().Str("date", dateStr).Int64("rows_affected", rowsAffected).Msg("Manual aggregation completed")

	return nil
}

// AggregateRange aggregates statistics for a date range (backfill)
func (sa *StatsAggregator) AggregateRange(fromDate, toDate time.Time) error {
	log.Info().
		Str("from", fromDate.Format("2006-01-02")).
		Str("to", toDate.Format("2006-01-02")).
		Msg("Running range stats aggregation")

	current := fromDate
	for !current.After(toDate) {
		if err := sa.AggregateDate(current); err != nil {
			log.Error().Err(err).Str("date", current.Format("2006-01-02")).Msg("Failed to aggregate date")
		}
		current = current.AddDate(0, 0, 1)
	}

	log.Info().Msg("Range aggregation completed")
	return nil
}
