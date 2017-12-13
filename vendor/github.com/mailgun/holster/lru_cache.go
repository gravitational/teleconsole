/*
Modifications Copyright 2017 Mailgun Technologies Inc

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

     http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

This work is derived from github.com/golang/groupcache/lru
*/
package holster

import (
	"container/list"
	"sync"
	"time"
)

// Holds stats collected about the cache
type LRUCacheStats struct {
	Size int64
	Miss int64
	Hit  int64
}

// Cache is an thread safe LRU cache that also supports optional TTL expiration
// You can use an non thread safe version of this
type LRUCache struct {
	// MaxEntries is the maximum number of cache entries before
	// an item is evicted. Zero means no limit.
	MaxEntries int

	// OnEvicted optionally specifies a callback function to be
	// executed when an entry is purged from the cache.
	OnEvicted func(key Key, value interface{})

	mutex sync.Mutex
	stats LRUCacheStats
	ll    *list.List
	cache map[interface{}]*list.Element
}

// A Key may be any value that is comparable. See http://golang.org/ref/spec#Comparison_operators
type Key interface{}

type cacheRecord struct {
	key      Key
	value    interface{}
	expireAt *time.Time
}

// New creates a new Cache.
// If maxEntries is zero, the cache has no limit and it's assumed
// that eviction is done by the caller.
func NewLRUCache(maxEntries int) *LRUCache {
	return &LRUCache{
		MaxEntries: maxEntries,
		ll:         list.New(),
		cache:      make(map[interface{}]*list.Element),
	}
}

// Adds a value to the cache
func (c *LRUCache) Add(key Key, value interface{}) {
	c.addRecord(&cacheRecord{key: key, value: value})
}

// Adds a value to the cache with a TTL
func (c *LRUCache) AddWithTTL(key Key, value interface{}, TTL time.Duration) {
	expireAt := time.Now().UTC().Add(TTL)
	c.addRecord(&cacheRecord{
		key:      key,
		value:    value,
		expireAt: &expireAt,
	})
}

// Adds a value to the cache.
func (c *LRUCache) addRecord(record *cacheRecord) {
	defer c.mutex.Unlock()
	c.mutex.Lock()

	// If the key already exist, set the new value
	if ee, ok := c.cache[record.key]; ok {
		c.ll.MoveToFront(ee)
		temp := ee.Value.(*cacheRecord)
		*temp = *record
		return
	}

	ele := c.ll.PushFront(record)
	c.cache[record.key] = ele
	if c.MaxEntries != 0 && c.ll.Len() > c.MaxEntries {
		c.RemoveOldest()
	}
}

// Get looks up a key's value from the cache.
func (c *LRUCache) Get(key Key) (value interface{}, ok bool) {
	defer c.mutex.Unlock()
	c.mutex.Lock()

	if ele, hit := c.cache[key]; hit {
		entry := ele.Value.(*cacheRecord)

		// If the entry has expired, remove it from the cache
		if entry.expireAt != nil && entry.expireAt.Before(time.Now().UTC()) {
			c.removeElement(ele)
			c.stats.Miss++
			return
		}
		c.stats.Hit++
		c.ll.MoveToFront(ele)
		return entry.value, true
	}
	c.stats.Miss++
	return
}

// Remove removes the provided key from the cache.
func (c *LRUCache) Remove(key Key) {
	defer c.mutex.Unlock()
	c.mutex.Lock()

	if ele, hit := c.cache[key]; hit {
		c.removeElement(ele)
	}
}

// RemoveOldest removes the oldest item from the cache.
func (c *LRUCache) RemoveOldest() {
	defer c.mutex.Unlock()
	c.mutex.Lock()

	ele := c.ll.Back()
	if ele != nil {
		c.removeElement(ele)
	}
}

func (c *LRUCache) removeElement(e *list.Element) {
	c.ll.Remove(e)
	kv := e.Value.(*cacheRecord)
	delete(c.cache, kv.key)
	if c.OnEvicted != nil {
		c.OnEvicted(kv.key, kv.value)
	}
}

// Len returns the number of items in the cache.
func (c *LRUCache) Size() int {
	defer c.mutex.Unlock()
	c.mutex.Lock()
	return c.ll.Len()
}

// Returns stats about the current state of the cache
func (c *LRUCache) Stats() LRUCacheStats {
	defer func() {
		c.stats = LRUCacheStats{}
		c.mutex.Unlock()
	}()
	c.mutex.Lock()
	c.stats.Size = int64(len(c.cache))
	return c.stats
}

// Get a list of keys at this point in time
func (c *LRUCache) Keys() (keys []interface{}) {
	defer c.mutex.Unlock()
	c.mutex.Lock()

	for key := range c.cache {
		keys = append(keys, key)
	}
	return
}

// Get the value without updating the expiration or last used or stats
func (c *LRUCache) Peek(key interface{}) (value interface{}, ok bool) {
	defer c.mutex.Unlock()
	c.mutex.Lock()

	if ele, hit := c.cache[key]; hit {
		entry := ele.Value.(*cacheRecord)
		return entry.value, true
	}
	return nil, false
}

// Processes each item in the cache in a thread safe way, such that the cache can be in use
// while processing items in the cache. Processing the cache with `Each()` does not update
// the expiration or last used.
func (c LRUCache) Each(concurrent int, callBack func(key interface{}, value interface{}) error) []error {
	fanOut := NewFanOut(concurrent)
	keys := c.Keys()

	for _, key := range keys {
		fanOut.Run(func(key interface{}) error {
			value, ok := c.Peek(key)
			if !ok {
				// Key disappeared during cache iteration, This can occur as
				// expiration and removal can happen during iteration
				return nil
			}

			err := callBack(key, value)
			if err != nil {
				return err
			}
			return nil
		}, key)
	}

	// Wait for all the routines to complete
	errs := fanOut.Wait()
	if errs != nil {
		return errs
	}
	return nil
}
