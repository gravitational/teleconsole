package version

import (
	"fmt"
)

// Print prints the version and build date into the CLI
func Print(prefix string, verbose bool) {
	fmt.Printf("%s %v\n", prefix, Version)
	if verbose {
		if BuildDate != "" {
			fmt.Printf("Built on %v. Git: %v\n", BuildDate, Gitref)
		} else {
			fmt.Printf("Installed via go\n")
		}
	}
}
