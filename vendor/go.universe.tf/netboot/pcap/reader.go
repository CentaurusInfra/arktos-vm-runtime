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

// Package pcap implements reading and writing the "classic" libpcap format.
package pcap // import "go.universe.tf/netboot/pcap"

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"time"
)

// LinkType describes the contents of each packet in a pcap.
type LinkType uint32

// Some of the more commonly used LinkTypes.
const (
	LinkEthernet LinkType = 1
	LinkRaw      LinkType = 101
)

// Reader extracts packets from a pcap file.
type Reader struct {
	LinkType LinkType

	r     io.Reader
	order binary.ByteOrder
	tmult int64

	pkt *Packet
	err error
}

// Packet is one raw packet and its metadata.
type Packet struct {
	Timestamp time.Time
	Length    int
	Bytes     []byte
}

// NewReader returns a new Reader that decodes pcap data from r.
func NewReader(r io.Reader) (*Reader, error) {
	ret := &Reader{
		r:     bufio.NewReader(r),
		order: binary.LittleEndian,
	}

	header := struct {
		Magic uint32
		Major uint16
		Minor uint16
		// Timezone correction and time accuracy - both 0 in practice.
		Ignored uint64
		Snaplen uint32
		Type    uint32
	}{}

	bs := make([]byte, binary.Size(header))
	if _, err := io.ReadFull(ret.r, bs); err != nil {
		return nil, fmt.Errorf("reading pcap header: %s", err)
	}

	// Annoyingly, the header encodings are defined in terms of "same"
	// or "opposite" endian, rather than in absolute terms, so reading
	// the magic alone (as is intended) doesn't let us figure out what
	// endianness to use. However, we can cheat and look at the
	// major/minor version numbers instead. Try little-endian first,
	// since that's more common these days.
	if err := binary.Read(bytes.NewBuffer(bs), ret.order, &header); err != nil {
		return nil, err
	}
	if header.Major == 0x200 && header.Minor == 0x400 {
		// Byte order was wrong, read again
		ret.order = binary.BigEndian
		if err := binary.Read(bytes.NewBuffer(bs), ret.order, &header); err != nil {
			return nil, err
		}
	}
	switch header.Magic {
	case 0xa1b2c3d4:
		// Timestamps are (sec, usec)
		ret.tmult = 1000
	case 0xa1b23c4d:
		// Timestamps are (sec, nsec)
		ret.tmult = 1
	default:
		return nil, errors.New("bad magic")
	}

	if header.Major != 2 || header.Minor != 4 {
		return nil, fmt.Errorf("Unknown pcap version %d.%d", header.Major, header.Minor)
	}

	ret.LinkType = LinkType(header.Type)

	return ret, nil
}

// Packet returns the packet read by the last call to Next.
func (r *Reader) Packet() *Packet {
	return r.pkt
}

// Err returns the first non-EOF error encountered by the Reader.
func (r *Reader) Err() error {
	if r.err == io.EOF {
		return nil
	}
	return r.err
}

// Next advances the Reader to the next packet in the input, which
// will then be available through the Packet method. It returns false
// when the Reader stops, either by reaching the end of the input or
// an error. After Next returns false, the Err method will return any
// error that occured while reading, except that if it was io.EOF, Err
// will return nil.
func (r *Reader) Next() bool {
	hdr := struct {
		Sec     uint32
		SubSec  uint32
		Len     uint32
		OrigLen uint32
	}{}

	if err := binary.Read(r.r, r.order, &hdr); err != nil {
		r.err = err
		return false
	}

	bs := make([]byte, hdr.Len)
	if _, err := io.ReadFull(r.r, bs); err != nil {
		r.err = err
		return false
	}

	r.pkt = &Packet{
		Timestamp: time.Unix(int64(hdr.Sec), r.tmult*int64(hdr.SubSec)),
		Length:    int(hdr.OrigLen),
		Bytes:     bs,
	}
	return true
}
