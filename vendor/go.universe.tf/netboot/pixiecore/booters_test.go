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
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func mustMAC(s string) net.HardwareAddr {
	m, err := net.ParseMAC(s)
	if err != nil {
		panic(err)
	}
	return m
}

func mustWrite(dir, path, contents string) {
	if err := ioutil.WriteFile(filepath.Join(dir, path), []byte(contents), 0644); err != nil {
		panic(err)
	}
}

func mustRead(f io.ReadCloser, sz int64, err error) string {
	if err != nil {
		panic(err)
	}
	defer f.Close()
	bs, err := ioutil.ReadAll(f)
	if err != nil {
		panic(err)
	}
	if sz >= 0 && int64(len(bs)) != sz {
		panic(fmt.Errorf("sz = %d, but ReadCloser has %d bytes", sz, len(bs)))
	}
	return string(bs)
}

func TestStaticBooter(t *testing.T) {
	dir, err := ioutil.TempDir("", "pixiecore-static-booter-test")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(dir)
	mustWrite(dir, "foo", "foo file")
	mustWrite(dir, "bar", "bar file")
	mustWrite(dir, "baz", "baz file")
	mustWrite(dir, "quux", "quux file")

	s := &Spec{
		Kernel: ID(filepath.Join(dir, "foo")),
		Initrd: []ID{
			ID(filepath.Join(dir, "bar")),
			ID(filepath.Join(dir, "baz")),
		},
		Cmdline: fmt.Sprintf(`test={{ ID "%s" }} thing=other`, filepath.Join(dir, "quux")),
		Message: "Hello from testing world!",
	}

	b, err := StaticBooter(s)
	if err != nil {
		t.Fatalf("Constructing StaticBooter: %s", err)
	}

	m := Machine{
		MAC:  mustMAC("01:02:03:04:05:06"),
		Arch: ArchIA32,
	}

	spec, err := b.BootSpec(m)
	if err != nil {
		t.Fatalf("Getting bootspec: %s", err)
	}

	expected := &Spec{
		Kernel:  ID("kernel"),
		Initrd:  []ID{"initrd-0", "initrd-1"},
		Cmdline: `test={{ ID "other-0" }} thing=other`,
		Message: "Hello from testing world!",
	}

	if !reflect.DeepEqual(spec, expected) {
		t.Fatalf("Expected equal specs, but they differed:\nwant: %#v\ngot:  %#v", expected, spec)
	}

	// Different machine gets the same spec
	m.MAC = mustMAC("02:03:04:05:06:07")
	m.Arch = ArchX64

	spec, err = b.BootSpec(m)
	if err != nil {
		t.Fatalf("Getting bootspec: %s", err)
	}
	if !reflect.DeepEqual(spec, expected) {
		t.Fatalf("Expected equal specs, but they differed:\nwant: %#v\ngot:  %#v", expected, spec)
	}

	// Check that the referenced files exist
	fs := map[ID]string{
		"kernel":   "foo file",
		"initrd-0": "bar file",
		"initrd-1": "baz file",
		"other-0":  "quux file",
	}
	for id, contents := range fs {
		v := mustRead(b.ReadBootFile(id))
		if v != contents {
			t.Fatalf("Wrong file contents for %q: wanted %q, got %q", id, contents, v)
		}
	}
}

func TestAPIBooter(t *testing.T) {
	// Set up an HTTP server to act as a (terrible) API server
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Couldn't get a listener for HTTP: %s", err)
	}

	http.HandleFunc("/v1/boot/01:02:03:04:05:06", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{
  "kernel": "/foo",
  "initrd": ["/bar", "/baz"],
  "cmdline": "test={{ URL \"/quux\" }} other=thing",
  "message": "Hello from test world!"
}`))
	})
	http.HandleFunc("/foo", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`foo file`)) })
	http.HandleFunc("/bar", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`bar file`)) })
	http.HandleFunc("/baz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`baz file`)) })
	http.HandleFunc("/quux", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`quux file`)) })
	go http.Serve(l, nil)

	// Finally, build an APIBooter and test it.
	b, err := APIBooter(fmt.Sprintf("http://%s/", l.Addr()), 100*time.Millisecond)
	if err != nil {
		t.Fatalf("Constructing APIBooter: %s", err)
	}

	m := Machine{
		MAC:  mustMAC("01:02:03:04:05:06"),
		Arch: ArchIA32,
	}

	spec, err := b.BootSpec(m)
	if err != nil {
		t.Fatalf("Getting bootspec: %s", err)
	}

	// Unlike StaticBooter, the IDs are variable here because the
	// server address isn't deterministic (also we don't make
	// rand.Reader deterministic). Let's do as much checking as we
	// can, and then just fetch the IDs we got back to check the rest.
	if spec.Message != "Hello from test world!" {
		t.Fatalf("Wrong message %q", spec.Message)
	}
	if len(spec.Initrd) != 2 {
		t.Fatalf("Wrong number of initrds: %d", len(spec.Initrd))
	}
	if !strings.HasPrefix(spec.Cmdline, `test={{ ID "`) || !strings.HasSuffix(spec.Cmdline, `" }} other=thing`) {
		t.Fatalf("Wrong cmdline %q", spec.Cmdline)
	}

	quuxID := ID(spec.Cmdline[12 : len(spec.Cmdline)-16])
	fs := map[ID]string{
		spec.Kernel:    "foo file",
		spec.Initrd[0]: "bar file",
		spec.Initrd[1]: "baz file",
		quuxID:         "quux file",
	}
	for id, contents := range fs {
		v := mustRead(b.ReadBootFile(id))
		if v != contents {
			t.Fatalf("Wrong file contents for %q: wanted %q, got %q", id, contents, v)
		}
	}
}
