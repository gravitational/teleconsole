package lib

import "os"

// fileExists() returns true if a file exists
func fileExists(filename string) bool {
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		return false
	}
	return true
}

// equalSlices checks if two string slices contain equal elements (order does not matter)
func equalSlices(s1 []string, s2 []string) bool {
	if len(s1) != len(s2) {
		return false
	}

	for _, s := range s1 {
		if !stringIn(s, s2) {
			return false
		}
	}
	return true
}

// stringIn() checks if str is present in the slice
func stringIn(str string, slice []string) bool {
	for _, s := range slice {
		if s == str {
			return true
		}
	}
	return false
}
