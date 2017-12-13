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
	"github.com/sirupsen/logrus"
	. "gopkg.in/check.v1"
)

type MiscTestSuite struct{}

var _ = Suite(&MiscTestSuite{})

func (s *MiscTestSuite) SetUpSuite(c *C) {
}

func (s *MiscTestSuite) TestToFieldsStruct(c *C) {
	var conf struct {
		Foo string
		Bar int
	}
	conf.Bar = 23
	conf.Foo = "bar"

	fields := holster.ToFields(conf)
	c.Assert(fields, DeepEquals, logrus.Fields{
		"Foo": "bar",
		"Bar": 23,
	})
}

func (s *MiscTestSuite) TestToFieldsMap(c *C) {
	conf := map[string]interface{}{
		"Bar": 23,
		"Foo": "bar",
	}

	fields := holster.ToFields(conf)
	c.Assert(fields, DeepEquals, logrus.Fields{
		"Foo": "bar",
		"Bar": 23,
	})
}

func (s *MiscTestSuite) TestToFieldsPanic(c *C) {
	defer func() {
		if r := recover(); r != nil {
			c.Assert(r, Equals, "ToFields(): value must be of kind struct or map")
		}
	}()

	// Should panic
	holster.ToFields(1)
	c.Fatalf("Should have caught panic")
}
