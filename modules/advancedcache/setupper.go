package advancedcache

import (
	"github.com/caddyserver/caddy/v2/pkg/model"
	"github.com/caddyserver/caddy/v2/pkg/repository"
	"github.com/caddyserver/caddy/v2/pkg/storage"
	"github.com/caddyserver/caddy/v2/pkg/storage/lfu"
	"github.com/caddyserver/caddy/v2/pkg/storage/lru"
	sharded "github.com/caddyserver/caddy/v2/pkg/storage/map"
)

func (middleware *CacheMiddleware) setUpCache() {
	shardedMap := sharded.NewMap[*model.Response](middleware.ctx, middleware.cfg.Cache.Preallocate.PerShard)
	middleware.backend = repository.NewBackend(middleware.cfg)
	balancer := lru.NewBalancer(middleware.ctx, shardedMap)
	middleware.refresher = storage.NewRefresher(middleware.ctx, middleware.cfg, balancer)
	middleware.store = lru.NewStorage(middleware.ctx, middleware.cfg, balancer, middleware.backend, lfu.NewTinyLFU(middleware.ctx), shardedMap)
	middleware.dumper = storage.NewDumper(middleware.cfg, shardedMap, middleware.store, middleware.backend)
	middleware.evictor = storage.NewEvictor(middleware.ctx, middleware.cfg, middleware.store, balancer)
}
