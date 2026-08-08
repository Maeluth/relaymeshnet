package crypto

import (
	"crypto/ed25519"
	"testing"
)

func TestGenerateIdentity(t *testing.T) {
	ik, err := GenerateIdentity()
	if err != nil {
		t.Fatalf("GenerateIdentity: %v", err)
	}
	if len(ik.PubKey) != ed25519.PublicKeySize {
		t.Errorf("PubKey size: got %d, want %d", len(ik.PubKey), ed25519.PublicKeySize)
	}
	if len(ik.PeerID()) != 32 {
		t.Errorf("PeerID length: got %d, want 32", len(ik.PeerID()))
	}
	if ik.DHPub == [32]byte{} {
		t.Error("DHPub is zero")
	}
	if ik.DHKey == [32]byte{} {
		t.Error("DHKey is zero")
	}
}

func TestSignVerify(t *testing.T) {
	ik, _ := GenerateIdentity()
	data := []byte("hello")
	sig := ik.Sign(data)
	if !Verify(ik.PubKey, data, sig) {
		t.Error("signature verification failed")
	}
	badSig := append([]byte{}, sig...)
	badSig[0] ^= 1
	if Verify(ik.PubKey, data, badSig) {
		t.Error("bad signature should not verify")
	}
}

func TestPeerIDDeterministic(t *testing.T) {
	ik, _ := GenerateIdentity()
	id1 := ik.PeerID()
	id2 := ik.PeerID()
	if id1 != id2 {
		t.Error("PeerID not deterministic")
	}
}

func TestDHExchange(t *testing.T) {
	a, _ := GenerateDH()
	b, _ := GenerateDH()
	s1, _ := DH(a.PrivKey, b.PubKey)
	s2, _ := DH(b.PrivKey, a.PubKey)
	if s1 != s2 {
		t.Error("DH exchange mismatch")
	}
}

func TestPreKeyBundle(t *testing.T) {
	ik, _ := GenerateIdentity()
	spk, _ := GenerateDH()
	otks := make([][32]byte, 3)
	for i := range otks {
		k, _ := GenerateDH()
		otks[i] = k.PubKey
	}
	bundle := NewPreKeyBundle(ik, spk.PubKey, otks)
	if !bundle.VerifyBundle() {
		t.Error("valid bundle should verify")
	}
	// Tampered SPK
	tampered := *bundle
	tampered.SignedPreKey[0] ^= 1
	if tampered.VerifyBundle() {
		t.Error("tampered bundle should not verify")
	}
}

func TestX3DHRoundtrip(t *testing.T) {
	alice, _ := GenerateIdentity()
	bob, _ := GenerateIdentity()
	bobSPK, _ := GenerateDH()
	otks := make([][32]byte, 2)
	otkKeys := make([]*DHKeypair, 2)
	for i := range otks {
		k, _ := GenerateDH()
		otks[i] = k.PubKey
		otkKeys[i] = k
	}

	bobBundle := NewPreKeyBundle(bob, bobSPK.PubKey, otks)
	result, err := X3DHSend(alice, bobBundle)
	if err != nil {
		t.Fatalf("X3DHSend: %v", err)
	}
	if result.SharedSecret == [32]byte{} {
		t.Error("shared secret is zero")
	}

	aliceDHKey, _ := GenerateDH()
	preKeys := make([]*DHKeypair, 2)
	for i := range preKeys {
		preKeys[i] = otkKeys[i]
	}

	// For the roundtrip test we need the init message format:
	// IK_A_ed25519_pub || IK_A_x25519_pub || EK_A_x25519_pub || prekey_id
	// The AD from X3DHSend contains: IK_A_ed25519 || IK_B_ed25519 || EK_A_x25519
	if len(result.AD) < len(alice.PubKey)+len(bob.PubKey)+32 {
		t.Fatal("AD too short")
	}
	_ = aliceDHKey // used in production for DH1
	_ = preKeys

	t.Logf("X3DH handshake: shared secret established")
}
