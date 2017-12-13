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

// Package reversetunnel sets up persistent reverse tunnel
// between remote site and teleport proxy, when site agents
// dial to teleport proxy's socket and teleport proxy can connect
// to any server through this tunnel.
package reversetunnel

import (
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/gravitational/teleport"
	"github.com/gravitational/teleport/lib/auth"
	"github.com/gravitational/teleport/lib/defaults"
	"github.com/gravitational/teleport/lib/services"
	"github.com/gravitational/teleport/lib/sshutils"
	"github.com/gravitational/teleport/lib/utils"
	"github.com/gravitational/teleport/lib/utils/proxy"

	"github.com/gravitational/trace"
	log "github.com/sirupsen/logrus"
	"golang.org/x/crypto/ssh"
)

// Agent is a reverse tunnel agent running as a part of teleport Proxies
// to establish outbound reverse tunnels to remote proxies
type Agent struct {
	log  *log.Entry
	addr utils.NetAddr
	clt  *auth.TunClient
	// domain name of the tunnel server, used only for debugging & logging
	remoteDomainName string
	// clientName format is "hostid.domain" (where 'domain' is local domain name)
	clientName      string
	broadcastClose  *utils.CloseBroadcaster
	disconnectC     chan bool
	hostKeyCallback utils.HostKeyCallback
	authMethods     []ssh.AuthMethod
	accessPoint     auth.AccessPoint
}

// AgentOption specifies parameter that could be passed to Agents
type AgentOption func(a *Agent) error

// NewAgent returns a new reverse tunnel agent
// Parameters:
//	  addr points to the remote reverse tunnel server
//    remoteDomainName is the domain name of the runnel server, used only for logging
//    clientName is hostid.domain (where 'domain' is local domain name)
func NewAgent(
	addr utils.NetAddr,
	remoteDomainName string,
	clientName string,
	signers []ssh.Signer,
	clt *auth.TunClient,
	accessPoint auth.AccessPoint) (*Agent, error) {

	log.Debugf("reversetunnel.NewAgent %s -> %s", clientName, remoteDomainName)

	a := &Agent{
		log: log.WithFields(log.Fields{
			teleport.Component: teleport.ComponentReverseTunnel,
			teleport.ComponentFields: map[string]interface{}{
				"side":   "agent",
				"remote": addr.String(),
				"mode":   "agent",
			},
		}),
		clt:              clt,
		addr:             addr,
		remoteDomainName: remoteDomainName,
		clientName:       clientName,
		broadcastClose:   utils.NewCloseBroadcaster(),
		disconnectC:      make(chan bool, 10),
		authMethods:      []ssh.AuthMethod{ssh.PublicKeys(signers...)},
		accessPoint:      accessPoint,
	}
	a.hostKeyCallback = a.checkHostSignature
	return a, nil
}

// Close signals to close all connections
func (a *Agent) Close() error {
	return a.broadcastClose.Close()
}

// Start starts agent that attempts to connect to remote server part
func (a *Agent) Start() error {
	conn, err := a.connect()
	if err != nil {
		log.Errorf("Failed to create remote tunnel for %v on %s(%s): %v",
			a.clientName, a.remoteDomainName, a.addr.FullAddress(), err)
	}
	// start heartbeat even if error happend, it will reconnect
	go a.runHeartbeat(conn)
	return err
}

// Wait waits until all outstanding operations are completed
func (a *Agent) Wait() error {
	return nil
}

// String returns debug-friendly
func (a *Agent) String() string {
	return fmt.Sprintf("tunagent(remote=%s)", a.addr.String())
}

func (a *Agent) checkHostSignature(hostport string, remote net.Addr, key ssh.PublicKey) error {
	cert, ok := key.(*ssh.Certificate)
	if !ok {
		return trace.BadParameter("expected certificate")
	}
	cas, err := a.accessPoint.GetCertAuthorities(services.HostCA, false)
	if err != nil {
		return trace.Wrap(err, "failed to fetch remote certs")
	}
	for _, ca := range cas {
		checkers, err := ca.Checkers()
		if err != nil {
			return trace.BadParameter("error parsing key: %v", err)
		}
		for _, checker := range checkers {
			if sshutils.KeysEqual(checker, cert.SignatureKey) {
				a.log.Debugf("matched key %v for %v", ca.GetName(), hostport)
				return nil
			}
		}
	}
	return trace.NotFound(
		"no matching keys found when checking server's host signature")
}

func (a *Agent) connect() (conn *ssh.Client, err error) {
	if a.addr.IsEmpty() {
		return nil, trace.BadParameter("reverse tunnel cannot be created: target address is empty")
	}
	for _, authMethod := range a.authMethods {
		// if http_proxy is set, dial through the proxy
		dialer := proxy.DialerFromEnvironment()
		conn, err = dialer.Dial(a.addr.AddrNetwork, a.addr.Addr, &ssh.ClientConfig{
			User:            a.clientName,
			Auth:            []ssh.AuthMethod{authMethod},
			HostKeyCallback: a.hostKeyCallback,
			Timeout:         defaults.DefaultDialTimeout,
		})
		if conn != nil {
			break
		}
	}
	return conn, err
}

func (a *Agent) proxyAccessPoint(ch ssh.Channel, req <-chan *ssh.Request) {
	log.Debugf("[HA Agent] proxyAccessPoint")
	defer ch.Close()

	conn, err := a.clt.GetDialer()()
	if err != nil {
		a.log.Errorf("error dialing: %v", err)
		return
	}

	// apply read/write timeouts to this connection that are 10x of what normal
	// reverse tunnel ping is supposed to be:
	conn = utils.ObeyIdleTimeout(conn,
		defaults.ReverseTunnelAgentHeartbeatPeriod*10,
		"reverse tunnel client")

	wg := sync.WaitGroup{}
	wg.Add(2)

	go func() {
		defer wg.Done()
		defer conn.Close()
		io.Copy(conn, ch)
	}()

	go func() {
		defer wg.Done()
		defer conn.Close()
		io.Copy(ch, conn)
	}()

	wg.Wait()
}

// proxyTransport runs as a goroutine running inside a reverse tunnel client
// and it establishes and maintains the following remote connection:
//
// tsh -> proxy(also reverse-tunnel-server) -> reverse-tunnel-agent
//
// ch   : SSH channel which received "teleport-transport" out-of-band request
// reqC : request payload
func (a *Agent) proxyTransport(ch ssh.Channel, reqC <-chan *ssh.Request) {
	log.Debugf("[HA Agent] proxyTransport")
	defer ch.Close()

	// always push space into stderr to make sure the caller can always
	// safely call read(stderr) without blocking. this stderr is only used
	// to request proxying of TCP/IP via reverse tunnel.
	fmt.Fprint(ch.Stderr(), " ")

	var req *ssh.Request
	select {
	case <-a.broadcastClose.C:
		a.log.Infof("is closed, returning")
		return
	case req = <-reqC:
		if req == nil {
			a.log.Infof("connection closed, returning")
			return
		}
	case <-time.After(defaults.DefaultDialTimeout):
		a.log.Errorf("timeout waiting for dial")
		return
	}

	server := string(req.Payload)
	var servers []string

	// if the request is for the special string @remote-auth-server, then get the
	// list of auth servers and return that. otherwise try and connect to the
	// passed in server.
	if server == RemoteAuthServer {
		authServers, err := a.clt.GetAuthServers()
		if err != nil {
			a.log.Errorf("unable to find auth servers: %v", err)
			return
		}
		for _, as := range authServers {
			servers = append(servers, as.GetAddr())
		}
	} else {
		servers = append(servers, server)
	}

	log.Debugf("got out of band request %v", servers)

	var conn net.Conn
	var err error

	// loop over all servers and try and connect to one of them
	for _, s := range servers {
		conn, err = net.Dial("tcp", s)
		if err == nil {
			break
		}

		// log the reason we were not able to connect
		log.Debugf(trace.DebugReport(err))
	}

	// if we were not able to connect to any server, write the last connection
	// error to stderr of the caller (via SSH channel) so the error will be
	// propagated all the way back to the client (most likely tsh)
	if err != nil {
		fmt.Fprint(ch.Stderr(), err.Error())
		req.Reply(false, []byte(err.Error()))
		return
	}

	// successfully dialed
	req.Reply(true, []byte("connected"))
	a.log.Infof("successfully dialed to %v, start proxying", server)

	wg := sync.WaitGroup{}
	wg.Add(2)

	go func() {
		defer wg.Done()
		// make sure that we close the client connection on a channel
		// close, otherwise the other goroutine would never know
		// as it will block on read from the connection
		defer conn.Close()
		io.Copy(conn, ch)
	}()

	go func() {
		defer wg.Done()
		io.Copy(ch, conn)
	}()

	wg.Wait()
}

// runHeartbeat is a blocking function which runs in a loop sending heartbeats
// to the given SSH connection.
//
func (a *Agent) runHeartbeat(conn *ssh.Client) {
	ticker := time.NewTicker(defaults.ReverseTunnelAgentHeartbeatPeriod)
	defer ticker.Stop()

	heartbeatLoop := func() error {
		if conn == nil {
			return trace.Errorf("heartbeat cannot ping: need to reconnect")
		}
		log.Infof("[TUNNEL CLIENT] connected to %s", conn.RemoteAddr())
		defer conn.Close()
		hb, reqC, err := conn.OpenChannel(chanHeartbeat, nil)
		if err != nil {
			return trace.Wrap(err)
		}
		newAccesspointC := conn.HandleChannelOpen(chanAccessPoint)
		newTransportC := conn.HandleChannelOpen(chanTransport)

		// send first ping right away, then start a ping timer:
		hb.SendRequest("ping", false, nil)

		for {
			select {
			// need to exit:
			case <-a.broadcastClose.C:
				return nil
			// time to ping:
			case <-ticker.C:
				log.Debugf("[TUNNEL CLIENT] pings \"%s\" at %s", a.remoteDomainName, conn.RemoteAddr())
				_, err := hb.SendRequest("ping", false, nil)
				if err != nil {
					log.Error(err)
					return trace.Wrap(err)
				}
			// ssh channel closed:
			case req := <-reqC:
				if req == nil {
					return trace.Errorf("heartbeat: connection closed")
				}
			// new access point request:
			case nch := <-newAccesspointC:
				if nch == nil {
					continue
				}
				a.log.Infof("[TUNNEL CLIENT] access point request: %v", nch.ChannelType())
				ch, req, err := nch.Accept()
				if err != nil {
					a.log.Errorf("failed to accept request: %v", err)
					continue
				}
				go a.proxyAccessPoint(ch, req)
			// new transport request:
			case nch := <-newTransportC:
				if nch == nil {
					continue
				}
				a.log.Infof("[TUNNEL CLIENT] transport request: %v", nch.ChannelType())
				ch, req, err := nch.Accept()
				if err != nil {
					a.log.Errorf("failed to accept request: %v", err)
					continue
				}
				go a.proxyTransport(ch, req)
			}
		}
	}

	// run heartbeat loop, and when it fails (probably means that a tunnel got disconnected)
	// keep repeating to reconnect until we're asked to stop
	err := heartbeatLoop()

	// when this happens, this is #1 issue we have right now with Teleport. So I'm making
	// it EASY to see in the logs. This condition should never be permanent (like repeates
	// every XX seconds)
	if err != nil {
		log.Warn(err)
	}

	if err != nil || conn == nil {
		select {
		// abort if asked to stop:
		case <-a.broadcastClose.C:
			return
			// reconnect
		case <-ticker.C:
			a.Start()
		}
	}
}

const (
	chanHeartbeat        = "teleport-heartbeat"
	chanAccessPoint      = "teleport-access-point"
	chanTransport        = "teleport-transport"
	chanTransportDialReq = "teleport-transport-dial"
)

const (
	// RemoteSiteStatusOffline indicates that site is considered as
	// offline, since it has missed a series of heartbeats
	RemoteSiteStatusOffline = "offline"
	// RemoteSiteStatusOnline indicates that site is sending heartbeats
	// at expected interval
	RemoteSiteStatusOnline = "online"
)

// RemoteAuthServer is a special non-resolvable address that indicates we want
// a connection to the remote auth server.
const RemoteAuthServer = "@remote-auth-server"
