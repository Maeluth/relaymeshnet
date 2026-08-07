package protocol

import (
	"encoding/binary"
	"fmt"
)

const (
	MagicSize   = 4
	VersionSize = 2
	TypeSize    = 2
	LengthSize  = 2
	HeaderSize  = MagicSize + VersionSize + TypeSize + LengthSize
	MaxPayload  = 65535
)

var Magic = [4]byte{'R', 'M', 'N', 0x01}

type MessageType uint16

const (
	MsgChat    MessageType = 0x0001
	MsgFile    MessageType = 0x0002
	MsgConfirm MessageType = 0x0003
	MsgPing    MessageType = 0x0004
	MsgPong    MessageType = 0x0005
	MsgPoR     MessageType = 0x0006
	MsgTransfer MessageType = 0x0007
	MsgDHTPing MessageType = 0x0010
	MsgDHTStore MessageType = 0x0011
	MsgDHTFind MessageType = 0x0012
	MsgGossip  MessageType = 0x0020
	MsgCover   MessageType = 0x0030
)

type Frame struct {
	Version uint16
	Type    MessageType
	Payload []byte
}

func (f *Frame) Marshal() ([]byte, error) {
	if len(f.Payload) > MaxPayload {
		return nil, fmt.Errorf("payload too large: %d > %d", len(f.Payload), MaxPayload)
	}

	buf := make([]byte, HeaderSize+len(f.Payload))
	copy(buf[0:4], Magic[:])
	binary.BigEndian.PutUint16(buf[4:6], f.Version)
	binary.BigEndian.PutUint16(buf[6:8], uint16(f.Type))
	binary.BigEndian.PutUint16(buf[8:10], uint16(len(f.Payload)))
	copy(buf[10:], f.Payload)

	return buf, nil
}

func Unmarshal(data []byte) (*Frame, error) {
	if len(data) < HeaderSize {
		return nil, fmt.Errorf("frame too short: %d < %d", len(data), HeaderSize)
	}

	var magic [4]byte
	copy(magic[:], data[0:4])
	if magic != Magic {
		return nil, fmt.Errorf("invalid magic: %v", magic)
	}

	version := binary.BigEndian.Uint16(data[4:6])
	msgType := MessageType(binary.BigEndian.Uint16(data[6:8]))
	length := binary.BigEndian.Uint16(data[8:10])

	if len(data) < HeaderSize+int(length) {
		return nil, fmt.Errorf("payload truncated: got %d, want %d", len(data)-HeaderSize, length)
	}

	payload := make([]byte, length)
	copy(payload, data[HeaderSize:HeaderSize+int(length)])

	return &Frame{
		Version: version,
		Type:    msgType,
		Payload: payload,
	}, nil
}
