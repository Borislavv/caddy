package advancedcache

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"github.com/caddyserver/caddy/v2/pkg/config"
	"github.com/caddyserver/caddy/v2/pkg/model"
	"github.com/caddyserver/caddy/v2/pkg/repository"
	"github.com/caddyserver/caddy/v2/pkg/storage"
	"github.com/caddyserver/caddy/v2/pkg/storage/lfu"
	"github.com/caddyserver/caddy/v2/pkg/storage/lru"
	sharded "github.com/caddyserver/caddy/v2/pkg/storage/map"
	"github.com/joho/godotenv"
	"github.com/rs/zerolog/log"
	"github.com/spf13/viper"
	"go.uber.org/zap"
)

var (
	ctx        context.Context
	cancel     context.CancelFunc
	cfg        *config.Cache
	store      *lru.Storage
	shardedMap *sharded.Map[*model.Response]
)

func init() {
	caddy.RegisterModule(&Cache{})
	httpcaddyfile.RegisterHandlerDirective("http_cache", parseCaddyfile)

	if err := godotenv.Overload(".env", ".env.local"); err != nil {
		panic(err)
	}

	viper.AutomaticEnv()
	_ = viper.BindEnv("APP_ENV")
	_ = viper.BindEnv("FASTHTTP_SERVER_NAME")
	_ = viper.BindEnv("FASTHTTP_SERVER_PORT")
	_ = viper.BindEnv("FASTHTTP_SERVER_SHUTDOWN_TIMEOUT")
	_ = viper.BindEnv("FASTHTTP_SERVER_REQUEST_TIMEOUT")
	_ = viper.BindEnv("LIVENESS_PROBE_TIMEOUT")
	_ = viper.BindEnv("IS_PROMETHEUS_METRICS_ENABLED")

	cfg = loadCfg()

	ctx, cancel = context.WithCancel(context.Background())

	shardedMap = sharded.NewMap[*model.Response](ctx, cfg.Cache.Preallocate.PerShard)
	backend := repository.NewBackend(cfg)
	balancer := lru.NewBalancer(ctx, shardedMap)
	refresher := storage.NewRefresher(ctx, cfg, balancer)
	tinyLFU := lfu.NewTinyLFU(ctx)
	store = lru.NewStorage(ctx, cfg, balancer, backend, tinyLFU, shardedMap)
	dumper := storage.NewDumper(cfg, shardedMap, store, backend)
	evictor := storage.NewEvictor(ctx, cfg, store, balancer)

	if err := dumper.Load(ctx); err != nil {
		log.Error().Err(err).Msg("[dump] failed to load")
	}

	go func() {
		osSigsCh := make(chan os.Signal, 1)
		signal.Notify(osSigsCh, os.Interrupt)
		<-osSigsCh
		cancel()

		dCtx, dCancel := context.WithTimeout(context.Background(), 9*time.Second)
		defer dCancel()

		if err := dumper.Dump(dCtx); err != nil {
			log.Error().Err(err).Msg("[dump] failed to store dump")
		}
	}()

	store.Run()
	evictor.Run()
	refresher.Run()
}

func loadCfg() *config.Cache {
	cacheCfg, err := config.LoadConfig()
	if err != nil {
		log.Error().Err(err).Msg("[main] failed to load config from envs")
		panic(err)
	}
	log.Printf("cfg=%+v, viperEntries=%+v", cacheCfg, viper.AllSettings())
	return cacheCfg
}

var _ caddy.Module = (*Cache)(nil)

type Cache struct {
	logger *zap.Logger
}

func (*Cache) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "http.handlers.http_cache",
		New: func() caddy.Module { return new(Cache) },
	}
}

func (m *Cache) Provision(reqCtx caddy.Context) error {
	ctx, cancel = context.WithCancel(reqCtx.Context)
	m.logger = reqCtx.Logger(m)
	m.logger.Info("cache provisioning")
	return nil
}

type captureRW struct {
	header      http.Header
	body        bytes.Buffer
	status      int
	wroteHeader bool
}

func newCaptureRW() *captureRW {
	return &captureRW{
		header: make(http.Header),
		status: http.StatusOK,
	}
}

func (c *captureRW) Header() http.Header {
	return c.header
}

func (c *captureRW) WriteHeader(code int) {
	if !c.wroteHeader {
		c.status = code
		c.wroteHeader = true
	}
}

func (c *captureRW) Write(b []byte) (int, error) {
	if !c.wroteHeader {
		c.WriteHeader(http.StatusOK)
	}
	return c.body.Write(b)
}

func (c *captureRW) flush(w http.ResponseWriter) {
	for k, vals := range c.header {
		for _, v := range vals {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(c.status)
	_, _ = w.Write(c.body.Bytes())
}

func (m *Cache) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	defer runtime.Gosched()

	w.Header().Add("Content-Type", "application/json")

	req, err := model.NewRequestFromNetHttp(cfg, r)
	if err != nil {
		return next.ServeHTTP(w, r)
	}

	resp, isHit := store.Get(req)
	if isHit {
		for key, vv := range resp.Data().Headers() {
			for _, value := range vv {
				w.Header().Add(key, value)
			}
		}
		w.Header().Add("Last-Modified", resp.RevalidatedAt().Format(http.TimeFormat))
		w.WriteHeader(resp.Data().StatusCode())
		_, _ = w.Write(resp.Data().Body())
		return nil
	}

	captured := newCaptureRW()
	err = next.ServeHTTP(captured, r)

	if err != nil {
		captured.header = make(http.Header)
		captured.body.Reset()
		captured.status = http.StatusServiceUnavailable
		captured.WriteHeader(captured.status)
		_, _ = captured.Write([]byte(`{"error":{"message":"Service temporarily unavailable"}}`))
	}

	path := []byte(r.URL.Path)

	data := model.NewData(
		cfg,
		path,
		captured.status,
		captured.header,
		captured.body.Bytes(),
	)

	resp, _ = model.NewResponse(data, req, cfg, func(ctx context.Context) (data *model.Data, err error) {
		return m.requestBackend(r, next)
	})

	store.Set(resp)
	w.Header().Add("Last-Modified", resp.RevalidatedAt().Format(http.TimeFormat))

	captured.flush(w)

	return err
}

func (m *Cache) requestBackend(r *http.Request, next caddyhttp.Handler) (*model.Data, error) {
	captured := newCaptureRW()
	if err := next.ServeHTTP(captured, r); err != nil {
		return nil, err
	}

	path := []byte(r.URL.Path)

	fmt.Println("after next.ServeHTTP")

	return model.NewData(
		cfg,
		path,
		captured.status,
		captured.header,
		captured.body.Bytes(),
	), nil
}

func (m *Cache) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	return nil
}

func parseCaddyfile(h httpcaddyfile.Helper) (caddyhttp.MiddlewareHandler, error) {
	var m Cache
	if err := m.UnmarshalCaddyfile(h.Dispenser); err != nil {
		return nil, err
	}
	return &m, nil
}
