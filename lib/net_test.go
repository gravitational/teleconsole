package lib

import (
	"testing"
)

func TestNet(t *testing.T) {
	_, err := ParseForwardAddr("foo")
	if err == nil {
		t.Error("expected to fail")
	}

	p, err := ParseForwardAddr("localhost:5000")
	if err != nil {
		t.Error(err)
	}
	if p.DestHost != "localhost" {
		t.Error("host failure")
	}
	if p.DestPort != 5000 {
		t.Error("port failure")
	}

	p, err = ParseForwardAddr("http://example.com")
	if err != nil {
		t.Error(err)
	}
	if p.DestHost != "example.com" {
		t.Error("URL host failure")
	}
	if p.DestPort != 80 {
		t.Error("URL port failure")
	}

	p, err = ParseForwardAddr("8888")
	if err != nil {
		t.Error(err)
	}
	if p.DestHost != "localhost" {
		t.Error("hostless host failure")
	}
	if p.DestPort != 8888 {
		t.Error("hostless port failure")
	}

}
