package advancedcache

import (
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/pkg/config"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func (middleware *CacheMiddleware) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	for d.Next() {
		for d.NextBlock(0) {
			switch d.Val() {
			case "config_path":
				if !d.Args(&middleware.ConfigPath) {
					return d.Errf("advancedcache config path expected by found in Caddyfile")
				}
			default:
				return d.Errf("unknown directive: %s", d.Val())
			}
		}
	}
	return nil
}

func (middleware *CacheMiddleware) configure() (err error) {
	log.Info().Msgf("[advanced-cache] loading config by path %s", middleware.ConfigPath)
	if middleware.cfg, err = config.LoadConfig(middleware.ConfigPath); err != nil {
		return err
	}

	log.Info().Msgf("[config] loaded=%+v", middleware.cfg)

	level, err := zerolog.ParseLevel(middleware.cfg.Cache.Logs.Level)
	if err != nil {
		return err
	}
	zerolog.SetGlobalLevel(level)

	middleware.upstreamRateSema = make(chan struct{}, middleware.cfg.Cache.Upstream.Rate)
	for i := 0; i < middleware.cfg.Cache.Upstream.Rate; i++ {
		middleware.upstreamRateSema <- struct{}{}
	}
	middleware.errorCh = make(chan error, middleware.cfg.Cache.Upstream.Rate)

	if middleware.cfg.Cache.Logs.Stats {
		middleware.counterCh = make(chan struct{}, 10_000_000) // it costs 128 bytes with any buffer capacity
	}

	return nil
}
