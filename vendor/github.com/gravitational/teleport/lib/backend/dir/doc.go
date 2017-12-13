/*
Copyright 2016 Gravitational, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

fs package implements backend.Backend interface using a regular
filesystem-based directory. The filesystem needs to be POSIX
compliant and support 'date modified' attribute on files.

*/

// Package 'dir' implements the "directory backend". It uses a regular
// filesystem directories/files to store Teleport auth server state.
//
// Limitations:
// 	- key names cannot start with '.' (dot)
package dir
