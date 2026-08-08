package onion

import (
	"crypto/rand"
	"errors"
	"fmt"

	"golang.org/x/crypto/nacl/box"
)

const (
	FlagFinal = 0x00
	FlagRelay = 0x01

	Overhead   = box.AnonymousOverhead
	NextHopSize = 32
)

type Hop struct {
	PeerID [32]byte
	PubKey [32]byte
}

type OnionPacket struct {
	Data []byte
}

// BuildCircuit builds an onion-encrypted packet for a list of hops.
// Uses box.SealAnonymous — no sender identity at each layer (anonymous forwarding).
func BuildCircuit(plaintext []byte, hops []Hop) (*OnionPacket, error) {
	if len(hops) == 0 {
		return nil, errors.New("no hops provided")
	}

	current := make([]byte, 1+len(plaintext))
	current[0] = FlagFinal
	copy(current[1:], plaintext)

	for i := len(hops) - 1; i >= 0; i-- {
		hop := hops[i]

		if i < len(hops)-1 {
			nextHop := hops[i+1].PeerID
			inner := make([]byte, NextHopSize+1+len(current)) // +1 for FLAG_RELAY
			inner[0] = FlagRelay
			copy(inner[1:NextHopSize+1], nextHop[:])
			copy(inner[NextHopSize+1:], current)
			current = inner
		}

		encrypted, err := box.SealAnonymous(nil, current, &hop.PubKey, rand.Reader)
		if err != nil {
			return nil, fmt.Errorf("encrypt layer %d: %w", i, err)
		}
		current = encrypted
	}

	return &OnionPacket{Data: current}, nil
}

// UnwrapLayer decrypts one onion layer. Returns flag (RELAY/FINAL) and inner payload.
func UnwrapLayer(packet []byte, pubKey, privKey [32]byte) (flag byte, payload []byte, err error) {
	if len(packet) < Overhead {
		return 0, nil, errors.New("packet too short")
	}

	decrypted, ok := box.OpenAnonymous(nil, packet, &pubKey, &privKey)
	if !ok {
		return 0, nil, errors.New("decrypt failed: wrong key or corrupted packet")
	}

	if len(decrypted) < 1 {
		return 0, nil, errors.New("decrypted payload empty")
	}

	flag = decrypted[0]
	payload = decrypted[1:]

	return flag, payload, nil
}

// GetNextHop extracts the next hop peerID and inner onion from a RELAY payload.
func GetNextHop(payload []byte) (nextHop [32]byte, innerOnion []byte, err error) {
	if len(payload) < NextHopSize {
		return nextHop, nil, errors.New("payload too short for next_hop")
	}

	copy(nextHop[:], payload[0:NextHopSize])
	innerOnion = payload[NextHopSize:]

	return nextHop, innerOnion, nil
}

func OnionSize(plaintextSize, hopCount int) int {
	return plaintextSize + hopCount*Overhead + (hopCount-1)*(NextHopSize+1) + 1
}
