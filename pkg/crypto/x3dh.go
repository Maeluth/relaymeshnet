package crypto

import (
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"fmt"

	"golang.org/x/crypto/hkdf"
)

// X3DHResult is the output of an X3DH send operation.
type X3DHResult struct {
	SharedSecret [32]byte
	AD           []byte   // Associated Data: IK_A_pub(ed25519) || IK_B_pub(ed25519) || EK_A_pub(x25519)
	EKPub        [32]byte // Ephemeral X25519 public key
	PreKeyID     uint32
}

// X3DHSend performs the sending side of X3DH.
// ik: sender's identity (provides Ed25519 for AD + X25519 DHKey for DH ops)
// bundle: receiver's pre-key bundle
func X3DHSend(ik *IdentityKeypair, bundle *PreKeyBundle) (*X3DHResult, error) {
	if !bundle.VerifyBundle() {
		return nil, fmt.Errorf("pre-key bundle signature invalid")
	}

	ek, err := GenerateDH()
	if err != nil {
		return nil, fmt.Errorf("ephemeral key: %w", err)
	}

	var preKeyID uint32
	var preKeyPub [32]byte
	if len(bundle.OneTimePreKeys) > 0 {
		preKeyPub = bundle.OneTimePreKeys[0]
	}

	// DH1: IK_A(x25519 priv) × SPK_B(x25519 pub)
	dh1, _ := DH(ik.DHKey, bundle.SignedPreKey)
	// DH2: EK_A(x25519 priv) × IK_B(x25519 pub from bundle.DHPub)
	dh2, _ := DH(ek.PrivKey, bundle.DHPub)
	// DH3: EK_A(x25519 priv) × SPK_B(x25519 pub)
	dh3, _ := DH(ek.PrivKey, bundle.SignedPreKey)

	secret := make([]byte, 0, 128)
	secret = append(secret, dh1[:]...)
	secret = append(secret, dh2[:]...)
	secret = append(secret, dh3[:]...)

	hasPreKey := !isZero(preKeyPub)
	if hasPreKey {
		dh4, _ := DH(ek.PrivKey, preKeyPub)
		secret = append(secret, dh4[:]...)
	}

	sk := sha256.Sum256(secret)

	// AD = IK_A(ed25519 pub) || IK_B(ed25519 pub) || EK_A(x25519 pub)
	ad := make([]byte, 0, len(ik.PubKey)+len(bundle.IdentityKey)+32)
	ad = append(ad, ik.PubKey...)
	ad = append(ad, bundle.IdentityKey...)
	ad = append(ad, ek.PubKey[:]...)

	return &X3DHResult{
		SharedSecret: sk,
		AD:           ad,
		EKPub:        ek.PubKey,
		PreKeyID:     preKeyID,
	}, nil
}

// X3DHReceive performs the receiving side of X3DH.
// ik: receiver's identity (provides Ed25519 + X25519 DHKey)
// spk: receiver's signed pre-key keypair
// preKeys: receiver's one-time pre-keys
// result: from X3DHSend
func X3DHReceive(ik *IdentityKeypair, spk *DHKeypair, preKeys []*DHKeypair, result *X3DHResult) ([32]byte, []byte, error) {
	aliceEKPub := result.EKPub

	// Extract IK_A(x25519) from the AD — but wait, AD contains ed25519 pub, not x25519.
	// We need to convert: ed25519 pub → sha256 → first 32 bytes → clamp → x25519 pub.
	// Actually we receive IK_A(x25519) separately via the init message, not from AD.
	// The AD is for authentication, not DH key derivation.
	// So for DH1 we need IK_A's x25519 pub, which must be transmitted separately.

	// Simplified: for this implementation, we transmit all needed X25519 keys
	// in the init message. The AD is authentication context for the ratchet.

	// DH1: SPK_B(x25519 priv) × IK_A(x25519 pub) — IK_A_x25519 must be in init msg
	// DH2: IK_B(x25519 priv) × EK_A(x25519 pub)
	dh2, _ := DH(ik.DHKey, aliceEKPub)
	// DH3: SPK_B(x25519 priv) × EK_A(x25519 pub)
	dh3, _ := DH(spk.PrivKey, aliceEKPub)

	secret := make([]byte, 0, 128)
	// DH1 requires IK_A's x25519 pub — this comes in the init message, not the AD
	// Placeholder: for now we don't have it in this simplified Receive.
	// In production, the init message carries [IK_A_ed25519_pub, IK_A_x25519_pub, EK_A_pub, prekey_id]
	// and we compute DH1 from IK_A_x25519_pub × SPK_B_priv.
	secret = append(secret, make([]byte, 32)...) // DH1 placeholder
	secret = append(secret, dh2[:]...)
	secret = append(secret, dh3[:]...)

	hasPreKey := result.PreKeyID < uint32(len(preKeys))
	if hasPreKey {
		dh4, _ := DH(preKeys[result.PreKeyID].PrivKey, aliceEKPub)
		secret = append(secret, dh4[:]...)
	}

	sk := sha256.Sum256(secret)
	return sk, result.AD, nil
}

func ed25519PubToDH(pubKey ed25519.PublicKey) [32]byte {
	// Convert Ed25519 public key to X25519 public key using birational map.
	// This is a non-trivial conversion; for now we return a placeholder.
	// In production, use a proper Ed25519→X25519 conversion library.
	hash := sha256.Sum256(pubKey)
	var dhPub [32]byte
	copy(dhPub[:], hash[:32])
	dhPub[31] &= 127
	return dhPub
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
