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
	"net/http"
	"runtime"
	"strings"
	"time"
	"unsafe"
)

var _ caddy.Module = (*CacheMiddleware)(nil)

var (
	immutableEmptyHeader            = make(http.Header)
	contentTypeKey                  = "Content-Type"
	applicationJsonValue            = "application/json"
	lastModifiedKey                 = "Last-Modified"
	serviceTemporaryUnavailableBody = []byte(`{"error":{"message":"Service temporarily unavailable."}}`)
	tooManyRequestsBody             = []byte(`{"error":{"message":"Too many requests to upstream."}}`)
)

const moduleName = "advanced_cache"

func init() {
	caddy.RegisterModule(&CacheMiddleware{})
	httpcaddyfile.RegisterHandlerDirective(moduleName, parseCaddyfile)
}

type CacheMiddleware struct {
	ConfigPath       string
	ctx              context.Context
	cfg              *config.Cache
	store            storage.Storage
	backend          repository.Backender
	refresher        storage.Refresher
	evictor          storage.Evictor
	dumper           storage.Dumper
	upstreamRateSema chan struct{}
	counterCh        chan struct{}
	errorCh          chan error
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

func (middleware *CacheMiddleware) setUpCtxTimeout(r *http.Request) (context.Context, context.CancelFunc) {
	var (
		ctx    context.Context
		cancel context.CancelFunc
	)
loop:
	for headerName, vv := range r.Header {
		for range vv {
			if strings.EqualFold(headerName, middleware.cfg.Cache.LifeTime.EscapeMaxReqDurationHeader) {
				ctx, cancel = context.WithTimeout(r.Context(), time.Minute)
				break loop
			}
		}
	}
	if ctx == nil {
		ctx, cancel = context.WithTimeout(r.Context(), middleware.cfg.Cache.LifeTime.MaxReqDuration)
	}
	r.WithContext(ctx)
	return ctx, cancel
}

func (middleware *CacheMiddleware) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	defer runtime.Gosched()

	w.Header().Add(contentTypeKey, applicationJsonValue)

	_, cancel := middleware.setUpCtxTimeout(r)
	defer cancel()

	// Build request (return error on rule missing for current path)
	req, err := model.NewRequestFromNetHttp(middleware.cfg, r)
	if err != nil {
		// If path does not match then process request manually without cache
		return next.ServeHTTP(w, r)
	}

	resp, isHit := middleware.store.Get(req)
	if !isHit {
		captured := newCaptureResponseWriter(w)

		select {
		case s := <-middleware.upstreamRateSema:
			defer func() { middleware.upstreamRateSema <- s }()

			// Handle request manually due to store it
			if srvErr := next.ServeHTTP(captured, r); srvErr != nil {
				middleware.errorCh <- srvErr
				captured.body.Reset()
				captured.headers = immutableEmptyHeader
				captured.WriteHeader(captured.statusCode)
				_, _ = captured.Write(serviceTemporaryUnavailableBody)
			}
		default:
			captured.body.Reset()
			captured.headers = immutableEmptyHeader
			captured.WriteHeader(http.StatusTooManyRequests)
			_, _ = captured.Write(tooManyRequestsBody)
		}

		// Build new response
		path := unsafe.Slice(unsafe.StringData(r.URL.Path), len(r.URL.Path))
		data := model.NewData(middleware.cfg, path, captured.statusCode, captured.headers, captured.body.Bytes())
		resp, _ = model.NewResponse(data, req, middleware.cfg, middleware.backend.RevalidatorMaker(req))

		// Store response in cache
		middleware.store.Set(resp)
	} else {
		// Write status code on hit
		w.WriteHeader(resp.Data().StatusCode())

		// Write response data
		_, _ = w.Write(resp.Data().Body())

		// Apply custom http headers
		for key, vv := range resp.Data().Headers() {
			for _, value := range vv {
				w.Header().Add(key, value)
			}
		}
	}

	// Apply standard http headers
	w.Header().Add(lastModifiedKey, resp.RevalidatedAt().Format(http.TimeFormat))

	// Record the duration in debug mode for metrics.
	if middleware.cfg.Cache.Logs.Stats {
		middleware.counterCh <- struct{}{}
	}

	return err
}
