package crypto

import (
	"crypto/rand"
	"fmt"

	"golang.org/x/crypto/curve25519"
)

type DHKeypair struct {
	PubKey  [32]byte
	PrivKey [32]byte
}

func GenerateDH() (*DHKeypair, error) {
	var priv [32]byte
	if _, err := rand.Read(priv[:]); err != nil {
		return nil, fmt.Errorf("generate DH: %w", err)
	}
	priv[0] &= 248
	priv[31] &= 127
	priv[31] |= 64

	var pub [32]byte
	curve25519.ScalarBaseMult(&pub, &priv)
	return &DHKeypair{PubKey: pub, PrivKey: priv}, nil
}

func DH(priv [32]byte, pub [32]byte) ([32]byte, error) {
	var shared [32]byte
	curve25519.ScalarMult(&shared, &priv, &pub)
	return shared, nil
}
