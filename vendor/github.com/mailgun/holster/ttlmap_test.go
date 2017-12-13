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
package holster_test

import (
	"fmt"
	"time"

	"github.com/mailgun/holster"
	. "gopkg.in/check.v1"
)

type TestSuite struct {
	clock *holster.FrozenClock
}

var _ = Suite(&TestSuite{})

func (s *TestSuite) SetUpTest(c *C) {
	start := time.Date(2012, 3, 4, 5, 6, 7, 0, time.UTC)
	s.clock = &holster.FrozenClock{CurrentTime: start}
}

func (s *TestSuite) newMap(capacity int) *holster.TTLMap {
	return holster.NewTTLMapWithClock(capacity, s.clock)
}

func (s *TestSuite) advanceSeconds(seconds int) {
	s.clock.CurrentTime = s.clock.CurrentTime.Add(time.Second * time.Duration(seconds))
}

func (s *TestSuite) TestWithRealTime(c *C) {
	m := holster.NewTTLMap(1)
	c.Assert(m, Not(Equals), nil)
}

func (s *TestSuite) TestSetWrong(c *C) {
	m := s.newMap(1)

	err := m.Set("a", 1, -1)
	c.Assert(err, Not(Equals), nil)

	err = m.Set("a", 1, 0)
	c.Assert(err, Not(Equals), nil)

	_, err = m.Increment("a", 1, 0)
	c.Assert(err, Not(Equals), nil)

	_, err = m.Increment("a", 1, -1)
	c.Assert(err, Not(Equals), nil)
}

func (s *TestSuite) TestRemoveExpiredEmpty(c *C) {
	m := s.newMap(1)
	m.RemoveExpired(100)
}

func (s *TestSuite) TestRemoveLastUsedEmpty(c *C) {
	m := s.newMap(1)
	m.RemoveLastUsed(100)
}

func (s *TestSuite) TestGetSetExpire(c *C) {
	m := s.newMap(1)

	err := m.Set("a", 1, 1)
	c.Assert(err, Equals, nil)

	valI, exists := m.Get("a")
	c.Assert(exists, Equals, true)
	c.Assert(valI, Equals, 1)

	s.advanceSeconds(1)

	_, exists = m.Get("a")
	c.Assert(exists, Equals, false)
}

func (s *TestSuite) TestSetOverwrite(c *C) {
	m := s.newMap(1)

	err := m.Set("o", 1, 1)
	c.Assert(err, Equals, nil)

	valI, exists := m.Get("o")
	c.Assert(exists, Equals, true)
	c.Assert(valI, Equals, 1)

	err = m.Set("o", 2, 1)
	c.Assert(err, Equals, nil)

	valI, exists = m.Get("o")
	c.Assert(exists, Equals, true)
	c.Assert(valI, Equals, 2)
}

func (s *TestSuite) TestRemoveExpiredEdgeCase(c *C) {
	m := s.newMap(1)

	err := m.Set("a", 1, 1)
	c.Assert(err, Equals, nil)

	s.advanceSeconds(1)

	err = m.Set("b", 2, 1)
	c.Assert(err, Equals, nil)

	valI, exists := m.Get("a")
	c.Assert(exists, Equals, false)

	valI, exists = m.Get("b")
	c.Assert(exists, Equals, true)
	c.Assert(valI, Equals, 2)

	c.Assert(m.Len(), Equals, 1)
}

func (s *TestSuite) TestRemoveOutOfCapacity(c *C) {
	m := s.newMap(2)

	err := m.Set("a", 1, 5)
	c.Assert(err, Equals, nil)

	s.advanceSeconds(1)

	err = m.Set("b", 2, 6)
	c.Assert(err, Equals, nil)

	err = m.Set("c", 3, 10)
	c.Assert(err, Equals, nil)

	valI, exists := m.Get("a")
	c.Assert(exists, Equals, false)

	valI, exists = m.Get("b")
	c.Assert(exists, Equals, true)
	c.Assert(valI, Equals, 2)

	valI, exists = m.Get("c")
	c.Assert(exists, Equals, true)
	c.Assert(valI, Equals, 3)

	c.Assert(m.Len(), Equals, 2)
}

func (s *TestSuite) TestGetNotExists(c *C) {
	m := s.newMap(1)
	_, exists := m.Get("a")
	c.Assert(exists, Equals, false)
}

func (s *TestSuite) TestGetIntNotExists(c *C) {
	m := s.newMap(1)
	_, exists, err := m.GetInt("a")
	c.Assert(err, Equals, nil)
	c.Assert(exists, Equals, false)
}

func (s *TestSuite) TestGetInvalidType(c *C) {
	m := s.newMap(1)
	m.Set("a", "banana", 5)

	_, _, err := m.GetInt("a")
	c.Assert(err, Not(Equals), nil)

	_, err = m.Increment("a", 4, 1)
	c.Assert(err, Not(Equals), nil)
}

func (s *TestSuite) TestIncrementGetExpire(c *C) {
	m := s.newMap(1)

	m.Increment("a", 5, 1)
	val, exists, err := m.GetInt("a")

	c.Assert(err, Equals, nil)
	c.Assert(exists, Equals, true)
	c.Assert(val, Equals, 5)

	s.advanceSeconds(1)

	m.Increment("a", 4, 1)
	val, exists, err = m.GetInt("a")

	c.Assert(err, Equals, nil)
	c.Assert(exists, Equals, true)
	c.Assert(val, Equals, 4)
}

func (s *TestSuite) TestIncrementOverwrite(c *C) {
	m := s.newMap(1)

	m.Increment("a", 5, 1)
	val, exists, err := m.GetInt("a")

	c.Assert(err, Equals, nil)
	c.Assert(exists, Equals, true)
	c.Assert(val, Equals, 5)

	m.Increment("a", 4, 1)
	val, exists, err = m.GetInt("a")

	c.Assert(err, Equals, nil)
	c.Assert(exists, Equals, true)
	c.Assert(val, Equals, 9)
}

func (s *TestSuite) TestIncrementOutOfCapacity(c *C) {
	m := s.newMap(1)

	m.Increment("a", 5, 1)
	val, exists, err := m.GetInt("a")

	c.Assert(err, Equals, nil)
	c.Assert(exists, Equals, true)
	c.Assert(val, Equals, 5)

	m.Increment("b", 4, 1)
	val, exists, err = m.GetInt("b")

	c.Assert(err, Equals, nil)
	c.Assert(exists, Equals, true)
	c.Assert(val, Equals, 4)

	val, exists, err = m.GetInt("a")

	c.Assert(err, Equals, nil)
	c.Assert(exists, Equals, false)
}

func (s *TestSuite) TestIncrementRemovesExpired(c *C) {
	m := s.newMap(2)

	m.Increment("a", 1, 1)
	m.Increment("b", 2, 2)

	s.advanceSeconds(1)
	m.Increment("c", 3, 3)

	val, exists, err := m.GetInt("a")

	c.Assert(err, Equals, nil)
	c.Assert(exists, Equals, false)

	val, exists, err = m.GetInt("b")
	c.Assert(err, Equals, nil)
	c.Assert(exists, Equals, true)
	c.Assert(val, Equals, 2)

	val, exists, err = m.GetInt("c")
	c.Assert(err, Equals, nil)
	c.Assert(exists, Equals, true)
	c.Assert(val, Equals, 3)
}

func (s *TestSuite) TestIncrementRemovesLastUsed(c *C) {
	m := s.newMap(2)

	m.Increment("a", 1, 10)
	m.Increment("b", 2, 11)
	m.Increment("c", 3, 12)

	val, exists, err := m.GetInt("a")

	c.Assert(err, Equals, nil)
	c.Assert(exists, Equals, false)

	val, exists, err = m.GetInt("b")
	c.Assert(err, Equals, nil)
	c.Assert(exists, Equals, true)

	c.Assert(val, Equals, 2)

	val, exists, err = m.GetInt("c")
	c.Assert(err, Equals, nil)
	c.Assert(exists, Equals, true)
	c.Assert(val, Equals, 3)
}

func (s *TestSuite) TestIncrementUpdatesTtl(c *C) {
	m := s.newMap(1)

	m.Increment("a", 1, 1)
	m.Increment("a", 1, 10)

	s.advanceSeconds(1)

	val, exists, err := m.GetInt("a")
	c.Assert(err, Equals, nil)
	c.Assert(exists, Equals, true)
	c.Assert(val, Equals, 2)
}

func (s *TestSuite) TestUpdate(c *C) {
	m := s.newMap(1)

	m.Increment("a", 1, 1)
	m.Increment("a", 1, 10)

	s.advanceSeconds(1)

	val, exists, err := m.GetInt("a")
	c.Assert(err, Equals, nil)
	c.Assert(exists, Equals, true)
	c.Assert(val, Equals, 2)
}

func (s *TestSuite) TestCallOnExpire(c *C) {
	var called bool
	var key string
	var val interface{}
	m := s.newMap(1)
	m.OnExpire = func(k string, el interface{}) {
		called = true
		key = k
		val = el
	}

	err := m.Set("a", 1, 1)
	c.Assert(err, Equals, nil)

	valI, exists := m.Get("a")
	c.Assert(exists, Equals, true)
	c.Assert(valI, Equals, 1)

	s.advanceSeconds(1)

	_, exists = m.Get("a")
	c.Assert(exists, Equals, false)
	c.Assert(called, Equals, true)
	c.Assert(key, Equals, "a")
	c.Assert(val, Equals, 1)
}

func Example_TTLMap_Usage() {
	ttlMap := holster.NewTTLMap(10)
	ttlMap.Clock = &holster.FrozenClock{time.Now()}

	// Set a value that expires in 5 seconds
	ttlMap.Set("one", "one", 5)

	// Set a value that expires in 10 seconds
	ttlMap.Set("two", "twp", 10)

	// Simulate sleeping for 6 seconds
	ttlMap.Clock.Sleep(time.Second * 6)

	// Retrieve the expired value and un-expired value
	_, ok1 := ttlMap.Get("one")
	_, ok2 := ttlMap.Get("two")

	fmt.Printf("value one exists: %t\n", ok1)
	fmt.Printf("value two exists: %t\n", ok2)

	// Output: value one exists: false
	// value two exists: true
}
