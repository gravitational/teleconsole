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
	"net"

	"github.com/gravitational/trace"
	"gopkg.in/alecthomas/kingpin.v2"
)

// CIDRFlag returns CIDR range flag
func CIDRFlag(s kingpin.Settings) *CIDR {
	vars := new(CIDR)
	s.SetValue(vars)
	return vars
}

// ParseCIDR parses value of the CIDR from string
func ParseCIDR(v string) (*CIDR, error) {
	ip, ipnet, err := net.ParseCIDR(v)
	if err != nil {
		return nil, trace.BadParameter("failed to parse CIDR(%v): %v", v, err.Error())
	}
	return &CIDR{val: v, ip: ip, ipnet: *ipnet}, nil
}

// CIDR adds several helper methods over subnet range
type CIDR struct {
	val   string
	ip    net.IP
	ipnet net.IPNet
}

func (c *CIDR) Set(v string) error {
	out, err := ParseCIDR(v)
	if err != nil {
		return trace.Wrap(err)
	}
	*c = *out
	return nil
}

func (c *CIDR) String() string {
	return c.ipnet.String()
}

// FirstIP returns the first IP in this subnet that is not .0
func (c *CIDR) FirstIP() net.IP {
	var ip net.IP
	for ip = IncIP(c.ip.Mask(c.ipnet.Mask)); c.ipnet.Contains(ip); IncIP(ip) {
		break
	}
	return ip
}

// RelativeIP returns an IP given an offset from the first IP in the range.
// offset starts at 0, i.e. c.RelativeIP(0) == c.FirstIP()
func (c *CIDR) RelativeIP(offset int) net.IP {
	var ip net.IP
	for ip = IncIP(c.ip.Mask(c.ipnet.Mask)); c.ipnet.Contains(ip) && offset > 0; IncIP(ip) {
		offset--
	}
	return ip
}

func IncIP(ip net.IP) net.IP {
	for j := len(ip) - 1; j >= 0; j-- {
		ip[j]++
		if ip[j] > 0 {
			break
		}
	}
	return ip
}
