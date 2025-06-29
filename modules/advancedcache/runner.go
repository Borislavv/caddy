package advancedcache

import (
	"context"
	"github.com/rs/zerolog/log"
)

func (middleware *CacheMiddleware) run(ctx context.Context) error {
	log.Info().Msg("[advanced-cache] starting")

	middleware.ctx = ctx

	if err := middleware.configure(); err != nil {
		return err
	}

	middleware.setUpCache()

	if err := middleware.loadDump(); err != nil {
		log.Error().Err(err).Msg("[dump] failed to load")
	}

	enabledStatStr := "enabled"
	if middleware.cfg.Cache.Logs.Stats == false {
		enabledStatStr = "disabled"
	}
	log.Info().Msg("[logs] stats writing is " + enabledStatStr)

	middleware.store.Run()
	middleware.evictor.Run()
	middleware.refresher.Run()
	middleware.runControllerLogger()

	log.Info().Msg("[advanced-cache] has been started")

	return nil
}
