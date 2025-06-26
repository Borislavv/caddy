package advancedcache

import (
	"context"
	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"github.com/caddyserver/caddy/v2/pkg/config"
	"github.com/caddyserver/caddy/v2/pkg/model"
	"github.com/caddyserver/caddy/v2/pkg/repository"
	"github.com/caddyserver/caddy/v2/pkg/storage"
	"github.com/rs/zerolog/log"
	"net/http"
	"runtime"
	"runtime/debug"
	"sync/atomic"
	"time"
	"unsafe"
)

var _ caddy.Module = (*CacheMiddleware)(nil)

const moduleName = "advancedcache"

func init() {
	caddy.RegisterModule(&CacheMiddleware{})
	httpcaddyfile.RegisterHandlerDirective(moduleName, parseCaddyfile)
}

type CacheMiddleware struct {
	Env       string
	ctx       context.Context
	cancel    context.CancelFunc
	cfg       *config.Cache
	store     storage.Storage
	backend   repository.Backender
	refresher storage.Refresher
	evictor   storage.Evictor
	dumper    storage.Dumper
	count     int64 // Num
	duration  int64 // UnixNano
}

func (*CacheMiddleware) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "http.handlers." + moduleName,
		New: func() caddy.Module { return new(CacheMiddleware) },
	}
}

func (middleware *CacheMiddleware) Provision(ctx caddy.Context) error {
	return middleware.run(ctx.Context)
}

func (middleware *CacheMiddleware) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	defer func() {
		if rec := recover(); rec != nil {
			log.Error().
				Interface("panic", rec).
				Bytes("stack", debug.Stack()).
				Msg("[advanced-cache] panic recovered")
			http.Error(w, "Internal Server Error.", http.StatusInternalServerError)
		}
		runtime.Gosched()
	}()

	from := time.Now()
	if middleware.cfg == nil {
		w.Header().Add("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":{"message":"Service temporarily unavailable."}}`))
		return nil
	}

	req, err := model.NewRequestFromNetHttp(middleware.cfg, r)
	if err != nil {
		return next.ServeHTTP(w, r)
	}

	resp, isHit := middleware.store.Get(req)
	if !isHit {
		captured := newCaptureRW()
		if srvErr := next.ServeHTTP(captured, r); srvErr != nil {
			captured.header = make(http.Header)
			captured.body.Reset()
			captured.status = http.StatusServiceUnavailable
			captured.WriteHeader(captured.status)
			_, _ = captured.Write([]byte(`{"error":{"message":"Service temporarily unavailable."}}`))
		}

		path := unsafe.Slice(unsafe.StringData(r.URL.Path), len(r.URL.Path))
		data := model.NewData(middleware.cfg, path, captured.status, captured.header, captured.body.Bytes())
		resp, _ = model.NewResponse(data, req, middleware.cfg, middleware.backend.RevalidatorMaker(req))
		middleware.store.Set(resp)
	}

	for key, vv := range resp.Data().Headers() {
		for _, value := range vv {
			w.Header().Add(key, value)
		}
	}

	w.Header().Add("Content-Type", "application/json")
	w.Header().Add("Last-Modified", resp.RevalidatedAt().Format(http.TimeFormat))
	w.WriteHeader(resp.Data().StatusCode())
	_, _ = w.Write(resp.Data().Body())

	// Record the duration in debug mode for metrics.
	atomic.AddInt64(&middleware.count, 1)
	atomic.AddInt64(&middleware.duration, time.Since(from).Nanoseconds())

	return err
}
