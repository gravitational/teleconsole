/*
Copyright 2017 Mailgun Technologies Inc

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

     http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package holster

import (
	"crypto/rand"
	"fmt"
	"strings"
)

const NumericRunes = "0123456789"
const AlphaRunes = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// Return a random string made up of characters passed
func RandomRunes(prefix string, length int, runes ...string) string {
	chars := strings.Join(runes, "")
	var bytes = make([]byte, length)
	rand.Read(bytes)
	for i, b := range bytes {
		bytes[i] = chars[b%byte(len(chars))]
	}
	return prefix + string(bytes)
}

// Return a random string of alpha characters
func RandomAlpha(prefix string, length int) string {
	return RandomRunes(prefix, length, AlphaRunes)
}

// Return a random string of alpha and numeric characters
func RandomString(prefix string, length int) string {
	return RandomRunes(prefix, length, AlphaRunes, NumericRunes)
}

// Given a list of strings, return one of the strings randomly
func RandomItem(items ...string) string {
	var bytes = make([]byte, 1)
	rand.Read(bytes)
	return items[bytes[0]%byte(len(items))]
}

// Return a random domain name in the form "randomAlpha.net"
func RandomDomainName() string {
	return fmt.Sprintf("%s.%s",
		RandomAlpha("", 14),
		RandomItem("net", "com", "org", "io", "gov"))
}
