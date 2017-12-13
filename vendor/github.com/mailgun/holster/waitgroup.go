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

type WaitGroup struct {
	wg    sync.WaitGroup
	mutex sync.Mutex
	errs  []error
	done  chan struct{}
}

// Run a routine and collect errors if any
//func (wg *WaitGroup) Run(callBack func() error) {
func (wg *WaitGroup) Run(callBack func(interface{}) error, data interface{}) {
	wg.wg.Add(1)
	go func() {
		err := callBack(data)
		if err == nil {
			wg.wg.Done()
			return
		}
		wg.mutex.Lock()
		wg.errs = append(wg.errs, err)
		wg.wg.Done()
		wg.mutex.Unlock()
	}()
}

// Run a goroutine in a loop continuously, if the callBack returns false the loop is broken.
// `Until()` differs from `Loop()` in that if the `Stop()` is called on the WaitGroup
// the `done` channel is closed. Implementations of the callBack function can listen
// for the close to indicate a stop was requested.
func (wg *WaitGroup) Until(callBack func(done chan struct{}) bool) {
	wg.mutex.Lock()
	if wg.done == nil {
		wg.done = make(chan struct{})
	}
	wg.mutex.Unlock()

	wg.wg.Add(1)
	go func() {
		for {
			if !callBack(wg.done) {
				wg.wg.Done()
				break
			}
		}
	}()
}

// closes the done channel passed into `Until()` calls and waits for the `Until()` callBack to return false
func (wg *WaitGroup) Stop() {
	wg.mutex.Lock()
	if wg.done != nil {
		close(wg.done)
	}
	wg.mutex.Unlock()
	wg.Wait()
}

// Run a goroutine in a loop continuously, if the callBack returns false the loop is broken
func (wg *WaitGroup) Loop(callBack func() bool) {
	wg.wg.Add(1)
	go func() {
		for {
			if !callBack() {
				wg.wg.Done()
				break
			}
		}
	}()
}

// Wait for all the routines to complete and return any errors collected
func (wg *WaitGroup) Wait() []error {
	wg.wg.Wait()

	wg.mutex.Lock()
	defer wg.mutex.Unlock()

	if len(wg.errs) == 0 {
		return nil
	}
	return wg.errs
}
