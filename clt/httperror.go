package clt

import (
	"encoding/json"
	"fmt"

	"io/ioutil"
	"net/http"

	log "github.com/sirupsen/logrus"
)

type HTTPClientError struct {
	error
	StatusCode int
	Status     string
	Message    string
	body       []byte
}

// makeHTTPError converts HTTP response to trace.Error
func makeHTTPError(r *http.Response) error {
	if r.StatusCode == http.StatusOK {
		return nil
	}
	var message string
	body, e := ioutil.ReadAll(r.Body)
	if e != nil {
		log.Errorf("failed reading error response: %v", e)
		message = e.Error()
	} else {
		message = string(body)
		var m map[string]string
		if e = json.Unmarshal(body, &m); e != nil {
		} else {
			// JSON response:
			msg, ok := m["message"]
			if ok {
				message = msg
			}
		}
	}
	return &HTTPClientError{
		StatusCode: r.StatusCode,
		Status:     r.Status,
		body:       body,
		Message:    message,
	}
}

func (this *HTTPClientError) Error() string {
	return fmt.Sprintf("%s: %s", this.Status, this.Message)
}
