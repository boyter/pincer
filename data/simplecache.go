package data

import (
	"sync"
)

type cacheEntry struct {
	entry      []byte
	lastAccess uint64
}

// SimpleCache is a very simple cache with a hard limit on the number of items it can hold
// when a new item is added which would cause its max items count to be exceeded it will look through randomly
// based on map iteration and evict the oldest item it finds
// Gets will bump the time making this slightly LFU such that frequently accessed items should remain in the cache
type SimpleCache struct {
	maxItems int
	items    map[string]cacheEntry
	lock     sync.Mutex
	clock    uint64
}

func NewSimpleCache(maxItems int) *SimpleCache {
	cache := SimpleCache{
		maxItems: maxItems,
		items:    map[string]cacheEntry{},
		lock:     sync.Mutex{},
	}

	return &cache
}

func (cache *SimpleCache) Add(cacheKey string, entry []byte) {
	cache.lock.Lock()
	defer cache.lock.Unlock()

	if cache.maxItems <= 0 {
		return
	}

	cache.clock++
	if _, ok := cache.items[cacheKey]; ok {
		cache.items[cacheKey] = cacheEntry{
			entry:      entry,
			lastAccess: cache.clock,
		}
		return
	}

	cache.expireItems()

	cache.items[cacheKey] = cacheEntry{
		entry:      entry,
		lastAccess: cache.clock,
	}
}

func (cache *SimpleCache) Get(cacheKey string) ([]byte, bool) {
	cache.lock.Lock()
	defer cache.lock.Unlock()

	item, ok := cache.items[cacheKey]

	if ok {
		cache.clock++
		// Bump recency on reads so frequently accessed items stay resident.
		item.lastAccess = cache.clock
		cache.items[cacheKey] = item
		return item.entry, true
	}

	return nil, false
}

// ExpireItems is called before inserts so the cache stays within its hard size limit.
func (cache *SimpleCache) expireItems() {
	if len(cache.items) >= cache.maxItems {
		oldestKey := ""
		var oldestAccess uint64
		for k, v := range cache.items {
			if oldestKey == "" || v.lastAccess < oldestAccess {
				oldestKey = k
				oldestAccess = v.lastAccess
			}
		}

		if oldestKey != "" {
			delete(cache.items, oldestKey)
		}
	}
}
