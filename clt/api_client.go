package clt

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"

	"github.com/gravitational/teleport/integration"
	"github.com/gravitational/teleport/lib/client"
	"github.com/gravitational/teleport/lib/session"
	"github.com/gravitational/teleport/lib/utils"

	"github.com/gravitational/teleconsole/conf"
	"github.com/gravitational/teleconsole/lib"

	log "github.com/Sirupsen/logrus"
	"github.com/gravitational/trace"
)

// APIClient is an HTTP client for talking to telecast server asking for
// new Teleport proxy instances
type APIClient struct {
	SessionID     string
	Endpoint      *url.URL
	clientVersion string
	httpClient    http.Client
}

// NewAPIClient creates and returns the new API client
func NewAPIClient(config *conf.Config, clientVersion string) *APIClient {
	client := &APIClient{
		Endpoint:      config.APIEndpointURL,
		clientVersion: clientVersion,
	}
	// create cookie storage:
	client.httpClient.Jar, _ = cookiejar.New(nil)

	// disable automatic redirects
	client.httpClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}

	if config.InsecureHTTPS {
		fmt.Println("\033[1mWARNING:\033[0m running in insecure mode!")
		client.httpClient.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
	}
	return client
}

// Sends the version of the client to the server and receives a session
// cookie. Every new API conversation must start here
func (this *APIClient) CheckVersion() error {
	var (
		resp *http.Response
		err  error
	)
	const maxRedirects = 2

	for i := 0; i <= maxRedirects; i++ {
		log.Infof("Getting version from %s", this.Endpoint)
		// Request server's version (and report ours):
		resp, err = this.GET("/api/version")
		if err != nil {
			log.Error(err)
			return trace.Wrap(err)
		}
		// Redirect to another less busy server?
		if resp.StatusCode == http.StatusTemporaryRedirect {
			ep := resp.Header.Get("Location")
			if ep == "" {
				return trace.Errorf("Invalid redirect from the server")
			}
			if this.Endpoint, err = url.Parse(ep); err != nil {
				return trace.Errorf("Invalid redirect from the server to '%s'", ep)
			}
			continue
		}
		break
	}
	// HTTP error?
	if resp.StatusCode != http.StatusOK {
		return trace.Wrap(makeHTTPError(resp))
	}
	// parse response (updated session object) and return it:
	var sv lib.ServerVersion
	decoder := json.NewDecoder(resp.Body)
	if err = decoder.Decode(&sv); err != nil {
		log.Error(err)
		return trace.Errorf("Server returned malformed response")
	}
	// display server-supplied warning message:
	if sv.WarningMsg != "" {
		fmt.Println("\033[1mWARNING:\033[0m", sv.WarningMsg)
	}
	log.Infof("Connecting to https://%s", this.Endpoint.Host)
	return nil
}

// RequestNewSession makes an HTTP call to a Telecast server, passing the SSH secrets
// of the local session.
//
// The server will create a disposable SSH proxy pre-configured to trust this instance
func (this *APIClient) RequestNewSession(
	login string,
	fport *client.ForwardedPort,
	localTeleport *integration.TeleInstance) (*lib.Session, error) {

	log.Infof("Requesting a new session for %v forwarding %v", login, fport)

	// generate a random session ID:
	var err error
	this.SessionID, err = utils.CryptoRandomHex(20)
	if err != nil {
		log.Error(err)
		return nil, trace.Wrap(err)
	}

	// create a session here on the client, pack our trusted secrets to it and send it
	// to the server via HTTPS:
	session := &lib.Session{
		ID:            this.SessionID,
		Secrets:       localTeleport.Secrets,
		Login:         login,
		NodeHostPort:  net.JoinHostPort(localTeleport.Hostname, localTeleport.GetPortSSH()),
		ForwardedPort: fport,
	}

	// POST http://server/sessions
	sessionBytes, err := json.Marshal(session)
	if err != nil {
		log.Error(err)
		return nil, trace.Wrap(err)
	}
	resp, err := this.POST("/api/sessions", "application/json", bytes.NewBuffer(sessionBytes))
	if err != nil {
		log.Error(err)
		return nil, trace.Wrap(err)
	}
	// HTTP error:
	if resp.StatusCode != http.StatusOK {
		return nil, trace.Wrap(makeHTTPError(resp))
	}

	// parse response (updated session object) and return it:
	decoder := json.NewDecoder(resp.Body)
	if err = decoder.Decode(session); err != nil {
		return nil, trace.Wrap(err)
	}
	return session, nil
}

func (this *APIClient) PublishSessionID(sid session.ID) error {
	resp, err := this.POST("/api/session/"+this.SessionID,
		"text/plain", strings.NewReader(sid.String()))
	defer resp.Body.Close()
	// HTTP error:
	if resp.StatusCode != http.StatusOK {
		return trace.Wrap(makeHTTPError(resp))
	}
	return err
}

// GetSessionDetails requests the session details (keys) for a given session
// from the proxy. The proxy always sends its host public key, and if the
// session is anonymous, it also sends single-use user keys.
//
// If a session requres a key, a client won't receive them here, he will have
// to use his own from ~/.ssh
func (this *APIClient) GetSessionDetails(wsid string) (*lib.Session, error) {
	resp, err := this.GET("/api/sessions/" + wsid)
	if err != nil {
		return nil, trace.Wrap(err)
	}
	defer resp.Body.Close()
	// HTTP error:
	if resp.StatusCode != http.StatusOK {
		return nil, trace.Wrap(makeHTTPError(resp))
	}
	var s lib.Session
	decoder := json.NewDecoder(resp.Body)
	if err = decoder.Decode(&s); err != nil {
		return nil, trace.Wrap(err)
	}
	return &s, nil
}

func (this *APIClient) GetSessionStats(wsid string) (*lib.SessionStats, error) {
	url := fmt.Sprintf("/api/sessions/%s/stats", wsid)
	resp, err := this.GET(url)
	if err != nil {
		return nil, trace.Wrap(err)
	}
	defer resp.Body.Close()
	// HTTP error:
	if resp.StatusCode != http.StatusOK {
		return nil, trace.Wrap(makeHTTPError(resp))
	}
	var s lib.SessionStats
	decoder := json.NewDecoder(resp.Body)
	if err = decoder.Decode(&s); err != nil {
		return nil, trace.Wrap(err)
	}
	return &s, nil
}

func (this *APIClient) GET(url string) (*http.Response, error) {
	req, err := http.NewRequest("GET", this.Endpoint.String()+url, nil)
	if err != nil {
		return nil, err
	}
	// set the version of the client:
	req.Header.Set(lib.ClientVersionHeader, this.clientVersion)
	return this.httpClient.Do(req)
}

func (this *APIClient) POST(url string, contentType string, reader io.Reader) (*http.Response, error) {
	req, err := http.NewRequest("POST", this.Endpoint.String()+url, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", contentType)
	// set the version of the client:
	req.Header.Set(lib.ClientVersionHeader, this.clientVersion)
	return this.httpClient.Do(req)
}

// friendlyProxyURL returns the URL of the Teleport proxy, it's the one
// we print to stdout upon creation of a new session
func (this *APIClient) friendlyProxyURL() string {
	// remove :443 port if the theme is https
	host, port, err := net.SplitHostPort(this.Endpoint.Host)
	if err == nil {
		if port == "443" && this.Endpoint.Scheme == "https" {
			this.Endpoint.Host = host
		}
	}
	return this.Endpoint.String()
}
