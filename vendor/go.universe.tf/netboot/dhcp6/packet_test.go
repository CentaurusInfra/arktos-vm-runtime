package dhcp6

import (
	"encoding/binary"
	"testing"
)

func TestShouldDiscardSolicitWithoutBootfileUrlOption(t *testing.T) {
	clientID := []byte("clientid")
	options := make(Options)
	options.Add(&Option{ID: OptClientID, Length: uint16(len(clientID)), Value: clientID})
	solicit := &Packet{Type: MsgSolicit, TransactionID: [3]byte{'1', '2', '3'}, Options: options}

	if err := shouldDiscardSolicit(solicit); err == nil {
		t.Fatalf("Should discard solicit packet without bootfile url option, but didn't")
	}
}

func TestShouldDiscardSolicitWithoutClientIdOption(t *testing.T) {
	options := make(Options)
	options.Add(MakeOptionRequestOptions([]uint16{OptBootfileURL}))
	solicit := &Packet{Type: MsgSolicit, TransactionID: [3]byte{'1', '2', '3'}, Options: options}

	if err := shouldDiscardSolicit(solicit); err == nil {
		t.Fatalf("Should discard solicit packet without client id option, but didn't")
	}
}

func TestShouldDiscardSolicitWithServerIdOption(t *testing.T) {
	serverID := []byte("serverid")
	clientID := []byte("clientid")
	options := make(Options)
	options.Add(MakeOptionRequestOptions([]uint16{OptBootfileURL}))
	options.Add(&Option{ID: OptClientID, Length: uint16(len(clientID)), Value: clientID})
	options.Add(&Option{ID: OptServerID, Length: uint16(len(serverID)), Value: serverID})
	solicit := &Packet{Type: MsgSolicit, TransactionID: [3]byte{'1', '2', '3'}, Options: options}

	if err := shouldDiscardSolicit(solicit); err == nil {
		t.Fatalf("Should discard solicit packet with server id option, but didn't")
	}
}

func TestShouldDiscardRequestWithoutBootfileUrlOption(t *testing.T) {
	serverID := []byte("serverid")
	clientID := []byte("clientid")
	options := make(Options)
	options.Add(&Option{ID: OptClientID, Length: uint16(len(clientID)), Value: clientID})
	options.Add(&Option{ID: OptServerID, Length: uint16(len(serverID)), Value: serverID})
	request := &Packet{Type: MsgRequest, TransactionID: [3]byte{'1', '2', '3'}, Options: options}

	if err := shouldDiscardRequest(request, serverID); err == nil {
		t.Fatalf("Should discard request packet without bootfile url option, but didn't")
	}
}

func TestShouldDiscardRequestWithoutClientIdOption(t *testing.T) {
	serverID := []byte("serverid")
	options := make(Options)
	options.Add(MakeOptionRequestOptions([]uint16{OptBootfileURL}))
	options.Add(&Option{ID: OptServerID, Length: uint16(len(serverID)), Value: serverID})
	request := &Packet{Type: MsgRequest, TransactionID: [3]byte{'1', '2', '3'}, Options: options}

	if err := shouldDiscardRequest(request, serverID); err == nil {
		t.Fatalf("Should discard request packet without client id option, but didn't")
	}
}

func TestShouldDiscardRequestWithoutServerIdOption(t *testing.T) {
	clientID := []byte("clientid")
	options := make(Options)
	options.Add(MakeOptionRequestOptions([]uint16{OptBootfileURL}))
	options.Add(&Option{ID: OptClientID, Length: uint16(len(clientID)), Value: clientID})
	request := &Packet{Type: MsgRequest, TransactionID: [3]byte{'1', '2', '3'}, Options: options}

	if err := shouldDiscardRequest(request, []byte("serverid")); err == nil {
		t.Fatalf("Should discard request packet with server id option, but didn't")
	}
}

func TestShouldDiscardRequestWithWrongServerId(t *testing.T) {
	clientID := []byte("clientid")
	serverID := []byte("serverid")
	options := make(Options)
	options.Add(MakeOptionRequestOptions([]uint16{OptBootfileURL}))
	options.Add(&Option{ID: OptClientID, Length: uint16(len(clientID)), Value: clientID})
	options.Add(&Option{ID: OptServerID, Length: uint16(len(serverID)), Value: serverID})
	request := &Packet{Type: MsgRequest, TransactionID: [3]byte{'1', '2', '3'}, Options: options}

	if err := shouldDiscardRequest(request, []byte("wrongid")); err == nil {
		t.Fatalf("Should discard request packet with wrong server id option, but didn't")
	}
}

func MakeOptionRequestOptions(options []uint16) *Option {
	value := make([]byte, len(options)*2)
	for i, option := range options {
		binary.BigEndian.PutUint16(value[i*2:], option)
	}

	return &Option{ID: OptOro, Length: uint16(len(options) * 2), Value: value}
}
