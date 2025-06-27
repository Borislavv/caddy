# Caddy with Advanced Cache Middleware

> 🚀 A fork of vanilla [Caddy](https://caddyserver.com) with powerful in-memory caching powered by [Advanced Cache](https://github.com/Borislavv/advanced-cache).

This repository is a production-grade fork of the official **Caddy HTTP server**, extended with a high-performance in-memory caching middleware named `advanced_cache`, developed for speed, control, and memory efficiency.
Unlike plugin-based integrations, this version includes **native middleware embedding**, allowing for better optimization, tighter control, and lower overhead.

## 🔍 What Is Included

- ✅ Fully functional Caddy v2 core (unchanged in behavior)
- ➕ Embedded `advanced_cache` middleware — no plugin needed
- ⚡ Integrated support for:
    - LRU + TinyLFU hybrid caching algorithms
    - Memory-aware eviction
    - Zero-allocation request modeling
    - GZIP compression of cached bodies
    - `sync.Pool`-based memory reuse
    - Data refreshers and smart eviction strategies
    - Live metrics export (VictoriaMetrics compatible)

## 📦 Middleware Location

Middleware source is located at:
```
modules/advancedcache/cache.go
```
It is registered under Caddy module namespace:
```go
http.handlers.advanced_cache
```

## 🧱 Architecture

The embedded middleware is functionally identical to the original [Advanced Cache project](https://github.com/Borislavv/advanced-cache), including:

- Internal sharded cache with 2048 shards
- Per-shard LRU queues with proportional eviction
- Optional TinyLFU filter (Count-Min Sketch + Doorkeeper)
- Aggressive memory reuse and pooling (no allocations in hot paths)
- GZIP support with buffer reuse
- Real-time TTL and tag-based rule matching
- Auto-refresh of stale entries with minimal blocking

## 🛠 Build Instructions

### Requirements
- Go 1.20+

### Build
```bash
git clone https://gitlab.xbet.lan/v3group/backend/caddy.git
cd caddy
go build -o caddy ./cmd/caddy
```

## 🚀 Usage Example (Caddyfile)
```caddyfile
:80 {
  route {
    advanced_cache {
      config_path /config/config.prod.yaml
    }
    reverse_proxy localhost:8080
  }
}
```

## ⚙️ Config Example (`config.yaml`)
```yaml
cache:
  env: "prod"
  enabled: true

  lifetime:
    max_req_dur: "100ms" # If a request lifetime is longer than 100ms then request will be canceled by context.
    escape_max_req_dur: "X-Google-Bot" # If the header exists the timeout above will be skipped.

  upstream:
    url: "http://localhost:8080" # downstream reverse proxy host:port
    rate: 1000 # Rate limiting reqs to backend per second.
    timeout: "10s" # Timeout for requests to backend.

  preallocate:
    num_shards: 2048 # Fixed constant (see `NumOfShards` in code). Controls the number of sharded maps.
    per_shard: 8196  # Preallocated map size per shard. Without resizing, this supports 2048*8196=~16785408 keys in total.
    # Note: this is an upper-bound estimate and may vary depending on hash distribution quality.

  eviction:
    threshold: 0.9 # Trigger eviction when cache memory usage exceeds 90% of its configured limit.

  storage:
    size: 32212254720 # 30 GB of maximum allowed memory for the in-memory cache (in bytes).

  refresh:
    ttl: "12h"
    error_ttl: "1h"
    rate: 1000 # Rate limiting reqs to backend per second.
    scan_rate: 10000 # Rate limiting of num scans items per second.
    beta: 0.4 # Controls randomness in refresh timing to avoid thundering herd (from 0 to 1).

  persistence:
    dump:
      enabled: true
      format: "gzip" # gzip or raw json
      dump_dir: "public/dump"
      dump_name: "cache.dump.gz"
      rotate_policy: "ring" # fixed, ring
      max_files: 7

  rules:
    - path: "/api/v2/pagedata"
      ttl: "24h"
      error_ttl: "1h"
      beta: 0.3 # Controls randomness in refresh timing to avoid thundering herd.
      cache_key:
        query: ['project[id]', 'domain', 'language', 'choice'] # Match query parameters by prefix.
        headers: ['Accept-Encoding', 'X-Project-ID']           # Match headers by exact value.
      cache_value:
        headers: ['X-Project-ID']                              # Store only when headers match exactly.

    - path: "/api/v1/pagecontent"
      ttl: "36h"
      error_ttl: "3h"
      beta: 0.3 # Controls randomness in refresh timing to avoid thundering herd.
      cache_key:
        query: ['project[id]', 'domain', 'language', 'choice'] # Match query parameters by prefix.
        headers: ['Accept-Encoding', 'X-Project-ID']           # Match headers by exact value.
      cache_value:
        headers: ['X-Project-ID']                              # Store only when headers match exactly.

```

## 📊 Metrics
- `advanced_cache_http_requests_total{path,method,status}`
- `advanced_cache_memory_usage_bytes`
- `advanced_cache_items_total`
- Exposed at: `/metrics`

## 🧪 For Developers

- Look inside `modules/advancedcache/cache.go` for entrypoint
- Middleware registers itself in `init()`
- Can be hot-swapped with any other HTTP handler inside route block

## 📜 License
Apache-2.0 (Caddy) + MIT (Advanced Cache)

## 🔗 Related
- [Advanced Cache GitHub](https://github.com/Borislavv/advanced-cache)
- [Caddy Server](https://github.com/caddyserver/caddy)