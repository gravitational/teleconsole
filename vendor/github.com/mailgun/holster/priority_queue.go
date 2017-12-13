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
	"container/heap"
)

// An PQItem is something we manage in a priority queue.
type PQItem struct {
	Value    interface{}
	Priority int // The priority of the item in the queue.
	// The index is needed by update and is maintained by the heap.Interface methods.
	index int // The index of the item in the heap.
}

// Implements a PriorityQueue
type PriorityQueue struct {
	impl *pqImpl
}

func NewPriorityQueue() *PriorityQueue {
	mh := &pqImpl{}
	heap.Init(mh)
	return &PriorityQueue{impl: mh}
}

func (p PriorityQueue) Len() int { return p.impl.Len() }

func (p *PriorityQueue) Push(el *PQItem) {
	heap.Push(p.impl, el)
}

func (p *PriorityQueue) Pop() *PQItem {
	el := heap.Pop(p.impl)
	return el.(*PQItem)
}

func (p *PriorityQueue) Peek() *PQItem {
	return (*p.impl)[0]
}

// Modifies the priority and value of an Item in the queue.
func (p *PriorityQueue) Update(el *PQItem, priority int) {
	heap.Remove(p.impl, el.index)
	el.Priority = priority
	heap.Push(p.impl, el)
}

func (p *PriorityQueue) Remove(el *PQItem) {
	heap.Remove(p.impl, el.index)
}

// Actual Implementation using heap.Interface
type pqImpl []*PQItem

func (mh pqImpl) Len() int { return len(mh) }

func (mh pqImpl) Less(i, j int) bool {
	return mh[i].Priority < mh[j].Priority
}

func (mh pqImpl) Swap(i, j int) {
	mh[i], mh[j] = mh[j], mh[i]
	mh[i].index = i
	mh[j].index = j
}

func (mh *pqImpl) Push(x interface{}) {
	n := len(*mh)
	item := x.(*PQItem)
	item.index = n
	*mh = append(*mh, item)
}

func (mh *pqImpl) Pop() interface{} {
	old := *mh
	n := len(old)
	item := old[n-1]
	item.index = -1 // for safety
	*mh = old[0 : n-1]
	return item
}
