package storage

import (
	"context"
	"github.com/caddyserver/caddy/v2/pkg/rate"
	"github.com/caddyserver/caddy/v2/pkg/storage/lru"
	"runtime"
	"strconv"
	"time"

	"github.com/caddyserver/caddy/v2/pkg/config"
	"github.com/caddyserver/caddy/v2/pkg/model"
	"github.com/caddyserver/caddy/v2/pkg/utils"
	"github.com/rs/zerolog/log"
)

type Refresher interface {
	Run()
}

// Refresh is responsible for background refreshing of cache entries.
// It periodically samples random shards and randomly selects "cold" entries
// (from the end of each shard's Storage list) to refreshItem if necessary.
type Refresh struct {
	ctx                 context.Context
	cfg                 *config.Cache
	balancer            lru.Balancer
	scansRateLimiter    *rate.Limiter
	requestRateLimiter  *rate.Limiter
	refreshSuccessNumCh chan struct{}
	refreshErroredNumCh chan struct{}
	refreshItemsCh      chan *model.Response
}

// NewRefresher constructs a Refresh.
func NewRefresher(ctx context.Context, cfg *config.Cache, balancer lru.Balancer) *Refresh {
	return &Refresh{
		ctx:                 ctx,
		cfg:                 cfg,
		balancer:            balancer,
		scansRateLimiter:    rate.NewLimiter(ctx, cfg.Cache.Refresh.ScanRate, cfg.Cache.Refresh.ScanRate/10),
		requestRateLimiter:  rate.NewLimiter(ctx, cfg.Cache.Refresh.Rate, cfg.Cache.Refresh.Rate/10),
		refreshSuccessNumCh: make(chan struct{}, cfg.Cache.Refresh.Rate),        // Successful refreshes counter channel
		refreshErroredNumCh: make(chan struct{}, cfg.Cache.Refresh.Rate),        // Failed refreshes counter channel
		refreshItemsCh:      make(chan *model.Response, cfg.Cache.Refresh.Rate), // Failed refreshes counter channel
	}
}

// Run starts the refresher background loop.
// It runs a logger (if debugging is enabled), spawns a provider for sampling shards,
// and continuously processes shard samples for candidate responses to refreshItem.
func (r *Refresh) Run() {
	r.runLogger()   // handle consumer stats and print logs
	r.runConsumer() // scans rand items and checks whether they should be refreshed
	r.runProducer() // produces items which should be refreshed on processing
}

func (r *Refresh) runProducer() {
	go func() {
		for {
			select {
			case <-r.ctx.Done():
				return
			case <-r.scansRateLimiter.Chan():
				if item := r.balancer.RandNode().RandItem(r.ctx); item.ShouldBeRefreshed() {
					r.refreshItemsCh <- item
				}
			}
		}
	}()
}

func (r *Refresh) runConsumer() {
	go func() {
		for resp := range r.refreshItemsCh {
			r.refreshItem(resp)
		}
	}()
}

// refreshItem attempts to refreshItem the given response via Revalidate.
// If successful, increments the refreshItem metric (in debug mode); otherwise increments the error metric.
func (r *Refresh) refreshItem(resp *model.Response) {
	select {
	case <-r.ctx.Done():
		return
	case <-r.requestRateLimiter.Chan():
		go func() {
			// IMPORTANT: r.ctx used in resp.Revalidate(r.ctx) is a correct ctx due to be able to await requests through previous iterations.
			// Otherwise, you will have a lot of request errors (context cancelled), because in parent method ctx (from arg) has a timeout in milliseconds
			// for be able to stop cycles in current iteration and start a new one.
			if err := resp.Revalidate(r.ctx); err != nil {
				if r.cfg.Cache.Logs.Stats {
					r.refreshErroredNumCh <- struct{}{}
				}
				return
			}
			if r.cfg.Cache.Logs.Stats {
				r.refreshSuccessNumCh <- struct{}{}
			}
		}()
	}
}

// runLogger periodically logs the number of successful and failed refreshItem attempts.
// This runs only if debugging is enabled in the config.
func (r *Refresh) runLogger() {
	if !r.cfg.Cache.Logs.Stats {
		return
	}

	go func() {
		erroredNumPer5Sec := 0
		refreshesNumPer5Sec := 0
		ticker := utils.NewTicker(r.ctx, 5*time.Second)

	loop:
		for {
			select {
			case <-r.ctx.Done():
				return
			case <-r.refreshSuccessNumCh:
				refreshesNumPer5Sec++
				runtime.Gosched()
			case <-r.refreshErroredNumCh:
				erroredNumPer5Sec++
				runtime.Gosched()
			case <-ticker:
				if refreshesNumPer5Sec <= 0 && erroredNumPer5Sec <= 0 {
					runtime.Gosched()
					continue loop
				}

				var (
					errorsNum  = strconv.Itoa(erroredNumPer5Sec)
					successNum = strconv.Itoa(refreshesNumPer5Sec)
				)

				logEvent := log.Info()

				if r.cfg.IsProd() {
					logEvent.
						Str("target", "refresher").
						Str("refreshes", successNum).
						Str("errors", errorsNum)
				}

				logEvent.Msgf("[refresher][5s] updated %s items, errors: %s", successNum, errorsNum)

				refreshesNumPer5Sec = 0
				erroredNumPer5Sec = 0
				runtime.Gosched()
			}
		}
	}()
}
