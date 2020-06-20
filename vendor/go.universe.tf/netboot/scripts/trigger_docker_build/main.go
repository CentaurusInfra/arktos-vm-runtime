// Copyright 2017 Google Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
)

func fatalf(msg string, args ...interface{}) {
	fmt.Printf(msg+"\n", args...)
	os.Exit(1)
}

func main() {
	url := os.Getenv("DOCKER_TRIGGER_URL")
	if url == "" {
		fatalf("no DOCKER_TRIGGER_URL found in environment")
	}

	resp, err := http.Post(url, "application/json", bytes.NewBufferString(`{"docker_tag": "latest"}`))
	if err != nil {
		fatalf("post to docker trigger: %s", err)
	}
	if resp.StatusCode != 200 {
		msg, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			fatalf("reading error message from docker trigger response: %s", err)
		}
		fatalf("non-200 status from docker trigger: %s (%q)", resp.Status, string(msg))
	}

	fmt.Printf("Triggered Docker build\n")
}
