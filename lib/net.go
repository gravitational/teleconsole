package lib

import (
	"github.com/gravitational/teleport/lib/client"
	"github.com/gravitational/trace"
	"net/url"
	"strconv"

	"net"
	// log "github.com/Sirupsen/logrus"
)

const (
	ClientVersionHeader = "X-Client-Version"
)

// findFreePortRange returns a slice of n available IP ports
func GetFreePorts(n int) (ports []int, err error) {
	ports = make([]int, n)

	getFreePort := func() int {
		addr := net.TCPAddr{
			IP:   net.ParseIP("0.0.0.0"),
			Port: 0,
		}
		socket, err := net.ListenTCP("tcp", &addr)
		if err != nil {
			return 0
		}
		defer socket.Close()
		return socket.Addr().(*net.TCPAddr).Port
	}

	for n > 0 {
		port := getFreePort()
		if port == 0 {
			return ports, trace.Wrap(err)
		}
		ports[n-1] = port
		n -= 1
	}

	return ports, nil
}

// replaceHost takes a host:port string (with optional port), replaces
// host with 'newHost' and returns the result
func ReplaceHost(hostPort, newHost string) string {
	newHost, _, _ = net.SplitHostPort(newHost)
	_, port, err := net.SplitHostPort(hostPort)
	if err == nil && port != "" {
		return net.JoinHostPort(newHost, port)
	}
	return newHost
}

// ParseForwardAddr takes a host:port spec and returns a pre-configured "ForwardedPort"
// structure. It understands the following spec:
//
// "5000"         -> localhost:5000
// "host:port"    -> host:port
// "http://host"  -> host:80
//
func ParseForwardAddr(spec string) (p *client.ForwardedPort, err error) {
	var (
		port string
		u    *url.URL
	)
	// process "http://"
	u, err = url.Parse(spec)
	if err != nil {
		return nil, trace.Wrap(err)
	}
	p = &client.ForwardedPort{}
	if u.Host != "" && u.Scheme == "http" {
		p.DestPort = 80
		p.DestHost = u.Host
		return p, nil
	}
	if u.Host != "" && u.Scheme == "https" {
		p.DestPort = 443
		p.DestHost = u.Host
		return p, nil
	}
	// process port-only spec:
	p.DestPort, err = strconv.Atoi(spec)
	if err == nil {
		p.DestHost = "localhost"
		return p, nil
	}
	// process regular host:port spec:
	p.DestHost, port, err = net.SplitHostPort(spec)
	if err != nil {
		return nil, trace.Wrap(err)
	}
	p.DestPort, err = strconv.Atoi(port)
	if err != nil {
		return nil, trace.Wrap(err)
	}
	return p, nil
}
