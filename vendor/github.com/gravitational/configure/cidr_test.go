/*
Copyright 2015 Gravitational, Inc.

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

package configure

import (
	"github.com/gravitational/log"
	. "gopkg.in/check.v1"
)

type CIDRSuite struct {
}

var _ = Suite(&CIDRSuite{})

func (s *CIDRSuite) SetUpSuite(c *C) {
	log.Initialize("console", "INFO")
}

func (s *CIDRSuite) TestCIDR(c *C) {
	subnet, err := ParseCIDR("10.100.0.0/16")
	c.Assert(err, IsNil)
	c.Assert(subnet, NotNil)
	c.Assert(subnet.FirstIP().String(), Equals, "10.100.0.1")
	c.Assert(subnet.RelativeIP(3).String(), Equals, "10.100.0.4")
}
