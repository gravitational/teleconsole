package forward

import (
	"net"
	"net/http"
	"strings"

	"github.com/mailgun/oxy/utils"
)

// Rewriter is responsible for removing hop-by-hop headers and setting forwarding headers
type HeaderRewriter struct {
	TrustForwardHeader bool
	Hostname           string
}

func (rw *HeaderRewriter) Rewrite(req *http.Request) {
	if clientIP, _, err := net.SplitHostPort(req.RemoteAddr); err == nil {
		if rw.TrustForwardHeader {
			if prior, ok := req.Header[XForwardedFor]; ok {
				clientIP = strings.Join(prior, ", ") + ", " + clientIP
			}
		}
		req.Header.Set(XForwardedFor, clientIP)
	}

	if xfp := req.Header.Get(XForwardedProto); xfp != "" && rw.TrustForwardHeader {
		req.Header.Set(XForwardedProto, xfp)
	} else if req.TLS != nil {
		req.Header.Set(XForwardedProto, "https")
	} else {
		req.Header.Set(XForwardedProto, "http")
	}

	if req.Host != "" {
		req.Header.Set(XForwardedHost, req.Host)
	}
	req.Header.Set(XForwardedServer, rw.Hostname)

	// Remove hop-by-hop headers to the backend.  Especially important is "Connection" because we want a persistent
	// connection, regardless of what the client sent to us.
	utils.RemoveHeaders(req.Header, HopHeaders...)
}
