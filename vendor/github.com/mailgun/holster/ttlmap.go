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
	"fmt"
	"sync"
	"time"
)

type TTLMap struct {
	// Optionally specifies a callback function to be
	// executed when an entry has expired
	OnExpire func(key string, i interface{})

	// Optionally specify a time custom time object
	// used to determine if an item has expired
	Clock Clock

	capacity    int
	elements    map[string]*mapElement
	expiryTimes *PriorityQueue
	mutex       *sync.RWMutex
}

type mapElement struct {
	key    string
	value  interface{}
	heapEl *PQItem
}

func NewTTLMap(capacity int) *TTLMap {
	if capacity <= 0 {
		capacity = 0
	}

	return &TTLMap{
		capacity:    capacity,
		elements:    make(map[string]*mapElement),
		expiryTimes: NewPriorityQueue(),
		mutex:       &sync.RWMutex{},
		Clock:       &SystemClock{},
	}
}

func NewTTLMapWithClock(capacity int, clock Clock) *TTLMap {
	if clock == nil {
		clock = &SystemClock{}
	}
	m := NewTTLMap(capacity)
	m.Clock = clock
	return m
}

func (m *TTLMap) Set(key string, value interface{}, ttlSeconds int) error {
	expiryTime, err := m.toEpochSeconds(ttlSeconds)
	if err != nil {
		return err
	}
	m.mutex.Lock()
	defer m.mutex.Unlock()
	return m.set(key, value, expiryTime)
}

func (m *TTLMap) Len() int {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return len(m.elements)
}

func (m *TTLMap) Get(key string) (interface{}, bool) {
	value, mapEl, expired := m.lockNGet(key)
	if mapEl == nil {
		return nil, false
	}
	if expired {
		m.lockNDel(mapEl)
		return nil, false
	}
	return value, true
}

func (m *TTLMap) Increment(key string, value int, ttlSeconds int) (int, error) {
	expiryTime, err := m.toEpochSeconds(ttlSeconds)
	if err != nil {
		return 0, err
	}

	m.mutex.Lock()
	defer m.mutex.Unlock()

	mapEl, expired := m.get(key)
	if mapEl == nil || expired {
		m.set(key, value, expiryTime)
		return value, nil
	}

	currentValue, ok := mapEl.value.(int)
	if !ok {
		return 0, fmt.Errorf("Expected existing value to be integer, got %T", mapEl.value)
	}

	currentValue += value
	m.set(key, currentValue, expiryTime)
	return currentValue, nil
}

func (m *TTLMap) GetInt(key string) (int, bool, error) {
	valueI, exists := m.Get(key)
	if !exists {
		return 0, false, nil
	}
	value, ok := valueI.(int)
	if !ok {
		return 0, false, fmt.Errorf("Expected existing value to be integer, got %T", valueI)
	}
	return value, true, nil
}

func (m *TTLMap) set(key string, value interface{}, expiryTime int) error {
	if mapEl, ok := m.elements[key]; ok {
		mapEl.value = value
		m.expiryTimes.Update(mapEl.heapEl, expiryTime)
		return nil
	}

	if len(m.elements) >= m.capacity {
		m.freeSpace(1)
	}
	heapEl := &PQItem{
		Priority: expiryTime,
	}
	mapEl := &mapElement{
		key:    key,
		value:  value,
		heapEl: heapEl,
	}
	heapEl.Value = mapEl
	m.elements[key] = mapEl
	m.expiryTimes.Push(heapEl)
	return nil
}

func (m *TTLMap) lockNGet(key string) (value interface{}, mapEl *mapElement, expired bool) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	mapEl, expired = m.get(key)
	value = nil
	if mapEl != nil {
		value = mapEl.value
	}
	return value, mapEl, expired
}

func (m *TTLMap) get(key string) (*mapElement, bool) {
	mapEl, ok := m.elements[key]
	if !ok {
		return nil, false
	}
	now := int(m.Clock.Now().UTC().Unix())
	expired := mapEl.heapEl.Priority <= now
	return mapEl, expired
}

func (m *TTLMap) lockNDel(mapEl *mapElement) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	// Map element could have been updated. Now that we have a lock
	// retrieve it again and check if it is still expired.
	var ok bool
	if mapEl, ok = m.elements[mapEl.key]; !ok {
		return
	}
	now := int(m.Clock.Now().UTC().Unix())
	if mapEl.heapEl.Priority > now {
		return
	}

	if m.OnExpire != nil {
		m.OnExpire(mapEl.key, mapEl.value)
	}

	delete(m.elements, mapEl.key)
	m.expiryTimes.Remove(mapEl.heapEl)
}

func (m *TTLMap) freeSpace(count int) {
	removed := m.RemoveExpired(count)
	if removed >= count {
		return
	}
	m.RemoveLastUsed(count - removed)
}

func (m *TTLMap) RemoveExpired(iterations int) int {
	removed := 0
	now := int(m.Clock.Now().UTC().Unix())
	for i := 0; i < iterations; i += 1 {
		if len(m.elements) == 0 {
			break
		}
		heapEl := m.expiryTimes.Peek()
		if heapEl.Priority > now {
			break
		}
		m.expiryTimes.Pop()
		mapEl := heapEl.Value.(*mapElement)
		delete(m.elements, mapEl.key)
		removed += 1
	}
	return removed
}

func (m *TTLMap) RemoveLastUsed(iterations int) {
	for i := 0; i < iterations; i += 1 {
		if len(m.elements) == 0 {
			return
		}
		heapEl := m.expiryTimes.Pop()
		mapEl := heapEl.Value.(*mapElement)
		delete(m.elements, mapEl.key)
	}
}

func (m *TTLMap) toEpochSeconds(ttlSeconds int) (int, error) {
	if ttlSeconds <= 0 {
		return 0, fmt.Errorf("ttlSeconds should be >= 0, got %d", ttlSeconds)
	}
	return int(m.Clock.Now().UTC().Add(time.Second * time.Duration(ttlSeconds)).Unix()), nil
}
