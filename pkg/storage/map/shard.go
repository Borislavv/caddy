package sharded

import (
	"sync"
	"sync/atomic"
)

// Shard is a single partition of the sharded map.
// Each shard is an independent concurrent map with its own lock and refCounted pool for releasers.
type Shard[V Value] struct {
	*sync.RWMutex              // Shard-level RWMutex for concurrency
	items         map[uint64]V // Actual storage: key -> Value
	id            uint64       // Shard ID (index)
	mem           int64        // Weight usage in bytes (atomic)
	len           int64        // Length as int64 for use it as atomic
}

// NewShard creates a new shard with its own lock, value map, and releaser pool.
func NewShard[V Value](id uint64, defaultLen int) *Shard[V] {
	return &Shard[V]{
		id:      id,
		RWMutex: &sync.RWMutex{},
		items:   make(map[uint64]V, defaultLen),
	}
}

// ID returns the numeric index of this shard.
func (shard *Shard[V]) ID() uint64 {
	return shard.id
}

// Weight returns an approximate total memory usage for this shard (including overhead).
func (shard *Shard[V]) Weight() int64 {
	return atomic.LoadInt64(&shard.mem)
}

func (shard *Shard[V]) Len() int64 {
	return atomic.LoadInt64(&shard.len)
}

// Set inserts or updates a value by key, resets refCount, and updates counters.
// Returns a releaser for the inserted value.
func (shard *Shard[V]) Set(key uint64, value V) (takenMem int64) {
	shard.Lock()
	var memDiff int64
	found, ok := shard.items[key]
	if ok {
		memDiff = value.Weight() - found.Weight()
	} else {
		memDiff = value.Weight()
	}
	shard.items[key] = value
	shard.Unlock()

	takenMem = value.Weight()
	atomic.AddInt64(&shard.len, 1)
	atomic.AddInt64(&shard.mem, memDiff)

	// Return a releaser for this value (for the user to release later).
	return takenMem
}

// Get retrieves a value and returns a releaser for it, incrementing its refCount.
// Returns (value, releaser, true) if found; otherwise (zero, nil, false).
func (shard *Shard[V]) Get(key uint64) (val V, isHit bool) {
	shard.RLock()
	value, ok := shard.items[key]
	shard.RUnlock()
	return value, ok
}

// Remove removes a value from the shard, decrements counters, and may trigger full resource cleanup.
// Returns (memory_freed, pointer_to_list_element, was_found).
func (shard *Shard[V]) Remove(key uint64) (freed int64, isHit bool) {
	shard.Lock()
	v, ok := shard.items[key]
	if ok {
		delete(shard.items, key)
		shard.Unlock()

		weight := v.Weight()
		atomic.AddInt64(&shard.len, -1)
		atomic.AddInt64(&shard.mem, -weight)

		return weight, true
	}
	shard.Unlock()

	return 0, false
}
