package crypto

import (
	"crypto/ed25519"
	"encoding/binary"
)

type PreKeyBundle struct {
	IdentityKey      ed25519.PublicKey
	DHPub            [32]byte // Curve25519 pub key from identity
	SignedPreKey     [32]byte // Curve25519 pub
	SignedPreKeySig  []byte   // Ed25519 signature of SignedPreKey by IdentityKey
	OneTimePreKeys   [][32]byte // Curve25519 pub (single-use)
}

type SignedPreKeyRecord struct {
	Key       [32]byte
	Signature []byte
	Serial    uint32
}

func (ik *IdentityKeypair) NewPreKeyBundle(onetimeCount int) (*PreKeyBundle, error) {
	spk, err := GenerateDH()
	if err != nil {
		return nil, err
	}
	serial := make([]byte, 4)
	binary.BigEndian.PutUint32(serial, 0)
	spkSig := ik.Sign(spk.PubKey[:])

	otks := make([][32]byte, onetimeCount)
	for i := range otks {
		k, err := GenerateDH()
		if err != nil {
			return nil, err
		}
		otks[i] = k.PubKey
	}

	return &PreKeyBundle{
		IdentityKey:     ik.PubKey,
		DHPub:           ik.DHPub,
		SignedPreKey:    spk.PubKey,
		SignedPreKeySig: spkSig,
		OneTimePreKeys:  otks,
	}, nil
}

func (pkb *PreKeyBundle) Verify(identityPub ed25519.PublicKey) bool {
	return ed25519.Verify(identityPub, pkb.SignedPreKey[:], pkb.SignedPreKeySig)
}
