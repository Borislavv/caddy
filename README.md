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

- Internal sharded cache with 4096+ shards
- Per-shard LRU queues with proportional eviction
- Optional TinyLFU filter (Count-Min Sketch + Doorkeeper)
- Aggressive memory reuse and pooling (no allocations in hot paths)
- GZIP support with buffer reuse
- Real-time TTL and tag-based rule matching
- Auto-refresh of stale entries with minimal blocking

## 🛠 Build Instructions

### Requirements
- Go 1.20+
- Caddy build tools (optional: `xcaddy`)

### Build
```bash
git clone https://github.com/your-org/caddy-advanced-cache.git
cd caddy-advanced-cache
go build -o caddy ./cmd/caddy
```

## 🚀 Usage Example (Caddyfile)
```caddyfile
:80 {
  route {
    advanced_cache {
      config_path /etc/caddy/config.yaml
    }
    reverse_proxy localhost:8080
  }
}
```

## ⚙️ Config Example (`config.yaml`)
```yaml
cache:
  default_ttl: 30s
  max_size_mb: 512
  shards: 4096
  compression: true
  lru:
    enabled: true
    max_entries: 200000
rules:
  - path_prefix: /api/
    ttl: 15s
    tags: ["api"]
  - path_prefix: /static/
    ttl: 5m
    compression: false
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