package main

import (
	"crypto/x509"
	"fmt"
	"net/url"
	"os"

	"github.com/gravitational/teleconsole/clt"
	"github.com/gravitational/teleconsole/version"
	"github.com/gravitational/trace"
	"github.com/sirupsen/logrus"
)

func main() {
	app, err := clt.NewApp(nil)
	fatalIf(err)

	conf := app.GetConfig()

	if conf.Verbosity == 0 {
		logrus.SetLevel(logrus.PanicLevel)
	} else {
		logrus.SetFormatter(&trace.TextFormatter{})
		app.DebugDump()
	}

	if len(app.Args) == 0 {
		// start new broadcast if no arguments were specified
		fatalIf(app.Start())
		return
	}

	switch app.Args[0] {
	case "help":
		app.Usage()
	case "join":
		fatalIf(app.Join())
	case "version":
		version.Print("Teleconsole", conf.Verbosity > 0)
		os.Exit(0)
	default:
		app.Usage()
	}
}

func fatalIf(err error) {
	if err != nil {
		// see if it's untrusted HTTPS certificate error?
		if badCert, url := IsUntrustedCertError(err); badCert {
			fmt.Fprintf(os.Stderr, "\033[1mWARNING:\033[0m The SSL certificate for %s cannot be trusted!\n", url)
			fmt.Fprintf(os.Stderr, "Either you are being attacked, or try -insecure flag if you know what you're doing\n")
			// HTTP reponse error:
		} else {
			fmt.Fprintf(os.Stderr, "%s\n", err)
		}
		logrus.Debug(trace.DebugReport(err))
		os.Exit(1)
	}
}

// returns true if the error indicates
// "x509: certificate signed by unknown authority" error when talking to HTTPS server
//
// Also returns URL which caused the error
func IsUntrustedCertError(err error) (bool, string) {
	switch t := trace.Unwrap(err).(interface{}).(type) {
	case *url.Error:
		_, ok := t.Err.(x509.UnknownAuthorityError)
		return ok, t.URL
	}
	return false, ""
}
