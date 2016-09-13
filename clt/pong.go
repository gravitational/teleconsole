package clt

import (
	"fmt"
	"net/http"
	"time"

	log "github.com/Sirupsen/logrus"
)

var (
	DefaultEndpoint = "teleconsole.com"

	// list of Teleconsole proxy servers.
	Endpoints = []string{
		DefaultEndpoint,      // US
		"eu.teleconsole.com", // Europe
		"as.teleconsole.com", // Asia
	}
)

// FindFastestEndpoint returns the Teleconsole server endpoint which was
// the fastest to respond to HTTP ping/pong
func FindFastestEndpoint() string {
	responded := make(chan string)
	start := time.Now()

	playPong := func(endpoint string) {
		resp, err := http.Get(fmt.Sprintf("http://%s/ping", endpoint))
		if err == nil {
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				responded <- endpoint
				log.Infof("%s responded in %v", endpoint, time.Now().Sub(start))
			}
		}
	}
	for _, ep := range Endpoints {
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
