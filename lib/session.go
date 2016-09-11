package lib

import (
	"encoding/json"
	"net"
	"strconv"
	"time"

	"github.com/gravitational/teleport/integration"
	"github.com/gravitational/teleport/lib/client"
)

type Party struct {
	// FullName is not supported for now...
	FullName   string    `json"full_name"`
	RemoteAddr string    `json:"remote_addr"`
	LastActive time.Time `json:"last_active"`
}

// Session travels in JSON format between teleconsole client/server
type Session struct {
	// web session ID (not the same as teleport session ID)
	ID string `json:"id"`

	// teleport session ID
	TSID string `json:"teleport_session_id"`

	// Secretes and Login are needed to join this session
	Secrets integration.InstanceSecrets `json:"secrets"`
	Login   string                      `json:"login"`

	// ProxyHostPort is the host:port of the SSH proxy dynamically
	// created for this session
	ProxyHostPort string `json:"proxy_addr"`

	// NodeHostPort is the host:port of the client machine which
	// initiated the Teleconsole
	NodeHostPort string `json:"node_addr"`

	// Forwarded ports: these are set via -f flag on the client
	// when it creates a new session
	ForwardedPort *client.ForwardedPort `json:"forwarded_port"`
}

type SessionStats struct {
	// Parties lists all people who've joined this session
	Parties []Party `json:"connected_parties"`

	// Terminal size
	TermWidth  int `json:"term_width"`
	TermHeight int `json:"term_height"`
}

// ServerVersion is a JSON response returned by the server at
// the behinning of API conversation
type ServerVersion struct {
	// server's version & build data
	ServerVersion string `json:"server_ver"`

	// clients must show this warning message to users if it's not empty
	WarningMsg string `json:"warn_msg"`
}

func (s *Session) GetNodeHostPort() (host string, port int, err error) {
	h, p, err := net.SplitHostPort(s.NodeHostPort)
	if err != nil {
		return "", 0, err
	}
	port, err = strconv.Atoi(p)
	if err != nil {
		return "", 0, err
	}
	return h, port, nil
}

// ToJSON returns a nicely formatted JSON representation of the session
// (use it only for odebugging since there's no error handling)
func (s *Session) ToJSON() string {
	bytes, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		panic(err)
	}
	return string(bytes)
}
