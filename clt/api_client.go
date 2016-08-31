package clt

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/gravitational/teleport/integration"
	"github.com/gravitational/teleport/lib/client"
	"github.com/gravitational/teleport/lib/session"
	"github.com/gravitational/teleport/lib/utils"

	"github.com/gravitational/teleconsole/lib"

	log "github.com/Sirupsen/logrus"
	"github.com/gravitational/trace"
)

type APIClient struct {
	SessionID     string
	Endpoint      string
	clientVersion string
}

func NewAPIClient(endpoint *url.URL, clientVersion string) APIClient {
	return APIClient{
		Endpoint:      endpoint.String(),
		clientVersion: clientVersion,
	}
}

func (this *APIClient) ServerURL() (*url.URL, error) {
	return url.Parse(this.Endpoint)
}

func (this *APIClient) friendlyProxyURL() string {
	u, err := this.ServerURL()
	if err != nil {
		return fmt.Sprintf("[%v]", err)
	}
	// remove :443 port if the theme is https
	host, port, err := net.SplitHostPort(u.Host)
	if err == nil {
		if port == "443" && u.Scheme == "https" {
			u.Host = host
		}
	}
	return u.String()
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
	resp, err := this.POST(this.Endpoint+"/api/sessions", "application/json", bytes.NewBuffer(sessionBytes))
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
	url := fmt.Sprintf("%s/api/session/%s", this.Endpoint, this.SessionID)
	resp, err := this.POST(url, "text/plain", strings.NewReader(sid.String()))
	// HTTP error:
	if resp.StatusCode != http.StatusOK {
		return trace.Wrap(makeHTTPError(resp))
	}
	return err
}

func (this *APIClient) GetSessionDetails(wsid string) (*lib.Session, error) {
	url := fmt.Sprintf("%s/api/sessions/%s", this.Endpoint, wsid)
	resp, err := this.GET(url)
	if err != nil {
		return nil, trace.Wrap(err)
	}
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
	url := fmt.Sprintf("%s/api/sessions/%s/stats", this.Endpoint, wsid)
	resp, err := this.GET(url)
	if err != nil {
		return nil, trace.Wrap(err)
	}
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
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	// set the version of the client:
	req.Header.Set(lib.ClientVersionHeader, this.clientVersion)
	return http.DefaultClient.Do(req)
}

func (this *APIClient) POST(url string, contentType string, reader io.Reader) (*http.Response, error) {
	req, err := http.NewRequest("POST", url, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", contentType)
	// set the version of the client:
	req.Header.Set(lib.ClientVersionHeader, this.clientVersion)
	return http.DefaultClient.Do(req)
}
