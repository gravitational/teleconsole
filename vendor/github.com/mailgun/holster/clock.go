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
)

// TimeProvider is an interface we use to mock time in tests.
type Clock interface {
	Now() time.Time
	Sleep(time.Duration)
	After(time.Duration) <-chan time.Time
}

// system clock, time as reported by the operating system.
// Use this in production workloads.
type SystemClock struct{}

func (*SystemClock) Now() time.Time {
	return time.Now()
}

func (*SystemClock) Sleep(d time.Duration) {
	time.Sleep(d)
}

func (*SystemClock) After(d time.Duration) <-chan time.Time {
	return time.After(d)
}

// Manually controlled clock for use in tests
// Advance time by calling FrozenClock.Sleep()
type FrozenClock struct {
	CurrentTime time.Time
}

func (t *FrozenClock) Now() time.Time {
	return t.CurrentTime
}

func (t *FrozenClock) Sleep(d time.Duration) {
	t.CurrentTime = t.CurrentTime.Add(d)
}

func (t *FrozenClock) After(d time.Duration) <-chan time.Time {
	t.Sleep(d)
	c := make(chan time.Time, 1)
	c <- t.CurrentTime
	return c
}

// SleepClock returns a Clock that has good fakes for
// time.Sleep and time.After. Both functions will behave as if
// time is frozen until you call AdvanceTimeBy, at which point
// any calls to time.Sleep that should return do return and
// any ticks from time.After that should happen do happen.
type SleepClock struct {
	currentTime time.Time
	waiters     map[time.Time][]chan time.Time
	mu          sync.Mutex
}

func NewSleepClock(currentTime time.Time) Clock {
	return &SleepClock{
		currentTime: currentTime,
		waiters:     make(map[time.Time][]chan time.Time),
	}
}

func (t *SleepClock) Now() time.Time {
	return t.currentTime
}

func (t *SleepClock) Sleep(d time.Duration) {
	<-t.After(d)
}

func (t *SleepClock) After(d time.Duration) <-chan time.Time {
	t.mu.Lock()
	defer t.mu.Unlock()

	c := make(chan time.Time, 1)
	until := t.currentTime.Add(d)
	t.waiters[until] = append(t.waiters[until], c)
	return c
}

// Simulates advancing time by some time.Duration (Use for testing only)
func (t *SleepClock) Advance(d time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.currentTime = t.currentTime.Add(d)
	for k, v := range t.waiters {
		if k.Before(t.currentTime) {
			for _, c := range v {
				c <- t.currentTime
			}
			delete(t.waiters, k)
		}
	}

}

// Helper method for sleep clock (See SleepClock.Advance())
func AdvanceSleepClock(clock Clock, d time.Duration) {
	sleep := clock.(*SleepClock)
	sleep.Advance(d)
}
