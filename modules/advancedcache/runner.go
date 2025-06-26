package advancedcache

import (
	"context"
	"github.com/rs/zerolog/log"
	"os"
	"os/signal"
	"syscall"
)

func (middleware *CacheMiddleware) run(ctx context.Context) error {
	log.Info().Msg("[advanced-cache] starting")
	defer log.Info().Msg("[advanced-cache] has been started")

	middleware.ctx, middleware.cancel = context.WithCancel(ctx)
	go func() {
		defer middleware.cancel()
		osSignals := make(chan os.Signal, 1)
		signal.Notify(osSignals, syscall.SIGINT, syscall.SIGTERM)
		<-osSignals
	}()

	if err := middleware.loadConfig(); err != nil {
		return err
	}

	middleware.setUpCache()
	middleware.store.Run()
	middleware.evictor.Run()
	middleware.refresher.Run()
	middleware.runControllerLogger()

	return nil
}
