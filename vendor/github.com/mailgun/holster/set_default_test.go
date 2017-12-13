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
	"github.com/mailgun/holster"
	. "gopkg.in/check.v1"
)

type SetDefaultTestSuite struct{}

var _ = Suite(&SetDefaultTestSuite{})

func (s *SetDefaultTestSuite) SetUpSuite(c *C) {
}

func (s *SetDefaultTestSuite) TestIfEmpty(c *C) {
	var conf struct {
		Foo string
		Bar int
	}
	c.Assert(conf.Foo, Equals, "")
	c.Assert(conf.Bar, Equals, 0)

	// Should apply the default values
	holster.SetDefault(&conf.Foo, "default")
	holster.SetDefault(&conf.Bar, 200)

	c.Assert(conf.Foo, Equals, "default")
	c.Assert(conf.Bar, Equals, 200)

	conf.Foo = "thrawn"
	conf.Bar = 500

	// Should NOT apply the default values
	holster.SetDefault(&conf.Foo, "default")
	holster.SetDefault(&conf.Bar, 200)

	c.Assert(conf.Foo, Equals, "thrawn")
	c.Assert(conf.Bar, Equals, 500)
}

func (s *SetDefaultTestSuite) TestIsEmpty(c *C) {
	var count64 int64
	var thing string

	// Should return true
	c.Assert(holster.IsZero(count64), Equals, true)
	c.Assert(holster.IsZero(thing), Equals, true)

	thing = "thrawn"
	count64 = int64(1)
	c.Assert(holster.IsZero(count64), Equals, false)
	c.Assert(holster.IsZero(thing), Equals, false)
}

func (s *SetDefaultTestSuite) TestIfEmptyTypePanic(c *C) {
	defer func() {
		if r := recover(); r != nil {
			c.Assert(r, Equals, "reflect.Set: value of type int is not assignable to type string")
		}
	}()

	var thing string
	// Should panic
	holster.SetDefault(&thing, 1)
	c.Fatalf("Should have caught panic")
}

func (s *SetDefaultTestSuite) TestIfEmptyNonPtrPanic(c *C) {
	defer func() {
		if r := recover(); r != nil {
			c.Assert(r, Equals, "holster.IfEmpty: Expected first argument to be of type reflect.Ptr")
		}
	}()

	var thing string
	// Should panic
	holster.SetDefault(thing, "thrawn")
	c.Fatalf("Should have caught panic")
}
