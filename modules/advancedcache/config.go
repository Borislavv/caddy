package advancedcache

import (
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/pkg/config"
)

func (middleware *CacheMiddleware) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	for d.Next() {
		for d.NextBlock(0) {
			switch d.Val() {
			case "env":
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

func (middleware *CacheMiddleware) loadConfig() (err error) {
	if middleware.cfg, err = config.LoadConfig(middleware.ConfigPath); err != nil {
		return err
	}
	return nil
}
