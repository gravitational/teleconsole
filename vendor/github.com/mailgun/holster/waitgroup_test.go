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
	"sync/atomic"
	"time"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/suite"
	"gopkg.in/ahmetb/go-linq.v3"
	"testing"
	"github.com/mailgun/holster"
)

type WaitGroupTestSuite struct{
	suite.Suite
}

func TestWaitGroup(t *testing.T) {
	suite.Run(t, new(WaitGroupTestSuite))
}

func (s *WaitGroupTestSuite) TestRun() {
	var wg holster.WaitGroup

	items := []error{
		errors.New("Error 1"),
		errors.New("Error 2"),
	}

	// Iterate over a thing and doing some long running thing for each
	for _, item := range items {
		wg.Run(func(item interface{}) error {
			// Do some long running thing
			time.Sleep(time.Nanosecond * 50)
			// Return an error for testing
			return item.(error)
		}, item)
	}

	errs := wg.Wait()
	s.NotNil(errs)
	s.Equal(2, len(errs))
	s.Equal(true, linq.From(errs).Contains(items[0]))
	s.Equal(true, linq.From(errs).Contains(items[1]))
}

func (s *WaitGroupTestSuite) TestLoop() {
	pipe := make(chan int32, 0)
	var wg holster.WaitGroup
	var count int32

	wg.Loop(func() bool {
		select {
		case inc, ok := <-pipe:
			if !ok {
				return false
			}
			atomic.AddInt32(&count, inc)
		}
		return true
	})

	// Feed the loop some numbers and close the pipe
	pipe <- 1
	pipe <- 5
	pipe <- 10
	close(pipe)

	// Wait for the routine to end
	// no error collection when using Loop()
	errs := wg.Wait()
	s.Nil(errs)
	s.Equal(int32(16), count)
}

func (s *WaitGroupTestSuite) TestUntil() {
	pipe := make(chan int32, 0)
	var wg holster.WaitGroup
	var count int32

	wg.Until(func(done chan struct{}) bool {
		select {
		case inc := <-pipe:
			atomic.AddInt32(&count, inc)
		case <-done:
			return false
		}
		return true
	})

	wg.Until(func(done chan struct{}) bool {
		select {
		case inc := <-pipe:
			atomic.AddInt32(&count, inc)
		case <-done:
			return false
		}
		return true
	})

	// Feed the loop some numbers and close the pipe
	pipe <- 1
	pipe <- 5
	pipe <- 10

	// Wait for the routine to end
	wg.Stop()
	s.Equal(int32(16), count)
}
