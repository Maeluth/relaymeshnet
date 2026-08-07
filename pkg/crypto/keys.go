package crypto

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"golang.org/x/crypto/curve25519"
)

type IdentityKeypair struct {
	PubKey  ed25519.PublicKey
	PrivKey ed25519.PrivateKey
	DHKey   [32]byte // Curve25519 private key derived from same seed
	DHPub   [32]byte // Curve25519 public key
}

func GenerateIdentity() (*IdentityKeypair, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate identity: %w", err)
	}

	seed := priv.Seed()
	var dhPriv [32]byte
	copy(dhPriv[:], seed)
	dhPriv[0] &= 248
	dhPriv[31] &= 127
	dhPriv[31] |= 64

	var dhPub [32]byte
	curve25519.ScalarBaseMult(&dhPub, &dhPriv)

	return &IdentityKeypair{
		PubKey:  pub,
		PrivKey: priv,
		DHKey:   dhPriv,
		DHPub:   dhPub,
	}, nil
}

func (ik *IdentityKeypair) Sign(data []byte) []byte {
	return ed25519.Sign(ik.PrivKey, data)
}

func (ik *IdentityKeypair) Verify(data, sig []byte) bool {
	return ed25519.Verify(ik.PubKey, data, sig)
}

func (ik *IdentityKeypair) PeerID() string {
	h := sha256.Sum256(ik.PubKey)
	return hex.EncodeToString(h[:16])
}

func VerifyPeerID(pubKey ed25519.PublicKey, peerID string) bool {
	h := sha256.Sum256(pubKey)
	return hex.EncodeToString(h[:16]) == peerID
}
