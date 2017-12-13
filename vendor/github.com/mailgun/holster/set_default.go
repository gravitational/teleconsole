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

import "reflect"

// If 'value' is empty or of zero value, assign the default value.
// This panics if the value is not a pointer or if value and
// default value are not of the same type.
//      var config struct {
//		Verbose *bool
//		Foo string
//		Bar int
//	}
// 	holster.SetDefault(&config.Foo, "default")
// 	holster.SetDefault(&config.Bar, 200)
func SetDefault(value, defaultValue interface{}) {
	v := reflect.ValueOf(value)
	if v.Kind() != reflect.Ptr {
		panic("holster.IfEmpty: Expected first argument to be of type reflect.Ptr")
	}
	v = reflect.Indirect(v)
	if IsZeroValue(v) {
		v.Set(reflect.ValueOf(defaultValue))
	}
}

// Returns true if 'value' is zero (the default golang value)
//	var thingy string
// 	holster.IsZero(thingy) == true
func IsZero(value interface{}) bool {
	return IsZeroValue(reflect.ValueOf(value))
}

// Returns true if 'value' is zero (the default golang value)
//	var count int64
// 	holster.IsZeroValue(reflect.ValueOf(count)) == true
func IsZeroValue(value reflect.Value) bool {
	switch value.Kind() {
	case reflect.Array, reflect.String:
		return value.Len() == 0
	case reflect.Bool:
		return !value.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return value.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return value.Uint() == 0
	case reflect.Float32, reflect.Float64:
		return value.Float() == 0
	case reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return value.IsNil()
	}
	return false
}
