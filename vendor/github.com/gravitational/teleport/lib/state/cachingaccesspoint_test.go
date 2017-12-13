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

package state

import (
	"os"
	"testing"
	"time"

	"github.com/gravitational/teleport/lib/auth"
	"github.com/gravitational/teleport/lib/auth/testauthority"
	"github.com/gravitational/teleport/lib/backend"
	"github.com/gravitational/teleport/lib/backend/boltbk"
	"github.com/gravitational/teleport/lib/backend/dir"
	"github.com/gravitational/teleport/lib/defaults"
	"github.com/gravitational/teleport/lib/services"
	"github.com/gravitational/teleport/lib/utils"

	"github.com/gravitational/trace"
	"github.com/jonboulle/clockwork"
	"gopkg.in/check.v1"
)

// fake cluster we're testing on:
var (
	Nodes = []services.ServerV1{
		{
			ID:        "1",
			Addr:      "10.50.0.1",
			Hostname:  "one",
			Labels:    make(map[string]string),
			CmdLabels: make(map[string]services.CommandLabelV1),
			Namespace: defaults.Namespace,
		},
		{
			ID:        "2",
			Addr:      "10.50.0.2",
			Hostname:  "two",
			Labels:    make(map[string]string),
			CmdLabels: make(map[string]services.CommandLabelV1),
			Namespace: defaults.Namespace,
		},
	}
	Proxies = []services.ServerV1{
		{
			ID:       "3",
			Addr:     "10.50.0.3",
			Hostname: "three",
			Labels:   map[string]string{"os": "linux", "role": "proxy"},
			CmdLabels: map[string]services.CommandLabelV1{
				"uptime": {Period: time.Second, Command: []string{"uptime"}},
			},
		},
	}
	Users = []services.UserV1{
		{
			Name:           "elliot",
			AllowedLogins:  []string{"elliot", "root"},
			OIDCIdentities: []services.ExternalIdentity{},
		},
		{
			Name:          "bob",
			AllowedLogins: []string{"bob"},
			OIDCIdentities: []services.ExternalIdentity{
				{
					ConnectorID: "example.com",
					Username:    "bob@example.com",
				},
				{
					ConnectorID: "example.net",
					Username:    "bob@example.net",
				},
			},
		},
	}
)

type ClusterSnapshotSuite struct {
	dataDir    string
	backend    backend.Backend
	authServer *auth.AuthServer
	clock      clockwork.Clock
}

var _ = check.Suite(&ClusterSnapshotSuite{})

// bootstrap check
func TestState(t *testing.T) { check.TestingT(t) }

func (s *ClusterSnapshotSuite) SetUpSuite(c *check.C) {
	utils.InitLoggerForTests()
	s.clock = clockwork.NewRealClock()
}

func (s *ClusterSnapshotSuite) SetUpTest(c *check.C) {
	// create a new auth server:
	s.dataDir = c.MkDir()
	var err error
	s.backend, err = boltbk.New(backend.Params{"path": s.dataDir})
	c.Assert(err, check.IsNil)

	clusterName, err := services.NewClusterName(services.ClusterNameSpecV2{
		ClusterName: "localhost",
	})
	c.Assert(err, check.IsNil)
	staticTokens, err := services.NewStaticTokens(services.StaticTokensSpecV2{
		StaticTokens: []services.ProvisionToken{},
	})
	c.Assert(err, check.IsNil)
	s.authServer = auth.NewAuthServer(&auth.InitConfig{
		Backend:      s.backend,
		Authority:    testauthority.New(),
		ClusterName:  clusterName,
		StaticTokens: staticTokens,
	})
	err = s.authServer.UpsertNamespace(
		services.NewNamespace(defaults.Namespace))
	c.Assert(err, check.IsNil)
	// add some nodes to it:
	for _, n := range Nodes {
		v2 := n.V2()
		v2.SetTTL(s.clock, defaults.ServerHeartbeatTTL)
		err = s.authServer.UpsertNode(v2)
		c.Assert(err, check.IsNil)
	}
	// add some proxies to it:
	for _, p := range Proxies {
		v2 := p.V2()
		v2.SetTTL(s.clock, defaults.ServerHeartbeatTTL)
		err = s.authServer.UpsertProxy(v2)
		c.Assert(err, check.IsNil)
	}
	// add some users to it:
	for _, u := range Users {
		v2 := u.V2()
		err = s.authServer.UpsertUser(v2)
		c.Assert(err, check.IsNil)
	}
}

func (s *ClusterSnapshotSuite) TearDownTest(c *check.C) {
	s.authServer.Close()
	s.backend.Close()
	os.RemoveAll(s.dataDir)
}

func (s *ClusterSnapshotSuite) TestEverything(c *check.C) {
	cacheBackend, err := dir.New(backend.Params{"path": c.MkDir()})
	c.Assert(err, check.IsNil)
	snap, err := NewCachingAuthClient(Config{
		AccessPoint: s.authServer,
		Clock:       s.clock,
		Backend:     cacheBackend,
	})
	c.Assert(err, check.IsNil)
	c.Assert(snap, check.NotNil)

	// kill the 'upstream' server:
	s.authServer.Close()

	users, err := snap.GetUsers()
	c.Assert(err, check.IsNil)
	c.Assert(users, check.HasLen, len(Users))

	nodes, err := snap.GetNodes(defaults.Namespace)
	c.Assert(err, check.IsNil)
	c.Assert(nodes, check.HasLen, len(Nodes))

	proxies, err := snap.GetProxies()
	c.Assert(err, check.IsNil)
	c.Assert(proxies, check.HasLen, len(Proxies))
}

func (s *ClusterSnapshotSuite) TestTry(c *check.C) {
	var (
		successfullCalls int
		failedCalls      int
	)
	success := func() error { successfullCalls++; return nil }
	failure := func() error { failedCalls++; return trace.ConnectionProblem(nil, "lost uplink") }

	cacheBackend, err := dir.New(backend.Params{"path": c.MkDir()})
	c.Assert(err, check.IsNil)
	ap, err := NewCachingAuthClient(Config{
		AccessPoint: s.authServer,
		Clock:       s.clock,
		Backend:     cacheBackend,
	})
	c.Assert(err, check.IsNil)

	ap.try(success)
	ap.try(failure)

	c.Assert(successfullCalls, check.Equals, 1)
	c.Assert(failedCalls, check.Equals, 1)

	// these two calls should not happen because of a recent failure:
	ap.try(success)
	ap.try(failure)

	c.Assert(successfullCalls, check.Equals, 1)
	c.Assert(failedCalls, check.Equals, 1)

	// "wait" for backoff duration and try again:
	ap.lastErrorTime = time.Now().Add(-backoffDuration)

	ap.try(success)
	ap.try(failure)

	c.Assert(successfullCalls, check.Equals, 2)
	c.Assert(failedCalls, check.Equals, 2)
}
