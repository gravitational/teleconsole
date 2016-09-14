package geo

import (
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/gravitational/teleconsole/conf"
)

type Endpoint struct {
	Hostname      string `json:"dns_name"`
	SessionPrefix string `json:"session_prefix"`
}

var (
	// US West is the default:
	DefaultEndpoint = Endpoint{Hostname: conf.DefaultServerHost, SessionPrefix: ""}

	// List of Teleconsole proxy servers.
	Endpoints = []Endpoint{
		DefaultEndpoint,
		{"eu.teleconsole.com", "eu"},
		{"as.teleconsole.com", "as"},
	}
)

// FindFastestEndpoint returns the Teleconsole server endpoint which was
// the fastest to respond to HTTP ping/pong
func FindFastestEndpoint() Endpoint {
	responded := make(chan Endpoint)
	start := time.Now()

	// performs HTTP GET against a given endpoint
	ping := func(ep Endpoint) {
		url := fmt.Sprintf("http://%s/ping", ep.Hostname)
		log.Infof("Ping %s", url)
		resp, err := http.Get(url)
		if err != nil {
			log.Error(err)
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			responded <- ep
		}
	}
	for _, ep := range Endpoints {
		go ping(ep)
	}
	timeout := time.NewTimer(time.Second * 5)
	defer timeout.Stop()

	select {
	case e := <-responded:
		log.Infof("%s responded in %v", e.Hostname, time.Now().Sub(start))
		return e
	case <-timeout.C:
		log.Error("Timeout: none of the severs have played pong.")
	}
	return DefaultEndpoint
}

// SessionPrefixFor finds a session prefix for a given endpoint
func SesionPrefixFor(endpoint string) string {
	host, _, _ := net.SplitHostPort(endpoint)
	if host != "" {
		endpoint = host
	}
	for _, ep := range Endpoints {
		if endpoint == ep.Hostname {
			return ep.SessionPrefix
		}
	}
	return ""
}

// EndpointForSession deterines which Teleconsole server generated a given session ID
// It looks at the prefix (first few bytes) of it.
//
// Returns the endpoint (or "" for legacy sessions from teleconsole.com) and also
// returns the session ID without the prefix
func EndpointForSession(sid string) (string, string) {
	for _, ep := range Endpoints {
		if len(ep.SessionPrefix) > 0 {
			if strings.HasPrefix(sid, ep.SessionPrefix) {
				return ep.Hostname, sid[len(ep.SessionPrefix):]
			}
		}
	}
	return DefaultEndpoint.Hostname, sid
}

// IsGeobalancedSession returns 'true' if the given session ID starts with a geo prefix
func IsGeobalancedSession(sid string) bool {
	_, rs := EndpointForSession(sid)
	return rs != sid
}
