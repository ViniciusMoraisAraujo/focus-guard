// Package lru implements a tiny bounded LRU cache (stdlib only, no
// dependencies) used by the SNI → certificate caches of the interceptor and
// the local CA: in Server mode the TLS listener accepts arbitrary hostnames,
// so an unbounded map would let a port scanner grow memory without limit. A
// cert is regenerable at any time (signLeaf/selfSigned are cheap), so
// eviction never breaks a request — the next handshake re-generates it.
//
// The cache is NOT safe for concurrent use: the callers (tlsca.CA and
// interceptor.Server) guard it with their own mutexes.
package lru

import "container/list"

// Cache é um LRU limitado string→V. Zero-value não é utilizável; construa
// com New.
type Cache[V any] struct {
	max   int
	items map[string]*list.Element
	order *list.List
}

type entry[V any] struct {
	key   string
	value V
}

// New builds an LRU cache holding at most max entries (max < 1 → 1).
func New[V any](max int) *Cache[V] {
	if max < 1 {
		max = 1
	}
	return &Cache[V]{max: max, items: make(map[string]*list.Element), order: list.New()}
}

// Get returns the value for key and moves the entry to the front (most
// recently used). ok is false when the key is absent.
func (c *Cache[V]) Get(key string) (V, bool) {
	el, ok := c.items[key]
	if !ok {
		var zero V
		return zero, false
	}
	c.order.MoveToFront(el)
	return el.Value.(*entry[V]).value, true
}

// Set inserts or updates key→value, evicting the least-recently-used entry
// when the cap is reached.
func (c *Cache[V]) Set(key string, value V) {
	if el, ok := c.items[key]; ok {
		el.Value.(*entry[V]).value = value
		c.order.MoveToFront(el)
		return
	}
	el := c.order.PushFront(&entry[V]{key: key, value: value})
	c.items[key] = el
	if c.order.Len() > c.max {
		oldest := c.order.Back()
		c.order.Remove(oldest)
		delete(c.items, oldest.Value.(*entry[V]).key)
	}
}

// Len returns the number of entries currently cached.
func (c *Cache[V]) Len() int { return c.order.Len() }
