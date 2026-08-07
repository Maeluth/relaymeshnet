package crypto

import (
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"fmt"

	"golang.org/x/crypto/hkdf"
)

type X3DHResult struct {
	SharedSecret [32]byte
	AD           []byte // associated data for Double Ratchet init
	EKPub        [32]byte // ephemeral key sent to receiver
	PreKeyID     uint32 // which one-time pre-key was used
}

func X3DHSend(ik *IdentityKeypair, bobBundle *PreKeyBundle) (*X3DHResult, error) {
	if !bobBundle.Verify(bobBundle.IdentityKey) {
		return nil, fmt.Errorf("pre-key bundle signature invalid")
	}

	ek, err := GenerateDH()
	if err != nil {
		return nil, fmt.Errorf("ephemeral key: %w", err)
	}

	var preKeyID uint32
	var preKeyPub [32]byte
	if len(bobBundle.OneTimePreKeys) > 0 {
		preKeyPub = bobBundle.OneTimePreKeys[0]
		preKeyID = 0
	}

	dh1, _ := DH(ik.DHKey, bobBundle.SignedPreKey)    // IK_A priv × SPK_B pub
	dh2, _ := DH(ek.PrivKey, bobBundle.DHPub)          // EK_A priv × IK_B pub (DH key)
	dh3, _ := DH(ek.PrivKey, bobBundle.SignedPreKey)   // EK_A priv × SPK_B pub

	var dh4 [32]byte
	hasPreKey := len(bobBundle.OneTimePreKeys) > 0 && !isZero(preKeyPub)
	if hasPreKey {
		dh4, _ = DH(ek.PrivKey, preKeyPub) // EK_A priv × OPK_B pub
	}

	secret := make([]byte, 0, 128)
	secret = append(secret, dh1[:]...)
	secret = append(secret, dh2[:]...)
	secret = append(secret, dh3[:]...)
	if hasPreKey {
		secret = append(secret, dh4[:]...)
	}

	sk := sha256.Sum256(secret)

	ad := make([]byte, 0, 96)
	ad = append(ad, ik.PubKey...)
	ad = append(ad, bobBundle.IdentityKey...)
	ad = append(ad, ek.PubKey[:]...)

	return &X3DHResult{
		SharedSecret: sk,
		AD:           ad,
		EKPub:        ek.PubKey,
		PreKeyID:     preKeyID,
	}, nil
}

func X3DHReceive(ik *IdentityKeypair, spk *DHKeypair, preKeys []*DHKeypair, result *X3DHResult) ([32]byte, []byte, error) {
	var aliceIKPubDH [32]byte
	copy(aliceIKPubDH[:], result.AD[:32])
	aliceEKPub := result.EKPub

	dh1, _ := DH(spk.PrivKey, aliceIKPubDH)         // SPK_B priv × IK_A pub (DH key)
	dh2, _ := DH(ik.DHKey, aliceEKPub)               // IK_B priv × EK_A pub
	dh3, _ := DH(spk.PrivKey, aliceEKPub)            // SPK_B priv × EK_A pub

	var dh4 [32]byte
	hasPreKey := result.PreKeyID < uint32(len(preKeys))
	if hasPreKey {
		dh4, _ = DH(preKeys[result.PreKeyID].PrivKey, aliceEKPub)
	}

	secret := make([]byte, 0, 128)
	secret = append(secret, dh1[:]...)
	secret = append(secret, dh2[:]...)
	secret = append(secret, dh3[:]...)
	if hasPreKey {
		secret = append(secret, dh4[:]...)
	}

	sk := sha256.Sum256(secret)
	return sk, result.AD, nil
}

func edToDH(ed ed25519.PrivateKey) [32]byte {
	seed := ed.Seed()
	var dhPriv [32]byte
	copy(dhPriv[:], seed)
	dhPriv[0] &= 248
	dhPriv[31] &= 127
	dhPriv[31] |= 64
	return dhPriv
}

func KDF(key, salt, info []byte, length int) []byte {
	reader := hkdf.New(sha256.New, key, salt, info)
	out := make([]byte, length)
	_, _ = reader.Read(out)
	return out
}

func HMAC(key, data []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write(data)
	return mac.Sum(nil)
}

func Concat(a, b []byte) []byte {
	out := make([]byte, 0, len(a)+len(b)+8)
	out = append(out, a...)
	out = append(out, b...)
	return out
}

func Uint64BE(v uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, v)
	return b
}
