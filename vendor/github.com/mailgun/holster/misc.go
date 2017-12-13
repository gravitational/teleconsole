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
	"fmt"
	"os"
	"reflect"

	"github.com/fatih/structs"
	"github.com/sirupsen/logrus"
)

// Given a struct or map[string]interface{} return as a logrus.Fields{} map
func ToFields(value interface{}) logrus.Fields {
	v := reflect.ValueOf(value)
	var hash map[string]interface{}
	var ok bool

	switch v.Kind() {
	case reflect.Struct:
		hash = structs.Map(value)
	case reflect.Map:
		hash, ok = value.(map[string]interface{})
		if !ok {
			panic("ToFields(): map kind must be of type map[string]interface{}")
		}
	default:
		panic("ToFields(): value must be of kind struct or map")
	}

	result := make(logrus.Fields, len(hash))
	for key, value := range hash {
		// Convert values the JSON marshaller doesn't know how to marshal
		v := reflect.ValueOf(value)
		switch v.Kind() {
		case reflect.Func:
			value = fmt.Sprintf("%+v", value)
		case reflect.Struct, reflect.Map:
			value = ToFields(value)
		}

		// Ensure the key is a string. convert it if not
		v = reflect.ValueOf(key)
		if v.Kind() != reflect.String {
			key = fmt.Sprintf("%+v", key)
		}
		result[key] = value
	}
	return result
}

// Get the environment variable or return the default value if unset
func GetEnv(envName, defaultValue string) string {
	value := os.Getenv(envName)
	if value == "" {
		return defaultValue
	}
	return value
}
