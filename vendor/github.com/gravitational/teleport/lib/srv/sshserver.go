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

// Package srv implements SSH server that supports multiplexing
// tunneling, SSH connections proxying and only supports Key based auth
package srv

import (
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gravitational/teleport"
	"github.com/gravitational/teleport/lib/auth"
	"github.com/gravitational/teleport/lib/defaults"
	"github.com/gravitational/teleport/lib/events"
	"github.com/gravitational/teleport/lib/limiter"
	"github.com/gravitational/teleport/lib/reversetunnel"
	"github.com/gravitational/teleport/lib/services"
	rsession "github.com/gravitational/teleport/lib/session"
	"github.com/gravitational/teleport/lib/sshutils"
	"github.com/gravitational/teleport/lib/teleagent"
	"github.com/gravitational/teleport/lib/utils"

	"github.com/gravitational/trace"
	"github.com/jonboulle/clockwork"
	log "github.com/sirupsen/logrus"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// Server implements SSH server that uses configuration backend and
// certificate-based authentication
type Server struct {
	sync.Mutex

	namespace     string
	addr          utils.NetAddr
	hostname      string
	certChecker   ssh.CertChecker
	srv           *sshutils.Server
	hostSigner    ssh.Signer
	shell         string
	authService   auth.AccessPoint
	reg           *sessionRegistry
	sessionServer rsession.Service
	limiter       *limiter.Limiter

	labels      map[string]string                //static server labels
	cmdLabels   map[string]services.CommandLabel //dymanic server labels
	labelsMutex *sync.Mutex

	proxyMode bool
	proxyTun  reversetunnel.Server

	advertiseIP     net.IP
	proxyPublicAddr utils.NetAddr

	// server UUID gets generated once on the first start and never changes
	// usually stored in a file inside the data dir
	uuid string

	// this gets set to true for unit testing
	isTestStub bool

	// sets to true when the server needs to be stopped
	closer *utils.CloseBroadcaster

	// alog points to the AuditLog this server uses to report
	// auditable events
	alog events.IAuditLog

	// clock is a system clock
	clock clockwork.Clock

	// permitUserEnvironment controls if this server will read ~/.tsh/environment
	// before creating a new session.
	permitUserEnvironment bool

	// ciphers is a list of ciphers that the server supports. If omitted,
	// the defaults will be used.
	ciphers []string

	// kexAlgorithms is a list of key exchange (KEX) algorithms that the
	// server supports. If omitted, the defaults will be used.
	kexAlgorithms []string

	// macAlgorithms is a list of message authentication codes (MAC) that
	// the server supports. If omitted the defaults will be used.
	macAlgorithms []string
}

// ServerOption is a functional option passed to the server
type ServerOption func(s *Server) error

// Close closes listening socket and stops accepting connections
func (s *Server) Close() error {
	s.closer.Close()
	s.reg.Close()
	return s.srv.Close()
}

// Start starts server
func (s *Server) Start() error {
	if len(s.cmdLabels) > 0 {
		s.updateLabels()
	}
	go s.heartbeatPresence()
	return s.srv.Start()
}

// Wait waits until server stops
func (s *Server) Wait() {
	s.srv.Wait()
}

// SetShell sets default shell that will be executed for interactive
// sessions
func SetShell(shell string) ServerOption {
	return func(s *Server) error {
		s.shell = shell
		return nil
	}
}

// SetSessionServer represents realtime session registry server
func SetSessionServer(srv rsession.Service) ServerOption {
	return func(s *Server) error {
		s.sessionServer = srv
		return nil
	}
}

// SetProxyMode starts this server in SSH proxying mode
func SetProxyMode(tsrv reversetunnel.Server) ServerOption {
	return func(s *Server) error {
		s.proxyMode = (tsrv != nil)
		s.proxyTun = tsrv
		return nil
	}
}

// SetLabels sets dynamic and static labels that server will report to the
// auth servers
func SetLabels(labels map[string]string,
	cmdLabels services.CommandLabels) ServerOption {
	return func(s *Server) error {
		for name, label := range cmdLabels {
			if label.GetPeriod() < time.Second {
				label.SetPeriod(time.Second)
				cmdLabels[name] = label
				log.Warningf("label period can't be less that 1 second. Period for label '%v' was set to 1 second", name)
			}
		}

		s.labels = labels
		s.cmdLabels = cmdLabels
		return nil
	}
}

// SetLimiter sets rate and connection limiter for this server
func SetLimiter(limiter *limiter.Limiter) ServerOption {
	return func(s *Server) error {
		s.limiter = limiter
		return nil
	}
}

// SetAuditLog assigns an audit log interfaces to this server
func SetAuditLog(alog events.IAuditLog) ServerOption {
	return func(s *Server) error {
		s.alog = alog
		return nil
	}
}

func SetNamespace(namespace string) ServerOption {
	return func(s *Server) error {
		s.namespace = namespace
		return nil
	}
}

// SetPermitUserEnvironment allows you to set the value of permitUserEnvironment.
func SetPermitUserEnvironment(permitUserEnvironment bool) ServerOption {
	return func(s *Server) error {
		s.permitUserEnvironment = permitUserEnvironment
		return nil
	}
}

func SetCiphers(ciphers []string) ServerOption {
	return func(s *Server) error {
		s.ciphers = ciphers
		return nil
	}
}

func SetKEXAlgorithms(kexAlgorithms []string) ServerOption {
	return func(s *Server) error {
		s.kexAlgorithms = kexAlgorithms
		return nil
	}
}

func SetMACAlgorithms(macAlgorithms []string) ServerOption {
	return func(s *Server) error {
		s.macAlgorithms = macAlgorithms
		return nil
	}
}

// New returns an unstarted server
func New(addr utils.NetAddr,
	hostname string,
	signers []ssh.Signer,
	authService auth.AccessPoint,
	dataDir string,
	advertiseIP net.IP,
	proxyPublicAddr utils.NetAddr,
	options ...ServerOption) (*Server, error) {

	// read the host UUID:
	uuid, err := utils.ReadOrMakeHostUUID(dataDir)
	if err != nil {
		return nil, trace.Wrap(err)
	}

	s := &Server{
		addr:            addr,
		authService:     authService,
		hostname:        hostname,
		labelsMutex:     &sync.Mutex{},
		advertiseIP:     advertiseIP,
		proxyPublicAddr: proxyPublicAddr,
		uuid:            uuid,
		closer:          utils.NewCloseBroadcaster(),
		clock:           clockwork.NewRealClock(),
	}
	s.limiter, err = limiter.NewLimiter(limiter.LimiterConfig{})
	if err != nil {
		return nil, trace.Wrap(err)
	}
	s.certChecker = ssh.CertChecker{IsAuthority: s.isAuthority}

	for _, o := range options {
		if err := o(s); err != nil {
			return nil, trace.Wrap(err)
		}
	}

	var component string
	if s.proxyMode {
		component = teleport.ComponentProxy
	} else {
		component = teleport.ComponentNode
	}

	s.reg = newSessionRegistry(s)
	srv, err := sshutils.NewServer(
		component,
		addr, s, signers,
		sshutils.AuthMethods{PublicKey: s.keyAuth},
		sshutils.SetLimiter(s.limiter),
		sshutils.SetRequestHandler(s),
		sshutils.SetCiphers(s.ciphers),
		sshutils.SetKEXAlgorithms(s.kexAlgorithms),
		sshutils.SetMACAlgorithms(s.macAlgorithms))
	if err != nil {
		return nil, trace.Wrap(err)
	}
	s.srv = srv
	return s, nil
}

func (s *Server) getNamespace() string {
	return services.ProcessNamespace(s.namespace)
}

func (s *Server) logFields(fields map[string]interface{}) log.Fields {
	var component string
	if s.proxyMode {
		component = teleport.ComponentProxy
	} else {
		component = teleport.ComponentNode
	}
	return log.Fields{
		teleport.Component:       component,
		teleport.ComponentFields: fields,
	}
}

// Addr returns server address
func (s *Server) Addr() string {
	return s.srv.Addr()
}

// ID returns server ID
func (s *Server) ID() string {
	return s.uuid
}

// PermitUserEnvironment returns if ~/.tsh/environment will be read before a
// session is created by this server.
func (s *Server) PermitUserEnvironment() bool {
	return s.permitUserEnvironment
}

func (s *Server) setAdvertiseIP(ip net.IP) {
	s.Lock()
	defer s.Unlock()
	s.advertiseIP = ip
}

func (s *Server) getAdvertiseIP() net.IP {
	s.Lock()
	defer s.Unlock()
	return s.advertiseIP
}

// AdvertiseAddr returns an address this server should be publicly accessible
// as, in "ip:host" form
func (s *Server) AdvertiseAddr() string {
	// set if we have explicit --advertise-ip option
	if s.getAdvertiseIP() == nil {
		return s.addr.Addr
	}
	_, port, _ := net.SplitHostPort(s.addr.Addr)
	return net.JoinHostPort(s.getAdvertiseIP().String(), port)
}

func (s *Server) getInfo() services.Server {
	return &services.ServerV2{
		Kind:    services.KindNode,
		Version: services.V2,
		Metadata: services.Metadata{
			Name:      s.ID(),
			Namespace: s.getNamespace(),
			Labels:    s.labels,
		},
		Spec: services.ServerSpecV2{
			CmdLabels: services.LabelsToV2(s.getCommandLabels()),
			Addr:      s.AdvertiseAddr(),
			Hostname:  s.hostname,
		},
	}
}

// registerServer attempts to register server in the cluster
func (s *Server) registerServer() error {
	srv := s.getInfo()
	srv.SetTTL(s.clock, defaults.ServerHeartbeatTTL)
	if !s.proxyMode {
		return trace.Wrap(s.authService.UpsertNode(srv))
	}
	srv.SetPublicAddr(s.proxyPublicAddr.String())
	return trace.Wrap(s.authService.UpsertProxy(srv))
}

// heartbeatPresence periodically calls into the auth server to let everyone
// know we're up & alive
func (s *Server) heartbeatPresence() {
	sleepTime := defaults.ServerHeartbeatTTL/2 + utils.RandomDuration(defaults.ServerHeartbeatTTL/10)
	ticker := time.NewTicker(sleepTime)
	defer ticker.Stop()

	for {
		if err := s.registerServer(); err != nil {
			log.Warningf("failed to announce %v presence: %v", s.ID(), err)
		}
		select {
		case <-ticker.C:
			continue
		case <-s.closer.C:
			{
				log.Debugf("server.heartbeatPresence() exited")
				return
			}
		}
	}
}

func (s *Server) updateLabels() {
	for name, label := range s.cmdLabels {
		go s.periodicUpdateLabel(name, label.Clone())
	}
}

func (s *Server) syncUpdateLabels() {
	for name, label := range s.getCommandLabels() {
		s.updateLabel(name, label)
	}
}

func (s *Server) updateLabel(name string, label services.CommandLabel) {
	out, err := exec.Command(label.GetCommand()[0], label.GetCommand()[1:]...).Output()
	if err != nil {
		log.Errorf(err.Error())
		label.SetResult(err.Error() + " output: " + string(out))
	} else {
		label.SetResult(strings.TrimSpace(string(out)))
	}
	s.setCommandLabel(name, label)
}

func (s *Server) periodicUpdateLabel(name string, label services.CommandLabel) {
	for {
		s.updateLabel(name, label)
		time.Sleep(label.GetPeriod())
	}
}

func (s *Server) setCommandLabel(name string, value services.CommandLabel) {
	s.labelsMutex.Lock()
	defer s.labelsMutex.Unlock()
	s.cmdLabels[name] = value
}

func (s *Server) getCommandLabels() map[string]services.CommandLabel {
	s.labelsMutex.Lock()
	defer s.labelsMutex.Unlock()
	out := make(map[string]services.CommandLabel, len(s.cmdLabels))
	for key, val := range s.cmdLabels {
		out[key] = val.Clone()
	}
	return out
}

// extractRolesFromCert extracts roles from certificate metadata extensions
func (s *Server) extractRolesFromCert(cert *ssh.Certificate) ([]string, error) {
	data, ok := cert.Extensions[teleport.CertExtensionTeleportRoles]
	if !ok {
		// it's ok to not have any roles in the metadata
		return nil, nil
	}
	return services.UnmarshalCertRoles(data)
}

// checkPermissionToLogin checks the given certificate (supplied by a connected client)
// to see if this certificate can be allowed to login as user:login pair
func (s *Server) checkPermissionToLogin(cert *ssh.Certificate, teleportUser, osUser string) (string, error) {
	// enumerate all known CAs and see if any of them signed the
	// supplied certificate
	log.Debugf("[HA SSH NODE] checkPermsissionToLogin(%v, %v)", teleportUser, osUser)
	cas, err := s.authService.GetCertAuthorities(services.UserCA, false)
	if err != nil {
		return "", trace.Wrap(err)
	}
	var ca services.CertAuthority
	for i := range cas {
		checkers, err := cas[i].Checkers()
		if err != nil {
			return "", trace.Wrap(err)
		}
		for _, checker := range checkers {
			if sshutils.KeysEqual(cert.SignatureKey, checker) {
				ca = cas[i]
				break
			}
		}
	}
	// the certificate was signed by unknown authority
	if ca == nil {
		return "", trace.AccessDenied(
			"the certificate for user '%v' is signed by untrusted CA",
			teleportUser)
	}

	domainName, err := s.authService.GetDomainName()
	if err != nil {
		return "", trace.Wrap(err)
	}

	// for local users, go and check their individual permissions
	var roles services.RoleSet
	if domainName == ca.GetClusterName() {
		users, err := s.authService.GetUsers()
		if err != nil {
			return "", trace.Wrap(err)
		}
		for _, u := range users {
			if u.GetName() == teleportUser {
				// pass along the traits so we get the substituted roles for this user
				roles, err = services.FetchRoles(u.GetRoles(), s.authService, u.GetTraits())
				if err != nil {
					return "", trace.Wrap(err)
				}
			}
		}
	} else {
		certRoles, err := s.extractRolesFromCert(cert)
		if err != nil {
			log.Errorf("failed to extract roles from cert: %v", err)
			return "", trace.AccessDenied("failed to parse certificate roles")
		}
		roleNames, err := ca.CombinedMapping().Map(certRoles)
		if err != nil {
			log.Errorf("failed to map roles %v", err)
			return "", trace.AccessDenied("failed to map roles")
		}
		// pass the principals on the certificate along as the login traits
		// to the remote cluster.
		traits := map[string][]string{
			teleport.TraitLogins: cert.ValidPrincipals,
		}
		roles, err = services.FetchRoles(roleNames, s.authService, traits)
		if err != nil {
			return "", trace.Wrap(err)
		}
	}

	if err := roles.CheckAccessToServer(osUser, s.getInfo()); err != nil {
		return "", trace.AccessDenied("user %s@%s is not authorized to login as %v@%s: %v",
			teleportUser, ca.GetClusterName(), osUser, domainName, err)
	}

	return domainName, nil
}

// fetchRoleSet fretches role set for a given user
func (s *Server) fetchRoleSet(teleportUser string, clusterName string) (services.RoleSet, error) {
	localClusterName, err := s.authService.GetDomainName()
	if err != nil {
		return nil, trace.Wrap(err)
	}

	cas, err := s.authService.GetCertAuthorities(services.UserCA, false)
	if err != nil {
		return nil, trace.Wrap(err)
	}

	var ca services.CertAuthority
	for i := range cas {
		if cas[i].GetClusterName() == clusterName {
			ca = cas[i]
			break
		}
	}
	if ca == nil {
		return nil, trace.NotFound("could not find certificate authority for cluster %v and user %v", clusterName, teleportUser)
	}

	var roles services.RoleSet
	if localClusterName == clusterName {
		users, err := s.authService.GetUsers()
		if err != nil {
			return nil, trace.Wrap(err)
		}
		for _, u := range users {
			if u.GetName() == teleportUser {
				roles, err = services.FetchRoles(u.GetRoles(), s.authService, u.GetTraits())
				if err != nil {
					return nil, trace.Wrap(err)
				}
			}
		}
	} else {
		roles, err = services.FetchRoles(ca.GetRoles(), s.authService, nil)
		if err != nil {
			return nil, trace.Wrap(err)
		}
	}
	return roles, err
}

// isAuthority is called during checking the client key, to see if the signing
// key is the real CA authority key.
func (s *Server) isAuthority(cert ssh.PublicKey) bool {
	// find cert authority by it's key
	cas, err := s.authService.GetCertAuthorities(services.UserCA, false)
	if err != nil {
		log.Warningf("%v", trace.DebugReport(err))
		return false
	}

	for i := range cas {
		checkers, err := cas[i].Checkers()
		if err != nil {
			log.Warningf("%v", err)
			return false
		}
		for _, checker := range checkers {
			if sshutils.KeysEqual(cert, checker) {
				return true
			}
		}
	}
	return false
}

// EmitAuditEvent logs a given event to the audit log attached to the
// server who owns these sessions
func (s *Server) EmitAuditEvent(eventType string, fields events.EventFields) {
	log.Debugf("server.EmitAuditEvent(%v)", eventType)
	alog := s.alog
	if alog != nil {
		if err := alog.EmitAuditEvent(eventType, fields); err != nil {
			log.Error(err)
		}
	} else {
		log.Warn("SSH server has no audit log")
	}
}

// keyAuth implements SSH client authentication using public keys and is called
// by the server every time the client connects
func (s *Server) keyAuth(conn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
	cid := fmt.Sprintf("conn(%v->%v, user=%v)", conn.RemoteAddr(), conn.LocalAddr(), conn.User())
	fingerprint := fmt.Sprintf("%v %v", key.Type(), sshutils.Fingerprint(key))
	log.Debugf("[SSH] %v auth attempt with key %v", cid, fingerprint)

	logger := log.WithFields(log.Fields{
		"local":       conn.LocalAddr(),
		"remote":      conn.RemoteAddr(),
		"user":        conn.User(),
		"fingerprint": fingerprint,
	})

	cert, ok := key.(*ssh.Certificate)
	log.Debugf("[SSH] %v auth attempt with key %v, %#v", cid, fingerprint, cert)
	if !ok {
		log.Debugf("[SSH] auth attempt, unsupported key type for %v", fingerprint)
		return nil, trace.BadParameter("unsupported key type: %v", fingerprint)
	}
	if len(cert.ValidPrincipals) == 0 {
		log.Debugf("[SSH] need a valid principal for key %v", fingerprint)
		return nil, trace.BadParameter("need a valid principal for key %v", fingerprint)
	}
	if len(cert.KeyId) == 0 {
		log.Debugf("[SSH] need a valid key ID for key %v", fingerprint)
		return nil, trace.BadParameter("need a valid key for key %v", fingerprint)
	}
	teleportUser := cert.KeyId

	logAuditEvent := func(err error) {
		// only failed attempts are logged right now
		if err != nil {
			fields := events.EventFields{
				events.EventUser:          teleportUser,
				events.AuthAttemptSuccess: false,
				events.AuthAttemptErr:     err.Error(),
			}
			log.Warningf("[SSH] failed login attempt %#v", fields)
			s.EmitAuditEvent(events.AuthAttemptEvent, fields)
		}
	}
	permissions, err := s.certChecker.Authenticate(conn, key)
	if err != nil {
		logAuditEvent(err)
		return nil, trace.Wrap(err)
	}
	if err := s.certChecker.CheckCert(conn.User(), cert); err != nil {
		logAuditEvent(err)
		return nil, trace.Wrap(err)
	}
	logger.Debugf("[SSH] successfully authenticated")

	// see if the host user is valid (no need to do this in proxy mode)
	if !s.proxyMode {
		_, err = user.Lookup(conn.User())
		if err != nil {
			host, _ := os.Hostname()
			logger.Warningf("host '%s' does not have OS user '%s'", host, conn.User())
			logger.Errorf("no such user")
			return nil, trace.AccessDenied("no such user: '%s'", conn.User())
		}
	}

	// this is the only way I know of to pass valid principal with the
	// connection
	permissions.Extensions[utils.CertTeleportUser] = teleportUser

	if s.proxyMode {
		return permissions, nil
	}
	clusterName, err := s.checkPermissionToLogin(cert, teleportUser, conn.User())
	if err != nil {
		logger.Errorf("Permission denied: %v", err)
		logAuditEvent(err)
		return nil, trace.Wrap(err)
	}
	permissions.Extensions[utils.CertTeleportClusterName] = clusterName
	return permissions, nil
}

// HandleRequest is a callback for handling global out-of-band requests.
func (s *Server) HandleRequest(r *ssh.Request) {
	switch r.Type {
	case teleport.KeepAliveReqType:
		s.handleKeepAlive(r)
	default:
		log.Debugf("[SSH] Discarding %q global request: %+v", r.Type, r)
	}
}

// HandleNewChan is called when new channel is opened
func (s *Server) HandleNewChan(nc net.Conn, sconn *ssh.ServerConn, nch ssh.NewChannel) {
	channelType := nch.ChannelType()
	if s.proxyMode {
		if channelType == "session" { // interactive sessions
			ch, requests, err := nch.Accept()
			if err != nil {
				log.Infof("could not accept channel (%s)", err)
			}
			go s.handleSessionRequests(sconn, ch, requests)
		} else {
			nch.Reject(ssh.UnknownChannelType, fmt.Sprintf("unknown channel type: %v", channelType))
		}
		return
	}

	switch channelType {
	// a client requested the terminal size to be sent along with every
	// session message (Teleport-specific SSH channel for web-based terminals)
	case "x-teleport-request-resize-events":
		ch, _, _ := nch.Accept()
		go s.handleTerminalResize(sconn, ch)
	case "session": // interactive sessions
		ch, requests, err := nch.Accept()
		if err != nil {
			log.Infof("could not accept channel (%s)", err)
		}
		go s.handleSessionRequests(sconn, ch, requests)
	case "direct-tcpip": //port forwarding
		req, err := sshutils.ParseDirectTCPIPReq(nch.ExtraData())
		if err != nil {
			log.Errorf("failed to parse request data: %v, err: %v", string(nch.ExtraData()), err)
			nch.Reject(ssh.UnknownChannelType, "failed to parse direct-tcpip request")
		}
		ch, _, err := nch.Accept()
		if err != nil {
			log.Infof("could not accept channel (%s)", err)
		}
		go s.handleDirectTCPIPRequest(sconn, ch, req)
	default:
		nch.Reject(ssh.UnknownChannelType, fmt.Sprintf("unknown channel type: %v", channelType))
	}
}

// handleDirectTCPIPRequest does the port forwarding
func (s *Server) handleDirectTCPIPRequest(sconn *ssh.ServerConn, ch ssh.Channel, req *sshutils.DirectTCPIPReq) {
	// ctx holds the connection context and keeps track of the associated resources
	ctx := newCtx(s, sconn)
	ctx.isTestStub = s.isTestStub
	ctx.addCloser(ch)
	defer ctx.Debugf("direct-tcp closed")
	defer ctx.Close()

	addr := fmt.Sprintf("%v:%d", req.Host, req.Port)
	ctx.Infof("direct-tcpip channel: %#v to --> %v", req, addr)
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		ctx.Infof("failed connecting to: %v, err: %v", addr, err)
		return
	}
	defer conn.Close()
	// audit event:
	s.EmitAuditEvent(events.PortForwardEvent, events.EventFields{
		events.PortForwardAddr: addr,
		events.EventLogin:      ctx.login,
		events.LocalAddr:       sconn.LocalAddr().String(),
		events.RemoteAddr:      sconn.RemoteAddr().String(),
	})
	wg := &sync.WaitGroup{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		io.Copy(ch, conn)
		ch.Close()
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		io.Copy(conn, ch)
		conn.Close()
	}()
	wg.Wait()
}

// handleTerminalResize is called by the web proxy via its SSH connection.
// when a web browser connects to the web API, the web proxy asks us,
// by creating this new SSH channel, to start injecting the terminal size
// into every SSH write back to it.
//
// this is the only way to make web-based terminal UI not break apart
// when window changes its size
func (s *Server) handleTerminalResize(sconn *ssh.ServerConn, ch ssh.Channel) {
	// the party may not be immediately available for this connection,
	// keep asking for a full second:
	for i := 0; i < 10; i++ {
		party := s.reg.PartyForConnection(sconn)
		if party == nil {
			time.Sleep(time.Millisecond * 100)
			continue
		}
		// this starts a loop which will keep updating the terminal
		// size for every SSH write back to this connection
		party.termSizePusher(ch)
		return
	}
}

// handleSessionRequests handles out of band session requests once the session channel has been created
// this function's loop handles all the "exec", "subsystem" and "shell" requests.
func (s *Server) handleSessionRequests(sconn *ssh.ServerConn, ch ssh.Channel, in <-chan *ssh.Request) {
	// ctx holds the connection context and keeps track of the associated resources
	ctx := newCtx(s, sconn)
	ctx.isTestStub = s.isTestStub
	ctx.addCloser(ch)
	defer ctx.Close()

	// As SSH conversation progresses, at some point a session will be created and
	// its ID will be added to the environment
	updateContext := func() error {
		ssid, found := ctx.getEnv(sshutils.SessionEnvVar)
		if !found {
			return nil
		}
		// make sure whatever session is requested is a valid session
		_, err := rsession.ParseID(ssid)
		if err != nil {
			return trace.BadParameter("invalid session id")
		}
		findSession := func() (*session, bool) {
			s.reg.Lock()
			defer s.reg.Unlock()
			return s.reg.findSession(rsession.ID(ssid))
		}
		// update ctx with a session ID
		ctx.session, _ = findSession()
		if ctx.session == nil {
			log.Debugf("[SSH] will create new session for SSH connection %v", sconn.RemoteAddr())
		} else {
			log.Debugf("[SSH] will join session %v for SSH connection %v", ctx.session, sconn.RemoteAddr())
		}

		return nil
	}

	for {
		// update ctx with the session ID:
		if !s.proxyMode {
			err := updateContext()
			if err != nil {
				errorMessage := fmt.Sprintf("unable to update context: %v", err)
				ctx.Errorf("[SSH] %v", errorMessage)

				// write the error to channel and close it
				ch.Stderr().Write([]byte(errorMessage))
				_, err := ch.SendRequest("exit-status", false, ssh.Marshal(struct{ C uint32 }{C: teleport.RemoteCommandFailure}))
				if err != nil {
					ctx.Errorf("[SSH] failed to send exit status %v", errorMessage)
				}
				return
			}
		}
		select {
		case creq := <-ctx.subsystemResultC:
			// this means that subsystem has finished executing and
			// want us to close session and the channel
			ctx.Debugf("[SSH] close session request: %v", creq.err)
			return
		case req := <-in:
			if req == nil {
				// this will happen when the client closes/drops the connection
				ctx.Debugf("[SSH] client %v disconnected", sconn.RemoteAddr())
				return
			}
			if err := s.dispatch(ch, req, ctx); err != nil {
				replyError(ch, req, err)
				return
			}
			if req.WantReply {
				req.Reply(true, nil)
			}
		case result := <-ctx.result:
			ctx.Debugf("[SSH] ctx.result = %v", result)
			// this means that exec process has finished and delivered the execution result,
			// we send it back and close the session
			_, err := ch.SendRequest("exit-status", false, ssh.Marshal(struct{ C uint32 }{C: uint32(result.code)}))
			if err != nil {
				ctx.Infof("[SSH] %v failed to send exit status: %v", result.command, err)
			}
			return
		}
	}
}

// dispatch receives an SSH request for a subsystem and disptaches the request to the
// appropriate subsystem implementation
func (s *Server) dispatch(ch ssh.Channel, req *ssh.Request, ctx *ctx) error {
	ctx.Debugf("[SSH] ssh.dispatch(req=%v, wantReply=%v)", req.Type, req.WantReply)
	// if this SSH server is configured to only proxy, we do not support anything other
	// than our own custom "subsystems" and environment manipulation
	if s.proxyMode {
		switch req.Type {
		case "subsystem":
			return s.handleSubsystem(ch, req, ctx)
		case "env":
			// we currently ignore setting any environment variables via SSH for security purposes
			return s.handleEnv(ch, req, ctx)
		default:
			return trace.BadParameter(
				"proxy doesn't support request type '%v'", req.Type)
		}
	}

	switch req.Type {
	case "exec":
		// exec is a remote execution of a program, does not use PTY
		return s.handleExec(ch, req, ctx)
	case sshutils.PTYReq:
		// SSH client asked to allocate PTY
		return s.handlePTYReq(ch, req, ctx)
	case "shell":
		// SSH client asked to launch shell, we allocate PTY and start shell session
		ctx.exec = &execResponse{ctx: ctx}
		if err := s.reg.openSession(ch, req, ctx); err != nil {
			log.Error(err)
			return trace.Wrap(err)
		}
		return nil
	case "env":
		return s.handleEnv(ch, req, ctx)
	case "subsystem":
		// subsystems are SSH subsystems defined in http://tools.ietf.org/html/rfc4254 6.6
		// they are in essence SSH session extensions, allowing to implement new SSH commands
		return s.handleSubsystem(ch, req, ctx)
	case sshutils.WindowChangeReq:
		return s.handleWinChange(ch, req, ctx)
	case sshutils.AgentReq:
		// This happens when SSH client has agent forwarding enabled, in this case
		// client sends a special request, in return SSH server opens new channel
		// that uses SSH protocol for agent drafted here:
		// https://tools.ietf.org/html/draft-ietf-secsh-agent-02
		// the open ssh proto spec that we implement is here:
		// http://cvsweb.openbsd.org/cgi-bin/cvsweb/src/usr.bin/ssh/PROTOCOL.agent
		return s.handleAgentForward(ch, req, ctx)
	default:
		return trace.BadParameter(
			"proxy doesn't support request type '%v'", req.Type)
	}
}

func (s *Server) handleAgentForward(ch ssh.Channel, req *ssh.Request, ctx *ctx) error {
	roles, err := s.fetchRoleSet(ctx.teleportUser, ctx.clusterName)
	if err != nil {
		return trace.Wrap(err)
	}
	if err := roles.CheckAgentForward(ctx.login); err != nil {
		log.Warningf("[SSH:node] denied forward agent %v", err)
		return trace.Wrap(err)
	}
	systemUser, err := user.Lookup(ctx.login)
	if err != nil {
		return trace.ConvertSystemError(err)
	}
	uid, err := strconv.Atoi(systemUser.Uid)
	if err != nil {
		return trace.Wrap(err)
	}
	gid, err := strconv.Atoi(systemUser.Gid)
	if err != nil {
		return trace.Wrap(err)
	}

	authChan, _, err := ctx.conn.OpenChannel("auth-agent@openssh.com", nil)
	if err != nil {
		return trace.Wrap(err)
	}
	clientAgent := agent.NewClient(authChan)
	ctx.setAgent(clientAgent, authChan)

	pid := os.Getpid()
	socketDir, err := ioutil.TempDir(os.TempDir(), "teleport-")
	if err != nil {
		return trace.Wrap(err)
	}
	dirCloser := &utils.RemoveDirCloser{Path: socketDir}
	socketPath := filepath.Join(socketDir, fmt.Sprintf("teleport-%v.socket", pid))
	if err := os.Chown(socketDir, uid, gid); err != nil {
		if err := dirCloser.Close(); err != nil {
			log.Warn("failed to remove directory: %v", err)
		}
		return trace.ConvertSystemError(err)
	}

	agentServer := &teleagent.AgentServer{Agent: clientAgent}
	err = agentServer.ListenUnixSocket(socketPath, uid, gid, 0600)
	if err != nil {
		return trace.Wrap(err)
	}
	if req.WantReply {
		req.Reply(true, nil)
	}
	ctx.setEnv(teleport.SSHAuthSock, socketPath)
	ctx.setEnv(teleport.SSHAgentPID, fmt.Sprintf("%v", pid))
	ctx.addCloser(agentServer)
	ctx.addCloser(dirCloser)
	ctx.Debugf("[SSH:node] opened agent channel for teleport user %v and socket %v", ctx.teleportUser, socketPath)
	go agentServer.Serve()

	return nil
}

// handleWinChange gets called when 'window chnged' SSH request comes in
func (s *Server) handleWinChange(ch ssh.Channel, req *ssh.Request, ctx *ctx) error {
	params, err := parseWinChange(req)
	if err != nil {
		ctx.Error(err)
		return trace.Wrap(err)
	}
	term := ctx.getTerm()
	if term != nil {
		err = term.setWinsize(*params)
		if err != nil {
			ctx.Error(err)
		}
	}
	return trace.Wrap(s.reg.notifyWinChange(*params, ctx))
}

func (s *Server) handleSubsystem(ch ssh.Channel, req *ssh.Request, ctx *ctx) error {
	sb, err := parseSubsystemRequest(s, req)
	if err != nil {
		ctx.Warnf("[SSH] %v failed to parse subsystem request: %v", err)
		return trace.Wrap(err)
	}
	ctx.Debugf("[SSH] subsystem request: %v", sb)
	// starting subsystem is blocking to the client,
	// while collecting its result and waiting is not blocking
	if err := sb.start(ctx.conn, ch, req, ctx); err != nil {
		ctx.Warnf("[SSH] failed executing request: %v", err)
		ctx.sendSubsystemResult(trace.Wrap(err))
		return trace.Wrap(err)
	}
	go func() {
		err := sb.wait()
		log.Debugf("[SSH] %v finished with result: %v", sb, err)
		ctx.sendSubsystemResult(trace.Wrap(err))
	}()
	return nil
}

// handleEnv accepts environment variables sent by the client and stores them
// in connection context
func (s *Server) handleEnv(ch ssh.Channel, req *ssh.Request, ctx *ctx) error {
	var e sshutils.EnvReqParams
	if err := ssh.Unmarshal(req.Payload, &e); err != nil {
		ctx.Error(err)
		return trace.Wrap(err, "failed to parse env request")
	}
	ctx.setEnv(e.Name, e.Value)
	return nil
}

// handlePTYReq allocates PTY for this SSH connection per client's request
func (s *Server) handlePTYReq(ch ssh.Channel, req *ssh.Request, ctx *ctx) error {
	var (
		params *rsession.TerminalParams
		err    error
		term   *terminal
	)
	r, err := parsePTYReq(req)
	if err != nil {
		return trace.Wrap(err)
	}

	// if the caller asked for an invalid sized pty (like ansible
	// which asks for a 0x0 size) update the request with defaults
	err = r.CheckAndSetDefaults()
	if err != nil {
		return trace.Wrap(err)
	}

	params, err = rsession.NewTerminalParamsFromUint32(r.W, r.H)
	if err != nil {
		return trace.Wrap(err)
	}
	ctx.Debugf("[SSH] terminal requested of size %v", *params)

	// already have terminal?
	if term = ctx.getTerm(); term == nil {
		term, params, err = requestPTY(req)
		if err != nil {
			return trace.Wrap(err)
		}
		ctx.setTerm(term)
	}
	term.setWinsize(*params)

	// update the session:
	if err := s.reg.notifyWinChange(*params, ctx); err != nil {
		log.Error(err)
	}
	return nil
}

// handleExec is responsible for executing 'exec' SSH requests (i.e. executing
// a command after making an SSH connection)
//
// Note: this also handles 'scp' requests because 'scp' is a subset of "exec"
func (s *Server) handleExec(ch ssh.Channel, req *ssh.Request, ctx *ctx) error {
	execResponse, err := parseExecRequest(req, ctx)
	if err != nil {
		ctx.Infof("failed to parse exec request: %v", err)
		replyError(ch, req, err)
		return trace.Wrap(err)
	}
	if req.WantReply {
		req.Reply(true, nil)
	}
	// a terminal has been previously allocate for this command.
	// run this inside an interactive session
	if ctx.term != nil {
		return s.reg.openSession(ch, req, ctx)
	}
	// ... otherwise, regular execution:
	result, err := execResponse.start(ch)
	if err != nil {
		ctx.Error(err)
		replyError(ch, req, err)
	}
	if result != nil {
		ctx.Debugf("%v result collected: %v", execResponse, result)
		ctx.sendResult(*result)
	}
	if err != nil {
		return trace.Wrap(err)
	}

	// in case if result is nil and no error, this means that program is
	// running in the background
	go func() {
		result, err = execResponse.wait()
		if err != nil {
			ctx.Errorf("%v wait failed: %v", execResponse, err)
		}
		if result != nil {
			ctx.sendResult(*result)
		}
	}()
	return nil
}

// handleKeepAlive accepts and replies to keepalive@openssh.com requests.
func (s *Server) handleKeepAlive(req *ssh.Request) {
	log.Debugf("[KEEP ALIVE] Received %q: WantReply: %v", req.Type, req.WantReply)

	// only reply if the sender actually wants a response
	if req.WantReply {
		err := req.Reply(true, nil)
		if err != nil {
			log.Warnf("[KEEP ALIVE] Unable to reply to %q request: %v", req.Type, err)
			return
		}
	}

	log.Debugf("[KEEP ALIVE] Replied to %q", req.Type)
}

func replyError(ch ssh.Channel, req *ssh.Request, err error) {
	message := []byte(utils.UserMessageFromError(err))
	ch.Stderr().Write(message)
	if req.WantReply {
		req.Reply(false, message)
	}
}

func closeAll(closers ...io.Closer) error {
	var err error
	for _, cl := range closers {
		if cl == nil {
			continue
		}
		if e := cl.Close(); e != nil {
			err = e
		}
	}
	return err
}

type closerFunc func() error

func (f closerFunc) Close() error {
	return f()
}
