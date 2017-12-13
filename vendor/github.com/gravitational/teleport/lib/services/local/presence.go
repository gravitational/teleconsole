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

package local

import (
	"encoding/json"
	"sort"

	"github.com/gravitational/teleport/lib/backend"
	"github.com/gravitational/teleport/lib/services"

	"github.com/gravitational/trace"
)

// PresenceService records and reports the presence of all components
// of the cluster - Nodes, Proxies and SSH nodes
type PresenceService struct {
	backend.Backend
}

// NewPresenceService returns new presence service instance
func NewPresenceService(backend backend.Backend) *PresenceService {
	return &PresenceService{Backend: backend}
}

// UpsertLocalClusterName upserts local domain
func (s *PresenceService) UpsertLocalClusterName(name string) error {
	return s.UpsertVal([]string{localClusterPrefix}, "val", []byte(name), backend.Forever)
}

// GetLocalClusterName upserts local domain
func (s *PresenceService) GetLocalClusterName() (string, error) {
	data, err := s.GetVal([]string{localClusterPrefix}, "val")
	if err != nil {
		return "", trace.Wrap(err)
	}
	return string(data), nil
}

// DeleteAllNamespaces deletes all namespaces
func (s *PresenceService) DeleteAllNamespaces() error {
	return s.DeleteBucket([]string{}, namespacesPrefix)
}

// GetNamespaces returns a list of namespaces
func (s *PresenceService) GetNamespaces() ([]services.Namespace, error) {
	keys, err := s.GetKeys([]string{namespacesPrefix})
	if err != nil {
		return nil, trace.Wrap(err)
	}
	out := make([]services.Namespace, len(keys))
	for i, name := range keys {
		u, err := s.GetNamespace(name)
		if err != nil {
			return nil, trace.Wrap(err)
		}
		out[i] = *u
	}
	sort.Sort(services.SortedNamespaces(out))
	return out, nil
}

// UpsertNamespace upserts namespace
func (s *PresenceService) UpsertNamespace(n services.Namespace) error {
	data, err := json.Marshal(n)
	if err != nil {
		return trace.Wrap(err)
	}
	ttl := backend.TTL(s.Clock(), n.Metadata.Expiry())
	err = s.UpsertVal([]string{namespacesPrefix, n.Metadata.Name}, "params", []byte(data), ttl)
	if err != nil {
		return trace.Wrap(err)
	}
	return nil
}

// GetNamespace returns a namespace by name
func (s *PresenceService) GetNamespace(name string) (*services.Namespace, error) {
	if name == "" {
		return nil, trace.BadParameter("missing namespace name")
	}
	data, err := s.GetVal([]string{namespacesPrefix, name}, "params")
	if err != nil {
		if trace.IsNotFound(err) {
			return nil, trace.NotFound("namespace %v is not found", name)
		}
		return nil, trace.Wrap(err)
	}
	return services.UnmarshalNamespace(data)
}

// DeleteNamespace deletes a namespace with all the keys from the backend
func (s *PresenceService) DeleteNamespace(namespace string) error {
	if namespace == "" {
		return trace.BadParameter("missing namespace name")
	}
	err := s.DeleteBucket([]string{namespacesPrefix}, namespace)
	if err != nil {
		if trace.IsNotFound(err) {
			return trace.NotFound("namespace '%v' is not found", namespace)
		}
	}
	return trace.Wrap(err)
}

func (s *PresenceService) getServers(kind, prefix string) ([]services.Server, error) {
	keys, err := s.GetKeys([]string{prefix})
	if err != nil {
		return nil, trace.Wrap(err)
	}
	servers := make([]services.Server, len(keys))
	for i, key := range keys {
		data, err := s.GetVal([]string{prefix}, key)
		if err != nil {
			return nil, trace.Wrap(err)
		}
		server, err := services.GetServerMarshaler().UnmarshalServer(data, kind)
		if err != nil {
			return nil, trace.Wrap(err)
		}
		servers[i] = server
	}
	// sorting helps with tests and makes it all deterministic
	sort.Sort(services.SortedServers(servers))
	return servers, nil
}

func (s *PresenceService) upsertServer(prefix string, server services.Server) error {
	ttl := backend.TTL(s.Clock(), server.Expiry())
	data, err := services.GetServerMarshaler().MarshalServer(server)
	if err != nil {
		return trace.Wrap(err)
	}
	err = s.UpsertVal([]string{prefix}, server.GetName(), data, ttl)
	return trace.Wrap(err)
}

// DeleteAllNodes deletes all nodes in a namespace
func (s *PresenceService) DeleteAllNodes(namespace string) error {
	return s.DeleteBucket([]string{namespacesPrefix, namespace}, nodesPrefix)
}

// GetNodes returns a list of registered servers
func (s *PresenceService) GetNodes(namespace string) ([]services.Server, error) {
	if namespace == "" {
		return nil, trace.BadParameter("missing namespace value")
	}
	keys, err := s.GetKeys([]string{namespacesPrefix, namespace, nodesPrefix})
	if err != nil {
		return nil, trace.Wrap(err)
	}
	servers := make([]services.Server, 0, len(keys))
	for _, key := range keys {
		data, err := s.GetVal([]string{namespacesPrefix, namespace, nodesPrefix}, key)
		if err != nil {
			if trace.IsNotFound(err) {
				continue
			}
			return nil, trace.Wrap(err)
		}
		server, err := services.GetServerMarshaler().UnmarshalServer(data, services.KindNode)
		if err != nil {
			return nil, trace.Wrap(err)
		}
		servers = append(servers, server)
	}
	// sorting helps with tests and makes it all deterministic
	sort.Sort(services.SortedServers(servers))
	return servers, nil
}

// UpsertNode registers node presence, permanently if ttl is 0 or
// for the specified duration with second resolution if it's >= 1 second
func (s *PresenceService) UpsertNode(server services.Server) error {
	if server.GetNamespace() == "" {
		return trace.BadParameter("missing node namespace")
	}
	data, err := services.GetServerMarshaler().MarshalServer(server)
	if err != nil {
		return trace.Wrap(err)
	}
	ttl := backend.TTL(s.Clock(), server.Expiry())
	err = s.UpsertVal([]string{namespacesPrefix, server.GetNamespace(), nodesPrefix}, server.GetName(), data, ttl)
	return trace.Wrap(err)
}

// GetAuthServers returns a list of registered servers
func (s *PresenceService) GetAuthServers() ([]services.Server, error) {
	return s.getServers(services.KindAuthServer, authServersPrefix)
}

// UpsertAuthServer registers auth server presence, permanently if ttl is 0 or
// for the specified duration with second resolution if it's >= 1 second
func (s *PresenceService) UpsertAuthServer(server services.Server) error {
	return s.upsertServer(authServersPrefix, server)
}

// UpsertProxy registers proxy server presence, permanently if ttl is 0 or
// for the specified duration with second resolution if it's >= 1 second
func (s *PresenceService) UpsertProxy(server services.Server) error {
	return s.upsertServer(proxiesPrefix, server)
}

// GetProxies returns a list of registered proxies
func (s *PresenceService) GetProxies() ([]services.Server, error) {
	return s.getServers(services.KindProxy, proxiesPrefix)
}

// DeleteAllProxies deletes all proxies
func (s *PresenceService) DeleteAllProxies() error {
	return s.DeleteBucket([]string{}, proxiesPrefix)
}

// DeleteAllReverseTunnels deletes all reverse tunnels
func (s *PresenceService) DeleteAllReverseTunnels() error {
	return s.DeleteBucket([]string{}, reverseTunnelsPrefix)
}

// UpsertReverseTunnel upserts reverse tunnel entry temporarily or permanently
func (s *PresenceService) UpsertReverseTunnel(tunnel services.ReverseTunnel) error {
	if err := tunnel.Check(); err != nil {
		return trace.Wrap(err)
	}
	data, err := services.GetReverseTunnelMarshaler().MarshalReverseTunnel(tunnel)
	if err != nil {
		return trace.Wrap(err)
	}
	ttl := backend.TTL(s.Clock(), tunnel.Expiry())
	err = s.UpsertVal([]string{reverseTunnelsPrefix}, tunnel.GetName(), data, ttl)
	return trace.Wrap(err)
}

// GetReverseTunnels returns a list of registered servers
func (s *PresenceService) GetReverseTunnels() ([]services.ReverseTunnel, error) {
	keys, err := s.GetKeys([]string{reverseTunnelsPrefix})
	if err != nil {
		return nil, trace.Wrap(err)
	}
	tunnels := make([]services.ReverseTunnel, len(keys))
	for i, key := range keys {
		data, err := s.GetVal([]string{reverseTunnelsPrefix}, key)
		if err != nil {
			return nil, trace.Wrap(err)
		}
		tunnel, err := services.GetReverseTunnelMarshaler().UnmarshalReverseTunnel(data)
		if err != nil {
			return nil, trace.Wrap(err)
		}
		tunnels[i] = tunnel
	}
	// sorting helps with tests and makes it all deterministic
	sort.Sort(services.SortedReverseTunnels(tunnels))
	return tunnels, nil
}

// DeleteReverseTunnel deletes reverse tunnel by it's domain name
func (s *PresenceService) DeleteReverseTunnel(domainName string) error {
	err := s.DeleteKey([]string{reverseTunnelsPrefix}, domainName)
	return trace.Wrap(err)
}

// UpsertTrustedCluster creates or updates a TrustedCluster in the backend.
func (s *PresenceService) UpsertTrustedCluster(trustedCluster services.TrustedCluster) error {
	if err := trustedCluster.CheckAndSetDefaults(); err != nil {
		return trace.Wrap(err)
	}
	data, err := services.GetTrustedClusterMarshaler().Marshal(trustedCluster)
	if err != nil {
		return trace.Wrap(err)
	}
	ttl := backend.TTL(s.Clock(), trustedCluster.Expiry())
	err = s.UpsertVal([]string{"trustedclusters"}, trustedCluster.GetName(), []byte(data), ttl)
	if err != nil {
		return trace.Wrap(err)
	}

	return nil
}

// GetTrustedCluster returns a single TrustedCluster by name.
func (s *PresenceService) GetTrustedCluster(name string) (services.TrustedCluster, error) {
	data, err := s.GetVal([]string{"trustedclusters"}, name)
	if err != nil {
		if trace.IsNotFound(err) {
			return nil, trace.NotFound("trusted cluster not found")
		}
		return nil, trace.Wrap(err)
	}

	return services.GetTrustedClusterMarshaler().Unmarshal(data)
}

// GetTrustedClusters returns all TrustedClusters in the backend.
func (s *PresenceService) GetTrustedClusters() ([]services.TrustedCluster, error) {
	keys, err := s.GetKeys([]string{"trustedclusters"})
	if err != nil {
		return nil, trace.Wrap(err)
	}

	out := make([]services.TrustedCluster, len(keys))
	for i, name := range keys {
		tc, err := s.GetTrustedCluster(name)
		if err != nil {
			return nil, trace.Wrap(err)
		}
		out[i] = tc
	}

	sort.Sort(services.SortedTrustedCluster(out))
	return out, nil
}

// DeleteTrustedCluster removes a TrustedCluster from the backend by name.
func (s *PresenceService) DeleteTrustedCluster(name string) error {
	err := s.DeleteKey([]string{"trustedclusters"}, name)
	if err != nil {
		if trace.IsNotFound(err) {
			return trace.NotFound("trusted cluster %q not found", name)
		}
	}

	return trace.Wrap(err)
}

const (
	localClusterPrefix   = "localCluster"
	reverseTunnelsPrefix = "reverseTunnels"
	nodesPrefix          = "nodes"
	namespacesPrefix     = "namespaces"
	authServersPrefix    = "authservers"
	proxiesPrefix        = "proxies"
)
