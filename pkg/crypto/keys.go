package crypto

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"golang.org/x/crypto/curve25519"
)

// IdentityKeypair holds both Ed25519 (signing) and X25519 (DH) keys
// derived from the same seed.
type IdentityKeypair struct {
	PubKey  ed25519.PublicKey
	PrivKey ed25519.PrivateKey
	DHKey   [32]byte // X25519 private key
	DHPub   [32]byte // X25519 public key
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

func Verify(pub ed25519.PublicKey, data, sig []byte) bool {
	return ed25519.Verify(pub, data, sig)
}

func (ik *IdentityKeypair) PeerID() string {
	h := sha256.Sum256(ik.PubKey)
	return hex.EncodeToString(h[:16])
}
