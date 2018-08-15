package geo

import (
	"os"
	"testing"
)

var prefixes = map[string]string{
	"":   DefaultEndpoint.Hostname,
	"eu": "eu.teleconsole.com",
	"as": "as.teleconsole.com",
}

func TestPrefixes(t *testing.T) {
	for _, ep := range Endpoints {
		if ep.Hostname != prefixes[ep.SessionPrefix] {
			t.Fatalf("'%s' does not map to '%s' anymore", ep.Hostname, ep.SessionPrefix)
		}
	}
}

func TestEPSearch(t *testing.T) {
	ep, sid := EndpointForSession("5555")
	if ep != DefaultEndpoint.Hostname || sid != "5555" {
		t.Errorf("bad ep='%s', bad sid='%s'", ep, sid)
	}

	ep, sid = EndpointForSession("eu555")
	if ep != prefixes["eu"] || sid != "555" {
		t.Fatalf("Got '%s' for '%s'", ep, sid)
	}
}

func TestPrefixSearch(t *testing.T) {
	if SesionPrefixFor("example.com") != "" {
		t.Error("example.com must have no prefix")
	}
	p := SesionPrefixFor("teleconsole.com:433")
	if p != "" {
		t.Errorf("default teleconsole prefix is inavlid: '%s'", p)
	}
}

func TestGeoCheck(t *testing.T) {
	if !IsGeobalancedSession("eu1111") {
		t.Errorf("failed to detect eu endpoint")
	}
	if IsGeobalancedSession("1111") {
		t.Errorf("failed to detect non-geo session")
	}
}
