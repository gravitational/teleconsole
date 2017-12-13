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

import "sync"

// FanOut spawns a new go-routine each time `Run()` is called until `size` is reached,
// subsequent calls to `Run()` will block until previously `Run()` routines have completed.
// Allowing the user to control how many routines will run simultaneously. `Wait()` then
// collects any errors from the routines once they have all completed.
type FanOut struct {
	errChan chan error
	size    chan bool
	errs    []error
	wg      sync.WaitGroup
}

func NewFanOut(size int) *FanOut {
	// They probably want no concurrency
	if size == 0 {
		size = 1
	}

	pool := FanOut{
		errChan: make(chan error, size),
		size:    make(chan bool, size),
		errs:    make([]error, 0),
	}
	pool.start()
	return &pool
}

func (p *FanOut) start() {
	p.wg.Add(1)
	go func() {
		for {
			select {
			case err, ok := <-p.errChan:
				if !ok {
					p.wg.Done()
					return
				}
				p.errs = append(p.errs, err)
			}
		}
	}()
}

// Run a new routine with an optional data value
func (p *FanOut) Run(callBack func(interface{}) error, data interface{}) {
	p.size <- true
	go func() {
		err := callBack(data)
		if err != nil {
			p.errChan <- err
		}
		<-p.size
	}()
}

// Wait for all the routines to complete and return any errors
func (p *FanOut) Wait() []error {
	// Wait for all the routines to complete
	for i := 0; i < cap(p.size); i++ {
		p.size <- true
	}
	// Close the err channel
	if p.errChan != nil {
		close(p.errChan)
	}

	// Wait until the error collector routine is complete
	p.wg.Wait()

	// If there are no errors
	if len(p.errs) == 0 {
		return nil
	}
	return p.errs
}
