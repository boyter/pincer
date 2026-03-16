package data

import (
	"sync"
)

type cacheEntry struct {
	entry   []byte
	addTime int64
}

// SimpleCache is a very simple cache with a hard limit on the number of items it can hold
// when a new item is added which would cause its max items count to be exceeded it will look through randomly
// based on map iteration and evict the oldest item it finds
// Gets will bump the time making this slightly LFU such that frequently accessed items should remain in the cache
type SimpleCache struct {
	maxItems int
	items    map[string]cacheEntry
	lock     sync.Mutex
	clock    int64
}

func NewSimpleCache(maxItems int) *SimpleCache {
	cache := SimpleCache{
		maxItems: maxItems,
		items:    map[string]cacheEntry{},
		lock:     sync.Mutex{},
		clock:    0,
	}

	return &cache
}

func (cache *SimpleCache) Add(cacheKey string, entry []byte) {
	cache.lock.Lock()
	defer cache.lock.Unlock()

	cache.expireItems()
	cache.clock++

	cache.items[cacheKey] = cacheEntry{
		entry:   entry,
		addTime: cache.clock,
	}
}

func (cache *SimpleCache) Get(cacheKey string) ([]byte, bool) {
	cache.lock.Lock()
	defer cache.lock.Unlock()

	item, ok := cache.items[cacheKey]

	if ok {
		// if we got this item lets bump it so that things we use a lot remain
		// IE make it LFU ish...
		cache.clock++
		item.addTime = cache.clock
		cache.items[cacheKey] = item
		return item.entry, true
	}

	return nil, false
}

// ExpireItems is called before any insert operation because we need to ensure we have less than
// the total number of iterms
func (cache *SimpleCache) expireItems() {
	if len(cache.items) >= cache.maxItems {
		// get the first few items, then expire the oldest, since map order should
		// be random according to Go implementation we should find things that seem ok to eject
		// https://stackoverflow.com/questions/41019703/is-map-iteration-sufficiently-random-for-randomly-selecting-keys

		oldestKey := ""
		var oldestTime int64

		count := 0
		for k, v := range cache.items {
			count++

			if oldestTime == 0 || v.addTime < oldestTime {
				oldestKey = k
				oldestTime = v.addTime
			}

			if count >= 10 {
				break
			}
		}

		delete(cache.items, oldestKey)
	}
}
