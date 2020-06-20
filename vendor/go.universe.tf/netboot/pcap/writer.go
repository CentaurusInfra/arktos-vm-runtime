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
	"encoding/binary"
	"io"
)

// Writer serializes Packets to an io.Writer.
type Writer struct {
	Writer    io.Writer
	LinkType  LinkType
	SnapLen   uint32
	ByteOrder binary.ByteOrder // defaults to binary.LittleEndian

	headerWritten bool
}

func (w *Writer) order() binary.ByteOrder {
	if w.ByteOrder != nil {
		return w.ByteOrder
	}
	return binary.LittleEndian
}

func (w *Writer) header() error {
	hdr := struct {
		Magic   uint32
		Major   uint16
		Minor   uint16
		Ignored uint64
		Snaplen uint32
		Type    uint32
	}{
		Magic:   0xa1b23c4d,
		Major:   2,
		Minor:   4,
		Snaplen: w.SnapLen,
		Type:    uint32(w.LinkType),
	}

	if err := binary.Write(w.Writer, w.order(), hdr); err != nil {
		return err
	}
	w.headerWritten = true
	return nil
}

// Put serializes pkt to w.Writer.
func (w *Writer) Put(pkt *Packet) error {
	if !w.headerWritten {
		if err := w.header(); err != nil {
			return err
		}
	}
	hdr := struct {
		Sec     uint32
		NSec    uint32
		Len     uint32
		OrigLen uint32
	}{
		Sec:     uint32(pkt.Timestamp.Unix()),
		NSec:    uint32(pkt.Timestamp.Nanosecond()),
		Len:     uint32(len(pkt.Bytes)),
		OrigLen: uint32(pkt.Length),
	}

	if err := binary.Write(w.Writer, w.order(), hdr); err != nil {
		return err
	}
	if _, err := w.Writer.Write(pkt.Bytes); err != nil {
		return err
	}
	return nil
}
