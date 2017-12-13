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

This work is derived from github.com/golang/groupcache/lru
*/
package holster_test

import (
	"time"

	"github.com/mailgun/holster"
	. "gopkg.in/check.v1"
)

type LRUCacheTestSuite struct{}

var _ = Suite(&LRUCacheTestSuite{})

func (s *LRUCacheTestSuite) SetUpSuite(c *C) {
}

func (s *LRUCacheTestSuite) TestCache(c *C) {
	cache := holster.NewLRUCache(5)

	// Confirm non existent key
	value, ok := cache.Get("key")
	c.Assert(value, IsNil)
	c.Assert(ok, Equals, false)

	// Confirm add new value
	cache.Add("key", "value")
	value, ok = cache.Get("key")
	c.Assert(value, Equals, "value")
	c.Assert(ok, Equals, true)

	// Confirm overwrite current value correctly
	cache.Add("key", "new")
	value, ok = cache.Get("key")
	c.Assert(value, Equals, "new")
	c.Assert(ok, Equals, true)

	// Confirm removal works
	cache.Remove("key")
	value, ok = cache.Get("key")
	c.Assert(value, IsNil)
	c.Assert(ok, Equals, false)

	// Stats should be correct
	stats := cache.Stats()
	c.Assert(stats.Hit, Equals, int64(2))
	c.Assert(stats.Miss, Equals, int64(2))
	c.Assert(stats.Size, Equals, int64(0))
}

func (s *LRUCacheTestSuite) TestCacheWithTTL(c *C) {
	cache := holster.NewLRUCache(5)

	cache.AddWithTTL("key", "value", time.Nanosecond)
	value, ok := cache.Get("key")
	c.Assert(value, Equals, nil)
	c.Assert(ok, Equals, false)
}

func (s *LRUCacheTestSuite) TestCacheEach(c *C) {
	cache := holster.NewLRUCache(5)

	cache.Add("1", 1)
	cache.Add("2", 2)
	cache.Add("3", 3)
	cache.Add("4", 4)
	cache.Add("5", 5)

	var count int
	// concurrency of 0, means no concurrency (This test will not develop a race condition)
	errs := cache.Each(0, func(key interface{}, value interface{}) error {
		count++
		return nil
	})
	c.Assert(count, Equals, 5)
	c.Assert(errs, IsNil)

	stats := cache.Stats()
	c.Assert(stats.Hit, Equals, int64(0))
	c.Assert(stats.Miss, Equals, int64(0))
	c.Assert(stats.Size, Equals, int64(5))
}
