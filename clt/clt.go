package clt

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/fatih/color"
	"github.com/gravitational/teleport/integration"
	"github.com/gravitational/teleport/lib/client"
	tservice "github.com/gravitational/teleport/lib/service"

	"github.com/gravitational/teleconsole/conf"
	"github.com/gravitational/teleconsole/lib"

	tsession "github.com/gravitational/teleport/lib/session"
	"github.com/gravitational/trace"
)

var (
	tunnelError     = fmt.Errorf("Unable to initialize the API for the local Teleport instance")
	DefaultSiteName = "teleconsole-client"

	// SyncRefreshIntervalMs defines the minimum amount of time it takes for
	// the local SSH server and the disposable proxy to synchronize the session
	// state (milliseconds)
	SyncRefreshInterval = time.Second
)

// StartBroadcast starts a new SSH session exposed to the world via disposable
// SSH proxy.
//
// This function:
//
// 1. Generates a new SSH keypair and creates a temporary SSH server which
//    trusts this pair.
// 2. Sends the credentials via HTTPS to a Teleconsole server, which will create
//    a single-use, single-tentant (just for us) SSH proxy
// 3. Receives an ID of the server-side proxy session. That ID can be shared
//    with other Teleconsole users so they could join this SSH session via proxy
// 4. Launches shell. When the shell exits, the SSH session is also terminated
//    disconnecting all parties.
func StartBroadcast(c *conf.Config, api *APIClient, cmd []string) error {
	if c.ForwardPorts != nil {
		return trace.Errorf("-L must be used with join")
	}
	// check API connectivity and compatibility
	if err := api.CheckVersion(); err != nil {
		return trace.Wrap(err)
	}
	u, err := user.Current()
	if err != nil {
		return trace.Wrap(err)
	}
	ports, err := lib.GetFreePorts(5)
	if err != nil {
		log.Error(err)
		return trace.Wrap(err)
	}
	hostName := "localhost"
	// create a new (local) teleport server instance and add ourselves as a user to it:
	fmt.Printf("Starting local SSH server on %s...\n", hostName)
	local := integration.NewInstance(DefaultSiteName, hostName, ports, nil, nil)
	local.AddUser(u.Username, []string{u.Username})

	// request Teleconsole server to create a remote teleport proxy we can
	// broadcast our connection through:
	fmt.Printf("Requesting a disposable SSH proxy for %s...\n", u.Username)
	proxySession, err := api.RequestNewSession(u.Username, c.ForwardPort, local)
	if err != nil {
		log.Error(err)
		return trace.Wrap(err)
	}
	// Assign the proper server to the generated secrets (they'll be used to configure
	// the reverse SSH tunnel to it)
	serverURL := api.Endpoint
	proxySession.Secrets.ListenAddr = lib.ReplaceHost(proxySession.Secrets.ListenAddr, serverURL.Host)

	// start the local teleport server instance initialized to trust the newly created
	// singnle-user proxy:
	tconf := tservice.MakeDefaultConfig()
	tconf.SSH.Enabled = true
	tconf.Console = nil
	tconf.Auth.NoAudit = true
	tconf.Proxy.DisableWebUI = true
	if err = local.CreateEx(proxySession.Secrets.AsSlice(), tconf); err != nil {
		log.Error(err)
		return trace.Wrap(err)
	}

	log.Debugf("client config: %v\n", local.Config.DebugDumpToYAML())
	if err = local.Start(); err != nil {
		log.Error(err)
		return trace.Wrap(err)
	}

	// this will close the proxied connection:
	defer onStopBroadcast(local)

	port, _ := strconv.Atoi(local.GetPortSSH())
	localClient, err := local.NewClient(u.Username, DefaultSiteName, hostName, port)
	if err != nil {
		log.Error(err)
		return trace.Wrap(err)
	}

	// Define "shell created" callback
	localClient.OnShellCreated = func(shell io.ReadWriteCloser) (exit bool, err error) {
		// publish the session (when it's ready) so the server-side disposable
		// proxy will locate this client by a session ID
		if err = publishSession(local, api); err != nil {
			log.Error(err)
			return true, err
		}
		// now lets see how many clients the server sees (should be at 1 - ourselves)
		fmt.Println("Checking status of the SSH tunnel...")
		var brokenSessionError = fmt.Errorf("SSH tunnel cannot be established, please try again.")
		const attempts = 10
		for i := 0; i < attempts; i++ {
			time.Sleep(SyncRefreshInterval)
			sessionStats, err := api.GetSessionStats(api.SessionID)
			if err != nil {
				log.Debug(err)
				return true, brokenSessionError
			}
			// found ourserlves!
			if len(sessionStats.Parties) > 0 {
				fmt.Printf("\n\rYour Teleconsole ID: \033[1m%s\033[0m\n\rWebUI for this session: %v/s/%s\n\rTo stop broadcasting, exit current shell by typing 'exit' or closing the window.\n\r",
					api.SessionID, api.friendlyProxyURL(), api.SessionID)
				return false, nil
			}
		}
		return true, brokenSessionError
	}

	// SSH into ourselves (we'll try a few times)
	err = localClient.SSH(cmd, false)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
	} else {
		fmt.Println("You have ended your session broadcast and the SSH tunnel is closed.")
	}
	return nil
}

// onStopBroadcast is called when the broadcasted session ends
func onStopBroadcast(local *integration.TeleInstance) {
	local.Stop(true)
	err := os.RemoveAll(local.Config.DataDir)
	if err != nil {
		log.Error("Failed deleting session log", err)
		return
	}
	log.Infof("Deleted session log at %s", local.Config.DataDir)
}

// publishSession must run as a goroutine: it waits for the local session
// inside 'local' Teleport instance to become available, and as soon as it
// does, it publishes it to the Telecast servers' disposable proxy
func publishSession(local *integration.TeleInstance, api *APIClient) error {
	// make sure the tunnel ("site API") is initialized:
	if local.Tunnel == nil {
		return trace.Wrap(tunnelError)
	}
	site, err := local.Tunnel.GetSite(local.Config.Auth.DomainName)
	if err != nil {
		log.Error(err)
		return trace.Wrap(err)
	}
	siteAPI, err := site.GetClient()
	if err != nil {
		log.Error(err)
		return trace.Wrap(err)
	}
	// poll for the session ID:
	for {
		time.Sleep(time.Millisecond * 100)
		sessions, err := siteAPI.GetSessions()
		if err != nil {
			continue
		}
		if len(sessions) == 0 {
			continue
		}
		err = api.PublishSessionID(sessions[0].ID)
		if err != nil {
			log.Error("failed to publish to Teleconsole server: ", err)
			local.Stop(true)
		}
		// success:
		break
	}
	return nil
}

func printPortInvite(login string, p *client.ForwardedPort) {
	friendlySrc := func() string {
		if p.DestPort == 80 {
			return fmt.Sprintf("http://localhost:%v", p.SrcPort)
		}
		if p.DestPort == 443 {
			return fmt.Sprintf("https://localhost:%v", p.SrcPort)
		}
		return fmt.Sprintf("localhost:%v", p.SrcPort)
	}
	friendlyDest := func() string {
		if p.DestHost == "localhost" || p.DestHost == "127.0.0.1" {
			return fmt.Sprintf("port %v on their machine", p.DestPort)
		}
		return fmt.Sprintf("%s:%v using their machine as proxy",
			p.DestHost, p.DestPort)
	}
	fmt.Printf("ATTENTION: %s has invited you to access %s via %s\n",
		login,
		friendlyDest(),
		friendlySrc())
}

// Joins someone's session given its ID
func Join(c *conf.Config, api *APIClient, sid string) error {
	if c.ForwardPort != nil {
		return trace.Errorf("-f cannot be used with join")
	}
	red := color.New(color.FgHiBlue).SprintFunc()
	fmt.Printf("%s joining session...\n\r", red("Teleconsole:"))

	// request credentials from the proxy:
	session, err := api.GetSessionDetails(sid)
	if err != nil {
		log.Error(err)
		return trace.Wrap(err)
	}
	/*
		for _, u := range session.Secrets.Users {
			// session did not come with pre-generated user credentials?
			// this must be a protected session and we're supposed to use
			// our own key:
			if len(u.Key.Priv) == 0 {
				key, err := readLocalKey()
				if err != nil {
					return trace.Wrap(err)
				}
				u.Key.Priv = key.Priv
				u.Key.Pub = key.Pub
			}
		}
	*/
	// session's proxy host is never configured properly (because the server
	// who returned it does not know which DNS name it's accessible by).
	// replace host, keep ports:
	session.ProxyHostPort = lib.ReplaceHost(session.ProxyHostPort, api.Endpoint.Host)

	// if this session offers a "port forwarding invite", always configure it
	// to be accessible via 127.0.0.1:9000 (to be made configurable later)
	if session.ForwardedPort != nil {
		session.ForwardedPort.SrcIP = "127.0.0.1"
		session.ForwardedPort.SrcPort = 9000
		c.ForwardPorts = []client.ForwardedPort{*session.ForwardedPort}
		printPortInvite(session.Login, session.ForwardedPort)
	}
	// these are target host's node/port (machine where the invite came from)
	nodeHost, nodePort, err := session.GetNodeHostPort()
	if err != nil {
		return trace.Wrap(err)
	}
	// create a new SSH client
	tc, err := client.NewClient(&client.Config{
		Username:           session.Login,
		ProxyHost:          session.ProxyHostPort,
		Host:               nodeHost,
		HostPort:           nodePort,
		HostLogin:          session.Login,
		InsecureSkipVerify: false,
		KeysDir:            "/tmp/",
		SiteName:           DefaultSiteName,
		LocalForwardPorts:  c.ForwardPorts,
	})
	if err != nil {
		return trace.Wrap(err)
	}
	// configure it to trust the proxy:
	cas := session.Secrets.GetCAs()
	for i := range cas {
		if err = tc.AddTrustedCA(&cas[i]); err != nil {
			log.Error(err)
			return trace.Wrap(err)
		}
	}
	// initialize it with the user credentials we've received
	// from the proxy session:
	for _, u := range session.Secrets.Users {
		if err = tc.AddKey(nodeHost, u.Key); err != nil {
			log.Error(err)
			return trace.Wrap(err)
		}
	}
	// try to join up to 5 times:
	for i := 0; i < 5; i++ {
		if err = tc.Join(tsession.ID(session.TSID), nil); err != nil {
			break
		}
		log.Warning(err)
		time.Sleep(time.Second)
	}
	return trace.Wrap(err)
}

// readLocalKey reads ~/.ssh/id_rsa.pub (or equivalent) and returns it
func readLocalKey() (*client.Key, error) {
	// find all .pub (AKA identity) files in $HOME/.ssh
	me, err := user.Current()
	if err != nil {
		return nil, trace.Wrap(err, "failed to determine the current user")
	}
	keys, err := filepath.Glob(filepath.Join(me.HomeDir, ".ssh", "*.pub"))
	if err != nil {
		return nil, trace.Wrap(err, "cannot determine where your SSH public key is")
	}
	if len(keys) == 0 {
		return nil, trace.Errorf("This session requires a public SSH key.\n" +
			"Use -i flag to specify an identity file")
	}
	// if multiple .pub files were found, use the 1st one:
	keyFile := keys[0]
	if len(keys) > 1 {
		log.Warnf("Multiple identity files found. Using %s", keyFile)
	}
	// reading the public key:
	key := new(client.Key)
	if key.Pub, err = ioutil.ReadFile(keyFile); err != nil {
		return nil, trace.Wrap(err)
	}
	// reading the private key:
	if key.Priv, err = ioutil.ReadFile(keyFile[:len(keyFile)-len(".pub")]); err != nil {
		return nil, trace.Wrap(err)
	}
	return key, nil
}
