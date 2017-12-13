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

	"github.com/mailgun/holster"
	. "gopkg.in/check.v1"
)

type MinHeapSuite struct{}

var _ = Suite(&MinHeapSuite{})

func toPtr(i int) interface{} {
	return &i
}

func toInt(i interface{}) int {
	return *(i.(*int))
}

func (s *MinHeapSuite) TestPeek(c *C) {
	mh := holster.NewPriorityQueue()

	el := &holster.PQItem{
		Value:    toPtr(1),
		Priority: 5,
	}

	mh.Push(el)
	c.Assert(toInt(mh.Peek().Value), Equals, 1)
	c.Assert(mh.Len(), Equals, 1)

	el = &holster.PQItem{
		Value:    toPtr(2),
		Priority: 1,
	}
	mh.Push(el)
	c.Assert(mh.Len(), Equals, 2)
	c.Assert(toInt(mh.Peek().Value), Equals, 2)
	c.Assert(toInt(mh.Peek().Value), Equals, 2)
	c.Assert(mh.Len(), Equals, 2)

	el = mh.Pop()

	c.Assert(toInt(el.Value), Equals, 2)
	c.Assert(mh.Len(), Equals, 1)
	c.Assert(toInt(mh.Peek().Value), Equals, 1)

	mh.Pop()
	c.Assert(mh.Len(), Equals, 0)
}

func (s *MinHeapSuite) TestUpdate(c *C) {
	mh := holster.NewPriorityQueue()
	x := &holster.PQItem{
		Value:    toPtr(1),
		Priority: 4,
	}
	y := &holster.PQItem{
		Value:    toPtr(2),
		Priority: 3,
	}
	z := &holster.PQItem{
		Value:    toPtr(3),
		Priority: 8,
	}
	mh.Push(x)
	mh.Push(y)
	mh.Push(z)
	c.Assert(toInt(mh.Peek().Value), Equals, 2)

	mh.Update(z, 1)
	c.Assert(toInt(mh.Peek().Value), Equals, 3)

	mh.Update(x, 0)
	c.Assert(toInt(mh.Peek().Value), Equals, 1)
}

func Example_Priority_Queue_Usage() {
	queue := holster.NewPriorityQueue()

	queue.Push(&holster.PQItem{
		Value:    "thing3",
		Priority: 3,
	})

	queue.Push(&holster.PQItem{
		Value:    "thing1",
		Priority: 1,
	})

	queue.Push(&holster.PQItem{
		Value:    "thing2",
		Priority: 2,
	})

	// Pops item off the queue according to the priority instead of the Push() order
	item := queue.Pop()

	fmt.Printf("Item: %s", item.Value.(string))

	// Output: Item: thing1
}
