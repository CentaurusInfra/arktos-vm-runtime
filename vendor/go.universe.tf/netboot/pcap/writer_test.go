// Copyright 2016 Google Inc.
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

package pcap

import (
	"bytes"
	"encoding/binary"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

func TestReadback(t *testing.T) {
	pkts := []*Packet{
		{
			Timestamp: time.Now(),
			Length:    42,
			Bytes:     []byte{1, 2, 3, 4},
		},
		{
			Timestamp: time.Now(),
			Length:    30,
			Bytes:     []byte{2, 3, 4, 5},
		},
		{
			Timestamp: time.Now(),
			Length:    20,
			Bytes:     []byte{3, 4, 5, 6},
		},
		{
			Timestamp: time.Now(),
			Length:    10,
			Bytes:     []byte{4, 5, 6, 7},
		},
	}

	serializations := map[string]bool{}
	for _, order := range []binary.ByteOrder{nil, binary.LittleEndian, binary.BigEndian} {
		var b bytes.Buffer
		w := &Writer{
			Writer:    &b,
			LinkType:  LinkEthernet,
			SnapLen:   65535,
			ByteOrder: order,
		}

		for _, pkt := range pkts {
			if err := w.Put(pkt); err != nil {
				t.Fatalf("Writing packet %#v: %s", pkt, err)
			}
		}

		// Record the binary form, to check how many different serializations we get.
		serializations[b.String()] = true

		readBack := []*Packet{}
		r, err := NewReader(&b)
		if err != nil {
			t.Fatalf("Initializing reader for writer read-back: %s", err)
		}
		if r.LinkType != LinkEthernet {
			t.Fatalf("Wrote link type %d, read back %d", LinkEthernet, r.LinkType)
		}

		for r.Next() {
			readBack = append(readBack, r.Packet())
		}
		if r.Err() != nil {
			t.Fatalf("Reading packets back: %s", r.Err())
		}

		if diff := cmp.Diff(pkts, readBack); diff != "" {
			t.Fatalf("Packets mutated by write-then-read (-want +got):\n%s", diff)
		}
	}

	if len(serializations) != 2 {
		t.Fatalf("Expected 2 distinct serializations due to endianness, got %d", len(serializations))
	}
}
