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

	NonceSize  = 24
	MACSize    = 16
	Overhead   = NonceSize + MACSize
	NextHopSize = 32
)

type Hop struct {
	PeerID  [32]byte
	PubKey  [32]byte
}

type OnionPacket struct {
	Data []byte
}

func BuildCircuit(plaintext []byte, hops []Hop, senderPrivKey [32]byte) (*OnionPacket, error) {
	if len(hops) == 0 {
		return nil, errors.New("no hops provided")
	}

	// Start from the innermost layer (final recipient)
	current := make([]byte, 1+len(plaintext))
	current[0] = FlagFinal
	copy(current[1:], plaintext)

	// Build layers from inside out
	for i := len(hops) - 1; i >= 0; i-- {
		hop := hops[i]

		// If not the last hop, prepend next_hop peerID
		if i < len(hops)-1 {
			nextHop := hops[i+1].PeerID
			inner := make([]byte, NextHopSize+len(current))
			copy(inner[0:NextHopSize], nextHop[:])
			copy(inner[NextHopSize:], current)
			current = inner
		}

		// Encrypt for this hop
		encrypted, err := encryptForHop(current, hop.PubKey, senderPrivKey)
		if err != nil {
			return nil, fmt.Errorf("encrypt layer %d: %w", i, err)
		}
		current = encrypted
	}

	return &OnionPacket{Data: current}, nil
}

func UnwrapLayer(packet []byte, privKey [32]byte) (flag byte, payload []byte, err error) {
	if len(packet) < Overhead {
		return 0, nil, errors.New("packet too short")
	}

	// Try to decrypt
	decrypted, err := decryptLayer(packet, privKey)
	if err != nil {
		return 0, nil, fmt.Errorf("decrypt failed: %w", err)
	}

	if len(decrypted) < 1 {
		return 0, nil, errors.New("decrypted payload empty")
	}

	flag = decrypted[0]
	payload = decrypted[1:]

	return flag, payload, nil
}

func GetNextHop(payload []byte) (nextHop [32]byte, innerOnion []byte, err error) {
	if len(payload) < NextHopSize {
		return nextHop, nil, errors.New("payload too short for next_hop")
	}

	copy(nextHop[:], payload[0:NextHopSize])
	innerOnion = payload[NextHopSize:]

	return nextHop, innerOnion, nil
}

func encryptForHop(plaintext []byte, recipientPub [32]byte, senderPriv [32]byte) ([]byte, error) {
	var nonce [NonceSize]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}

	encrypted := box.Seal(nonce[:], plaintext, &nonce, &recipientPub, &senderPriv)
	return encrypted, nil
}

func decryptLayer(packet []byte, privKey [32]byte) ([]byte, error) {
	if len(packet) < NonceSize {
		return nil, errors.New("packet too short for nonce")
	}

	var nonce [NonceSize]byte
	copy(nonce[:], packet[0:NonceSize])

	ciphertext := packet[NonceSize:]

	// We need the sender's public key to decrypt. In onion routing,
	// each hop uses the same ephemeral keypair for all layers.
	// For now, we'll use a placeholder - in production, the sender's
	// public key should be included in the packet or derived from context.
	// This is a simplified version for the prototype.

	// TODO: In production, include sender's ephemeral public key in packet
	// For now, we'll assume the sender's public key is known or derived

	// Placeholder: use zero key (will be replaced in production)
	var senderPub [32]byte

	decrypted, ok := box.Open(nil, ciphertext, &nonce, &senderPub, &privKey)
	if !ok {
		return nil, errors.New("decryption failed")
	}

	return decrypted, nil
}

func OnionSize(plaintextSize, hopCount int) int {
	return plaintextSize + hopCount*Overhead + (hopCount-1)*NextHopSize + 1
}
