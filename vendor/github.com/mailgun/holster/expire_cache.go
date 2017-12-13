/*
Copyright 2017 Mailgun Technologies Inc

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

     http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package holster

import (
	"sync"
	"time"

	"github.com/pkg/errors"
)

type ExpireCacheStats struct {
	Size int64
	Miss int64
	Hit  int64
}

// ExpireCache is a cache which expires entries only after 2 conditions are met
// 1. The Specified TTL has expired
// 2. The item has been processed with ExpireCache.Each()
//
// This is an unbounded cache which guaranties each item in the cache
// has been processed before removal. This is different from a LRU
// cache, as the cache might decide an item needs to be removed
// (because we hit the cache limit) before the item has been processed.
//
// Every time an item is touched by `Get()` or `Add()` the duration is
// updated which ensures items in frequent use stay in the cache
//
// Processing can modify the item in the cache without updating the
// expiration time by using the `Update()` method
//
// The cache can also return statistics which can be used to graph track
// the size of the cache
//
// NOTE: Because this is an unbounded cache, the user MUST process the cache
// with `Each()` regularly! Else the cache items will never expire and the cache
// will eventually eat all the memory on the system
type ExpireCache struct {
	cache map[interface{}]*expireRecord
	mutex sync.Mutex
	ttl   time.Duration
	stats ExpireCacheStats
}

type expireRecord struct {
	Value    interface{}
	ExpireAt time.Time
}

// New creates a new ExpireCache.
func NewExpireCache(ttl time.Duration) *ExpireCache {
	return &ExpireCache{
		cache: make(map[interface{}]*expireRecord),
		ttl:   ttl,
	}
}

// Retrieves a key's value from the cache
func (c *ExpireCache) Get(key interface{}) (interface{}, bool) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	record, ok := c.cache[key]
	if !ok {
		c.stats.Miss++
		return nil, ok
	}

	// Since this was recently accessed, keep it in
	// the cache by resetting the expire time
	record.ExpireAt = time.Now().UTC().Add(c.ttl)

	c.stats.Hit++
	return record.Value, ok
}

// Put the key, value and TTL in the cache
func (c *ExpireCache) Add(key interface{}, value interface{}) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	record := expireRecord{
		Value:    value,
		ExpireAt: time.Now().UTC().Add(c.ttl),
	}
	// Add the record to the cache
	c.cache[key] = &record
}

// Update the value in the cache without updating the TTL
func (c *ExpireCache) Update(key interface{}, value interface{}) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	record, ok := c.cache[key]
	if !ok {
		return errors.Errorf("ExpoireCache() - No record found for '%+v'", key)
	}
	record.Value = value
	return nil
}

// Get a list of keys at this point in time
func (c *ExpireCache) Keys() (keys []interface{}) {
	defer c.mutex.Unlock()
	c.mutex.Lock()

	for key := range c.cache {
		keys = append(keys, key)
	}
	return
}

// Get the value without updating the expiration
func (c *ExpireCache) Peek(key interface{}) (value interface{}, ok bool) {
	defer c.mutex.Unlock()
	c.mutex.Lock()

	if record, hit := c.cache[key]; hit {
		return record.Value, true
	}
	return nil, false
}

// Processes each item in the cache in a thread safe way, such that the cache can be in use
// while processing items in the cache
func (c *ExpireCache) Each(concurrent int, callBack func(key interface{}, value interface{}) error) []error {
	fanOut := NewFanOut(concurrent)
	keys := c.Keys()

	for _, key := range keys {
		fanOut.Run(func(key interface{}) error {
			c.mutex.Lock()
			record, ok := c.cache[key]
			c.mutex.Unlock()
			if !ok {
				return errors.Errorf("Each() - key '%+v' disapeared "+
					"from cache during iteration", key)
			}

			err := callBack(key, record.Value)
			if err != nil {
				return err
			}

			c.mutex.Lock()
			if record.ExpireAt.Before(time.Now().UTC()) {
				delete(c.cache, key)
			}
			c.mutex.Unlock()
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

// Retrieve stats about the cache
func (c *ExpireCache) GetStats() ExpireCacheStats {
	c.stats.Size = c.Size()
	c.mutex.Lock()
	defer func() {
		c.stats = ExpireCacheStats{}
		c.mutex.Unlock()
	}()
	return c.stats
}

// Returns the number of items in the cache.
func (c *ExpireCache) Size() int64 {
	defer c.mutex.Unlock()
	c.mutex.Lock()
	return int64(len(c.cache))
}
