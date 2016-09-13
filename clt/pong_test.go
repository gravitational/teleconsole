package clt

import "testing"

func TestPrefixes(t *testing.T) {
	constants := map[string]string{
		"us": DefaultEndpoint,
		"eu": "eu.teleconsole.com",
		"as": "as.teleconsole.com",
	}
	for ep, prefix := range Endpoints {
		if ep != constants[prefix] {
			t.Fatalf("You must never change session prefixes. '%s' does not map to '%s' anymore",
				ep, prefix)
		}
		// make sure prefixes have runes with codes greater than 'f'
		good := false
		for _, r := range prefix {
			if r > 'f' {
				good = true
				break
			}
		}
		if !good {
			t.Fatalf("Prefix '%s' is not good. Can be mistaken for a hex-encoded number", prefix)
		}
	}
}
