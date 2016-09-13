package clt

import (
	"fmt"
	"net/http"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/kontsevoy/teleconsole/conf"
)

var (
	DefaultEndpoint = conf.DefaultServerHost

	// List of Teleconsole proxy servers.
	//
	// This map provides the mapping of DNS names to session prefixes.
	// This is just a usability to keep session IDs from being too long.
	//
	// NEVER change them! (they also must contain chars outside
	// of 'a'..'f' range
	Endpoints = map[string]string{
		DefaultEndpoint:      "us", // US
		"eu.teleconsole.com": "eu", // Europe
		"as.teleconsole.com": "as", // Asia
	}
)

// FindFastestEndpoint returns the Teleconsole server endpoint which was
// the fastest to respond to HTTP ping/pong
func FindFastestEndpoint() string {
	responded := make(chan string)
	start := time.Now()

	playPong := func(endpoint string) {
		log.Infof("GET http://%s/ping", endpoint)
		resp, err := http.Get(fmt.Sprintf("http://%s/ping", endpoint))
		if err == nil {
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				responded <- endpoint
				log.Infof("%s responded in %v", endpoint, time.Now().Sub(start))
			}
		}
	}
	for ep, _ := range Endpoints {
		go playPong(ep)
	}
	timeout := time.NewTimer(time.Second * 5)
	defer timeout.Stop()

	select {
	case endpoint := <-responded:
		return endpoint
	case <-timeout.C:
		log.Error("Timeout: none of the severs have played pong.")
	}
	return DefaultEndpoint
}

func SesionPrefixFor(endpoint string) string {
	return Endpoints[endpoint]
}
