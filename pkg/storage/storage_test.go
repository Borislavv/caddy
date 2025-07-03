package storage

import (
	"context"
	"github.com/caddyserver/caddy/v2/pkg/storage/lfu"
	"github.com/caddyserver/caddy/v2/pkg/storage/lru"
	"runtime"
	"testing"
	"time"

	"github.com/caddyserver/caddy/v2/pkg/config"
	"github.com/caddyserver/caddy/v2/pkg/mock"
	"github.com/caddyserver/caddy/v2/pkg/model"
	"github.com/caddyserver/caddy/v2/pkg/repository"
	sharded "github.com/caddyserver/caddy/v2/pkg/storage/map"
	"github.com/rs/zerolog"
)

var cfg *config.Cache

func init() {
	cfg = &config.Cache{
		Cache: config.CacheBox{
			Enabled: true,
			LifeTime: config.Lifetime{
				MaxReqDuration:             time.Millisecond * 100,
				EscapeMaxReqDurationHeader: "X-Target-Bot",
			},
			Upstream: config.Upstream{
				Url:     "https://google.com",
				Rate:    1000,
				Timeout: time.Second * 5,
			},
			Preallocate: config.Preallocation{
				PerShard: 8,
			},
			Eviction: config.Eviction{
				Policy:    "lru",
				Threshold: 0.9,
			},
			Refresh: config.Refresh{
				TTL:      time.Hour,
				ErrorTTL: time.Minute * 10,
				Beta:     0.4,
				MinStale: time.Minute * 40,
			},
			Storage: config.Storage{
				Type: "malloc",
				Size: 1024 * 1024 * 5, // 5 MB
			},
			Rules: []*config.Rule{
				{
					Path:      "/api/v2/pagedata",
					PathBytes: []byte("/api/v2/pagedata"),
					TTL:       time.Hour,
					ErrorTTL:  time.Minute * 15,
					Beta:      0.4,
					MinStale:  time.Duration(float64(time.Hour) * 0.4),
					CacheKey: config.Key{
						Query:        []string{"project[id]", "domain", "language", "choice"},
						QueryBytes:   [][]byte{[]byte("project[id]"), []byte("domain"), []byte("language"), []byte("choice")},
						Headers:      []string{"Accept-Encoding", "Accept-Language"},
						HeadersBytes: [][]byte{[]byte("Accept-Encoding"), []byte("Accept-Language")},
					},
					CacheValue: config.Value{
						Headers:      []string{"Content-Type", "Vary"},
						HeadersBytes: [][]byte{[]byte("Content-Type"), []byte("Vary")},
					},
				},
			},
		},
	}

	zerolog.SetGlobalLevel(zerolog.ErrorLevel)
}

func reportMemAndAdvancedCache(b *testing.B, usageMem int64) {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	b.ReportMetric(float64(mem.Alloc)/1024/1024, "allocsMB")
	b.ReportMetric(float64(usageMem)/1024/1024, "advancedCacheMB")
}

func BenchmarkReadFromStorage1000TimesPerIter(b *testing.B) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	shardedMap := sharded.NewMap[*model.Response](ctx, cfg.Cache.Preallocate.PerShard)
	balancer := lru.NewBalancer(ctx, shardedMap)
	backend := repository.NewBackend(cfg)
	tinyLFU := lfu.NewTinyLFU(ctx)
	db := lru.NewStorage(ctx, cfg, balancer, backend, tinyLFU, shardedMap)

	responses := mock.GenerateRandomResponses(cfg, path, b.N+1)
	for _, resp := range responses {
		db.Set(resp)
	}
	length := len(responses)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			for j := 0; j < 1000; j++ {
				_, _ = db.Get(responses[(i*j)%length].Request())
			}
			i += 1000
		}
	})
	b.StopTimer()

	reportMemAndAdvancedCache(b, shardedMap.Mem())
}

func BenchmarkWriteIntoStorage1000TimesPerIter(b *testing.B) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	shardedMap := sharded.NewMap[*model.Response](ctx, cfg.Cache.Preallocate.PerShard)
	balancer := lru.NewBalancer(ctx, shardedMap)
	backend := repository.NewBackend(cfg)
	tinyLFU := lfu.NewTinyLFU(ctx)
	db := lru.NewStorage(ctx, cfg, balancer, backend, tinyLFU, shardedMap)

	responses := mock.GenerateRandomResponses(cfg, path, b.N+1)
	length := len(responses)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			for j := 0; j < 1000; j++ {
				db.Set(responses[(i*j)%length])
			}
			i += 1000
		}
	})
	b.StopTimer()

	reportMemAndAdvancedCache(b, shardedMap.Mem())
}

func BenchmarkGetAllocs(b *testing.B) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	shardedMap := sharded.NewMap[*model.Response](ctx, cfg.Cache.Preallocate.PerShard)
	balancer := lru.NewBalancer(ctx, shardedMap)
	backend := repository.NewBackend(cfg)
	tinyLFU := lfu.NewTinyLFU(ctx)
	db := lru.NewStorage(ctx, cfg, balancer, backend, tinyLFU, shardedMap)

	resp := mock.GenerateRandomResponses(cfg, path, 1)[0]
	db.Set(resp)
	req := resp.Request()

	allocs := testing.AllocsPerRun(100_000, func() {
		db.Get(req)
	})
	b.ReportMetric(allocs, "allocs/op")

	reportMemAndAdvancedCache(b, shardedMap.Mem())
}

func BenchmarkSetAllocs(b *testing.B) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	shardedMap := sharded.NewMap[*model.Response](ctx, cfg.Cache.Preallocate.PerShard)
	balancer := lru.NewBalancer(ctx, shardedMap)
	backend := repository.NewBackend(cfg)
	tinyLFU := lfu.NewTinyLFU(ctx)
	db := lru.NewStorage(ctx, cfg, balancer, backend, tinyLFU, shardedMap)

	resp := mock.GenerateRandomResponses(cfg, path, 1)[0]

	allocs := testing.AllocsPerRun(100_000, func() {
		db.Set(resp)
	})
	b.ReportMetric(allocs, "allocs/op")

	reportMemAndAdvancedCache(b, shardedMap.Mem())
}
