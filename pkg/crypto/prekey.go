package crypto

import "crypto/ed25519"

// PreKeyBundle is published by a node to allow others to initiate X3DH.
// IdentityKey is Ed25519 (signing). DHPub, SignedPreKey, OneTimePreKeys are X25519.
type PreKeyBundle struct {
	IdentityKey     ed25519.PublicKey
	DHPub           [32]byte // X25519 pub from identity (derived from same seed)
	SignedPreKey    [32]byte // X25519 pub (medium-term)
	SignedPreKeySig []byte   // Ed25519 signature of SignedPreKey by IdentityKey
	OneTimePreKeys  [][32]byte // X25519 pub (single-use)
}

// NewPreKeyBundle creates a bundle signed by the identity key.
// The caller provides the SPK and OTKs.
func NewPreKeyBundle(ik *IdentityKeypair, spkPub [32]byte, otks [][32]byte) *PreKeyBundle {
	spkSig := ed25519.Sign(ik.PrivKey, spkPub[:])
	return &PreKeyBundle{
		IdentityKey:     ik.PubKey,
		DHPub:           ik.DHPub,
		SignedPreKey:    spkPub,
		SignedPreKeySig: spkSig,
		OneTimePreKeys:  otks,
	}
}

// VerifyBundle checks that the SignedPreKey is signed by the IdentityKey.
// This is NOT self-referential: the caller must already know the IdentityKey
// (e.g. from a trusted source or DHT lookup).
func (pkb *PreKeyBundle) VerifyBundle() bool {
	return ed25519.Verify(pkb.IdentityKey, pkb.SignedPreKey[:], pkb.SignedPreKeySig)
}
