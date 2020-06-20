// Copyright 2016 Google Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package pixiecore

import (
	"crypto/rand"
	"io"
	"testing"
)

func TestSignURL(t *testing.T) {
	var k [32]byte
	if _, err := io.ReadFull(rand.Reader, k[:]); err != nil {
		t.Fatalf("could not read randomness for signing nonce: %s", err)
	}

	u := "http://test.example/foo/bar"

	id, err := signURL(u, &k)
	if err != nil {
		t.Fatalf("URL signing failed: %s", err)
	}

	u2, err := getURL(id, &k)
	if err != nil {
		t.Fatalf("URL decoding failed: %s", err)
	}

	if u != u2 {
		t.Fatalf("getURL(signURL(%q)) = %q, which isn't the same thing", u, u2)
	}

	// Corrupt the signed thing
	id += "d"
	_, err = getURL(id, &k)
	if err == nil {
		t.Fatalf("Corrupted id %q decoded correctly", id)
	}
}
