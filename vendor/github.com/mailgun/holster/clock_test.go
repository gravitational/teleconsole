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

var _ = fmt.Printf // for testing

type ClockTestSuite struct{}

var _ = Suite(&ClockTestSuite{})

func (s *ClockTestSuite) TestRealTimeUtcNow(c *C) {
	rt := holster.SystemClock{}

	rtNow := rt.Now().UTC()
	atNow := time.Now().UTC()

	// times shouldn't be exact
	if rtNow.Equal(atNow) {
		c.Errorf("rt.UtcNow() = time.Now.UTC(), %v = %v, should be slightly different", rtNow, atNow)
	}

	rtNowPlusOne := atNow.Add(1 * time.Second)
	rtNowMinusOne := atNow.Add(-1 * time.Second)

	// but should be pretty close
	if atNow.After(rtNowPlusOne) || atNow.Before(rtNowMinusOne) {
		c.Errorf("timedelta between rt.UtcNow() and time.Now.UTC() greater than 2 seconds, %v, %v", rtNow, atNow)
	}
}

func (s *ClockTestSuite) TestFreezeTimeUtcNow(c *C) {
	tm := time.Date(2009, time.November, 10, 23, 0, 0, 0, time.UTC)
	ft := holster.FrozenClock{tm}

	if !tm.Equal(ft.Now()) {
		c.Errorf("ft.Now() != time.Date(2009, time.November, 10, 23, 0, 0, 0, time.UTC), %v, %v", tm, ft)
	}
}

func itTicks(c <-chan time.Time) bool {
	select {
	case <-c:
		return true
	case <-time.After(time.Millisecond):
		return false
	}
}

func (s *ClockTestSuite) TestSleepableTime(c *C) {
	tm := time.Date(2009, time.November, 10, 23, 0, 0, 0, time.UTC)
	st := holster.NewSleepClock(tm)

	if !tm.Equal(st.Now()) {
		c.Errorf("st.Now() != time.Date(2009, time.November, 10, 23, 0, 0, 0, time.UTC), %v, %v", tm, st)
	}

	// Check After with no AdvanceTimeBy
	if itTicks(st.After(time.Nanosecond)) {
		c.Error("Got tick from After before calling AdvanceTimeBy")
	}

	// Check After with one call to AdvanceTimeBy
	c0 := st.After(time.Hour)
	holster.AdvanceSleepClock(st, 2*time.Hour)
	if !itTicks(c0) {
		c.Error("Didn't get tick from After after calling AdvanceTimeBy")
	}

	// Check After with multiple calls to AdvanceTimeBy
	c0 = st.After(time.Hour)
	holster.AdvanceSleepClock(st, 20*time.Minute)
	if itTicks(c0) {
		c.Error("Got tick from After before we holster.AdvanceClockBy'd enough")
	}
	holster.AdvanceSleepClock(st, 20*time.Minute)
	if itTicks(c0) {
		c.Error("Got tick from After before we holster.AdvanceClockBy'd enough")
	}
	holster.AdvanceSleepClock(st, 40*time.Minute)
	if !itTicks(c0) {
		c.Error("Didn't get tick from After after we holster.AdvanceClockBy'd enough")
	}

	// Check Sleep with no holster.AdvanceClockBy
	c1 := make(chan time.Time)
	go func() {
		st.Sleep(time.Nanosecond)
		c1 <- st.Now()
	}()
	if itTicks(c1) {
		c.Error("Sleep returned before we called holster.AdvanceClockBy")
	}
}

func Example_Clock_Usage() {

	type MyApp struct {
		Clock holster.Clock
	}

	// Defaults to the system clock
	app := MyApp{Clock: &holster.SystemClock{}}

	// Override the system clock for testing
	app.Clock = &holster.FrozenClock{time.Date(2009, time.November, 10, 23, 0, 0, 0, time.UTC)}

	// Simulate sleeping for 10 seconds
	app.Clock.Sleep(time.Second * 10)

	fmt.Printf("Time is Now: %s", app.Clock.Now())

	// Output: Time is Now: 2009-11-10 23:00:10 +0000 UTC
}
