package model

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"github.com/caddyserver/caddy/v2/pkg/config"
	"github.com/caddyserver/caddy/v2/pkg/list"
	"math"
	"math/rand/v2"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"
)

const gzipThreshold = 1024 // Minimum body size to apply compress compression

// -- Internal pools for efficient memory management --

var (
	GzipBufferPool = &sync.Pool{New: func() any { return new(bytes.Buffer) }}
	GzipWriterPool = &sync.Pool{New: func() any {
		w, err := gzip.NewWriterLevel(nil, gzip.BestSpeed)
		if err != nil {
			panic("failed to Init. compress writer: " + err.Error())
		}
		return w
	}}
)

// Response is the main cache object, holding the request, payload, metadata, and list pointers.
type Response struct {
	cfg           *config.Cache                                     // Immutable field
	request       *atomic.Pointer[Request]                          // Associated Key
	data          *atomic.Pointer[Data]                             // Cached data
	lruListElem   *atomic.Pointer[list.Element[*Response]]          // Pointer for LRU list (per-shard)
	revalidator   func(ctx context.Context) (data *Data, err error) // Closure for refresh/revalidation
	weight        int64                                             // Response weight in bytes
	revalidatedAt int64                                             // Last revalidated time (nanoseconds since epoch)
}

// NewResponse constructs a new Response using memory pools and sets up all fields.
func NewResponse(
	data *Data, req *Request, cfg *config.Cache,
	revalidator func(ctx context.Context) (data *Data, err error),
) (*Response, error) {
	return new(Response).Init().SetUp(cfg, data, req, revalidator), nil
}

// Init ensures all pointers are non-nil after pool Get.
func (r *Response) Init() *Response {
	if r.request == nil {
		r.request = &atomic.Pointer[Request]{}
	}
	if r.data == nil {
		r.data = &atomic.Pointer[Data]{}
	}
	if r.lruListElem == nil {
		r.lruListElem = &atomic.Pointer[list.Element[*Response]]{}
	}
	return r
}

// SetUp stores the Data, Request, and config-driven fields into the Response.
func (r *Response) SetUp(
	cfg *config.Cache,
	data *Data,
	req *Request,
	revalidator func(ctx context.Context) (data *Data, err error),
) *Response {
	r.cfg = cfg
	r.data.Store(data)
	r.request.Store(req)
	r.revalidator = revalidator
	r.revalidatedAt = time.Now().UnixNano()
	r.weight = r.setUpWeight()
	return r
}

// --- Response API ---

func (r *Response) Touch() *Response {
	atomic.StoreInt64(&r.revalidatedAt, time.Now().UnixNano())
	return r
}

// ToQuery returns the query representation of the request.
func (r *Response) ToQuery() []byte {
	return r.request.Load().ToQuery()
}

// MapKey returns the key of the associated request.
func (r *Response) MapKey() uint64 {
	return r.request.Load().MapKey()
}

// ShardKey returns the shard key of the associated request.
func (r *Response) ShardKey() uint64 {
	return r.request.Load().ShardKey()
}

// ShouldBeRefreshed implements probabilistic refresh logic ("beta" algorithm).
// Returns true if the entry is stale and, with a probability proportional to its staleness, should be refreshed now.
func (r *Response) ShouldBeRefreshed() bool {
	if r == nil {
		return false
	}

	var (
		beta          float64
		interval      time.Duration
		minStale      time.Duration
		revalidatedAt = atomic.LoadInt64(&r.revalidatedAt)
	)

	req := r.request.Load()
	if req.rule != nil {
		beta = req.rule.Beta
		interval = req.rule.TTL
		minStale = req.rule.MinStale
	}

	const eps = 1e-9
	if math.Abs(beta) < eps { // safe check that float64 is zero
		beta = r.cfg.Cache.Refresh.Beta
	}

	if interval == 0 {
		interval = r.cfg.Cache.Refresh.TTL
	}
	if minStale == 0 {
		minStale = r.cfg.Cache.Refresh.MinStale
	}

	if r.data.Load().statusCode != http.StatusOK {
		interval = interval / 10 // On stale will be used 10% of origin interval.
		minStale = minStale / 10 // On stale will be used 10% of origin stale duration.
	}

	// hard check that min
	if age := time.Since(time.Unix(0, revalidatedAt)).Nanoseconds(); age > minStale.Nanoseconds() {
		return rand.Float64() >= math.Exp((-beta)*float64(age)/float64(interval))
	}

	return false
}

// Revalidate calls the revalidator closure to fetch fresh data and updates the timestamp.
func (r *Response) Revalidate(ctx context.Context) error {
	data, err := r.revalidator(ctx)
	if err != nil {
		return err
	}

	r.data.Store(data)
	atomic.AddInt64(&r.weight, data.Weight()-r.data.Load().Weight())
	atomic.StoreInt64(&r.revalidatedAt, time.Now().UnixNano())

	return nil
}

// Request returns the request pointer.
func (r *Response) Request() *Request {
	return r.request.Load()
}

// LruListElement returns the LRU list element pointer (for LRU cache management).
func (r *Response) LruListElement() *list.Element[*Response] {
	return r.lruListElem.Load()
}

// SetLruListElement sets the LRU list element pointer.
func (r *Response) SetLruListElement(el *list.Element[*Response]) {
	r.lruListElem.Store(el)
}

// Data returns the underlying Data payload.
func (r *Response) Data() *Data {
	return r.data.Load()
}

// Body returns the response body.
func (r *Response) Body() []byte {
	return r.data.Load().Body()
}

// Headers returns the HTTP h.
func (r *Response) Headers() http.Header {
	return r.data.Load().Headers()
}

// RevalidatedAt returns the last revalidation time (as time.Time).
func (r *Response) RevalidatedAt() time.Time {
	return time.Unix(0, atomic.LoadInt64(&r.revalidatedAt))
}

func (r *Response) setUpWeight() int64 {
	var size = int(unsafe.Sizeof(*r))

	data := r.data.Load()
	if data != nil {
		for key, values := range data.headers {
			size += len(key)
			for _, val := range values {
				size += len(val)
			}
		}
		size += len(data.body)
	}

	return int64(size) + r.Request().Weight()
}

// Weight estimates the in-memory size of this response (including dynamic fields).
func (r *Response) Weight() int64 {
	return r.weight
}

func (r *Response) PrintDump() {
	req := r.Request()
	data := r.Data()

	// Format request headers
	reqHeaders := make([]string, 0, len(req.Headers()))
	for _, header := range req.Headers() {
		reqHeaders = append(reqHeaders, fmt.Sprintf("%s: %s", header[0], header[1]))
	}

	// Format data headers
	dataHeaders := make([]string, 0, len(data.Headers()))
	for key, values := range data.Headers() {
		for _, val := range values {
			dataHeaders = append(dataHeaders, fmt.Sprintf("%s: %s", key, val))
		}
	}

	fmt.Printf(
		"Response Dump {\n"+
			"  Request:\n"+
			"    MapKey:   %d\n"+
			"    ShardKey: %d\n"+
			"    Query:    %s\n"+
			"    Headers:\n      %s\n"+
			"  Data:\n"+
			"    StatusCode: %d\n"+
			"    Headers:\n      %s\n"+
			"    Body: |-\n      %s\n"+
			"}\n",
		req.MapKey(),
		req.ShardKey(),
		string(req.ToQuery()),
		strings.Join(reqHeaders, "\n      "),
		data.StatusCode(),
		strings.Join(dataHeaders, "\n      "),
		string(data.Body()),
	)
}
