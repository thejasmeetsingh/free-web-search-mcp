package services

import (
	"container/list"
	"sync"
	"time"
)

type cacheEntry[V any] struct {
	key     string
	value   V
	expires time.Time
}

// Cache is a concurrency-safe in-memory cache with a per-entry TTL and
// least-recently-used eviction once maxEntries is exceeded.
// A ttl or maxEntries of 0 (or less) disables it: Get always misses and
// Set does nothing.
type Cache[V any] struct {
	mu         sync.Mutex
	ttl        time.Duration
	maxEntries int
	order      *list.List // front = most recently used
	items      map[string]*list.Element
}

func NewCache[V any](ttl time.Duration, maxEntries int) *Cache[V] {
	return &Cache[V]{
		ttl:        ttl,
		maxEntries: maxEntries,
		order:      list.New(),
		items:      make(map[string]*list.Element),
	}
}

func (c *Cache[V]) enabled() bool {
	return c.ttl > 0 && c.maxEntries > 0
}

func (c *Cache[V]) Get(key string) (V, bool) {
	var zero V
	if !c.enabled() {
		return zero, false
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	el, ok := c.items[key]
	if !ok {
		return zero, false
	}
	e := el.Value.(*cacheEntry[V])
	if time.Now().After(e.expires) {
		c.removeElement(el) // expired entries are removed lazily on access
		return zero, false
	}
	c.order.MoveToFront(el)
	return e.value, true
}

func (c *Cache[V]) Set(key string, value V) {
	if !c.enabled() {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	expires := time.Now().Add(c.ttl)
	if el, ok := c.items[key]; ok {
		e := el.Value.(*cacheEntry[V])
		e.value, e.expires = value, expires
		c.order.MoveToFront(el)
		return
	}

	c.items[key] = c.order.PushFront(&cacheEntry[V]{key: key, value: value, expires: expires})
	for c.order.Len() > c.maxEntries {
		c.removeElement(c.order.Back())
	}
}

func (c *Cache[V]) removeElement(el *list.Element) {
	e := el.Value.(*cacheEntry[V])
	delete(c.items, e.key)
	c.order.Remove(el)
}
