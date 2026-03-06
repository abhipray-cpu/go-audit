package cache

import (
	"context"
	"sync"

	"github.com/abhipray-cpu/go-audit/domain"
	"github.com/abhipray-cpu/go-audit/domain/port"
)

// Compile-time check.
var _ port.CachePort = (*LRU)(nil)

// DefaultCapacity is the default maximum number of entries in the cache.
const DefaultCapacity = 1024

// entry is a doubly-linked list node in the LRU cache.
type entry struct {
	key   string
	value domain.VersionRecord
	prev  *entry
	next  *entry
}

// LRU is an in-process Least-Recently-Used cache implementing [port.CachePort].
//
// All methods are safe for concurrent use. Write-through semantics: every
// [LRU.Put] updates the cache immediately; reads promote the entry to MRU
// position.
type LRU struct {
	mu       sync.Mutex
	capacity int
	items    map[string]*entry
	head     *entry // most recently used
	tail     *entry // least recently used
}

// New creates an LRU cache with the given capacity.
// If capacity ≤ 0, [DefaultCapacity] is used.
func New(capacity int) *LRU {
	if capacity <= 0 {
		capacity = DefaultCapacity
	}
	return &LRU{
		capacity: capacity,
		items:    make(map[string]*entry, capacity),
	}
}

// cacheKey returns a unique key for a version record.
func cacheKey(entityType, entityID string, version int64) string {
	// Fast key construction without fmt.Sprintf overhead.
	buf := make([]byte, 0, len(entityType)+len(entityID)+20)
	buf = append(buf, entityType...)
	buf = append(buf, ':')
	buf = append(buf, entityID...)
	buf = append(buf, ':')
	buf = appendInt64(buf, version)
	return string(buf)
}

// appendInt64 appends the decimal representation of n to buf.
func appendInt64(buf []byte, n int64) []byte {
	if n == 0 {
		return append(buf, '0')
	}
	if n < 0 {
		buf = append(buf, '-')
		n = -n
	}
	var digits [20]byte
	i := len(digits)
	for n > 0 {
		i--
		digits[i] = byte('0' + n%10)
		n /= 10
	}
	return append(buf, digits[i:]...)
}

// Get retrieves a cached version record. Returns the record and true if
// found; promotes the entry to MRU position. Returns a zero value and
// false on cache miss.
func (c *LRU) Get(_ context.Context, entityType, entityID string, version int64) (domain.VersionRecord, bool) {
	key := cacheKey(entityType, entityID, version)

	c.mu.Lock()
	defer c.mu.Unlock()

	e, ok := c.items[key]
	if !ok {
		return domain.VersionRecord{}, false
	}

	c.moveToFront(e)
	return e.value, true
}

// Put stores a version record in the cache (write-through). If the cache
// is at capacity, the least-recently-used entry is evicted.
func (c *LRU) Put(_ context.Context, record domain.VersionRecord) {
	key := cacheKey(record.EntityType, record.EntityID, record.Version)

	c.mu.Lock()
	defer c.mu.Unlock()

	if e, ok := c.items[key]; ok {
		e.value = record
		c.moveToFront(e)
		return
	}

	// Evict if at capacity.
	if len(c.items) >= c.capacity {
		c.evictLRU()
	}

	e := &entry{key: key, value: record}
	c.items[key] = e
	c.pushFront(e)
}

// Invalidate removes all cached entries for the given entity (all versions).
func (c *LRU) Invalidate(_ context.Context, entityType, entityID string) {
	prefix := entityType + ":" + entityID + ":"

	c.mu.Lock()
	defer c.mu.Unlock()

	for key, e := range c.items {
		if len(key) >= len(prefix) && key[:len(prefix)] == prefix {
			c.removeEntry(e)
			delete(c.items, key)
		}
	}
}

// Len returns the current number of entries in the cache.
func (c *LRU) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.items)
}

// ---------- doubly-linked list operations ----------

func (c *LRU) pushFront(e *entry) {
	e.prev = nil
	e.next = c.head
	if c.head != nil {
		c.head.prev = e
	}
	c.head = e
	if c.tail == nil {
		c.tail = e
	}
}

func (c *LRU) moveToFront(e *entry) {
	if c.head == e {
		return
	}
	c.removeEntry(e)
	c.pushFront(e)
}

func (c *LRU) removeEntry(e *entry) {
	if e.prev != nil {
		e.prev.next = e.next
	} else {
		c.head = e.next
	}
	if e.next != nil {
		e.next.prev = e.prev
	} else {
		c.tail = e.prev
	}
	e.prev = nil
	e.next = nil
}

func (c *LRU) evictLRU() {
	if c.tail == nil {
		return
	}
	delete(c.items, c.tail.key)
	c.removeEntry(c.tail)
}
