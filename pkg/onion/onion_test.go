package onion

import (
	"testing"

	"github.com/maeluth/relaymeshnet/pkg/crypto"
)

func TestBuildUnwrapSingleHop(t *testing.T) {
	alice, _ := crypto.GenerateIdentity()
	plaintext := []byte("secret message")

	hops := []Hop{
		{PeerID: alicePeerID(alice), PubKey: alice.DHPub},
	}

	pkt, err := BuildCircuit(plaintext, hops)
	if err != nil {
		t.Fatalf("BuildCircuit: %v", err)
	}
	if pkt == nil {
		t.Fatal("nil packet")
	}
	if len(pkt.Data) < Overhead {
		t.Fatalf("packet too small: %d", len(pkt.Data))
	}

	flag, payload, err := UnwrapLayer(pkt.Data, alice.DHPub, alice.DHKey)
	if err != nil {
		t.Fatalf("UnwrapLayer: %v", err)
	}
	if flag != FlagFinal {
		t.Errorf("flag: got %x, want %x", flag, FlagFinal)
	}
	if string(payload) != string(plaintext) {
		t.Errorf("payload: got %q, want %q", payload, plaintext)
	}
}

func TestBuildUnwrapThreeHops(t *testing.T) {
	alice, _ := crypto.GenerateIdentity()
	bob, _ := crypto.GenerateIdentity()
	charlie, _ := crypto.GenerateIdentity()
	plaintext := []byte("message through 3 hops")

	hops := []Hop{
		{PeerID: alicePeerID(alice), PubKey: alice.DHPub},
		{PeerID: alicePeerID(bob), PubKey: bob.DHPub},
		{PeerID: alicePeerID(charlie), PubKey: charlie.DHPub},
	}

	pkt, err := BuildCircuit(plaintext, hops)
	if err != nil {
		t.Fatalf("BuildCircuit: %v", err)
	}

	// Hop 1: Alice decrypts
	flag, payload, err := UnwrapLayer(pkt.Data, alice.DHPub, alice.DHKey)
	if err != nil {
		t.Fatalf("UnwrapLayer hop1: %v", err)
	}
	if flag != FlagRelay {
		t.Errorf("hop1 flag: got %x, want %x", flag, FlagRelay)
	}

	nextHop, inner, err := GetNextHop(payload)
	if err != nil {
		t.Fatalf("GetNextHop hop1: %v", err)
	}
	if nextHop != hops[1].PeerID {
		t.Error("hop1 nextHop mismatch")
	}

	// Hop 2: Bob decrypts
	flag, payload, err = UnwrapLayer(inner, bob.DHPub, bob.DHKey)
	if err != nil {
		t.Fatalf("UnwrapLayer hop2: %v", err)
	}
	if flag != FlagRelay {
		t.Errorf("hop2 flag: got %x, want %x", flag, FlagRelay)
	}

	nextHop, inner, err = GetNextHop(payload)
	if err != nil {
		t.Fatalf("GetNextHop hop2: %v", err)
	}
	if nextHop != hops[2].PeerID {
		t.Error("hop2 nextHop mismatch")
	}

	// Hop 3: Charlie decrypts (final)
	flag, payload, err = UnwrapLayer(inner, charlie.DHPub, charlie.DHKey)
	if err != nil {
		t.Fatalf("UnwrapLayer hop3: %v", err)
	}
	if flag != FlagFinal {
		t.Errorf("hop3 flag: got %x, want %x", flag, FlagFinal)
	}
	if string(payload) != string(plaintext) {
		t.Errorf("payload: got %q, want %q", payload, plaintext)
	}
}

func TestWrongKeyFails(t *testing.T) {
	alice, _ := crypto.GenerateIdentity()
	bob, _ := crypto.GenerateIdentity()
	plaintext := []byte("test")

	hops := []Hop{
		{PeerID: alicePeerID(alice), PubKey: alice.DHPub},
	}

	pkt, _ := BuildCircuit(plaintext, hops)

	_, _, err := UnwrapLayer(pkt.Data, bob.DHPub, bob.DHKey)
	if err == nil {
		t.Error("wrong key should fail")
	}
}

func TestOnionSize(t *testing.T) {
	size := OnionSize(14, 3)
	if size < 14 {
		t.Errorf("onion size too small: %d", size)
	}
	// For 3 hops: 14 + 3*48 + 2*33 + 1 = 14 + 144 + 66 + 1 = 225
	expected := 14 + 3*Overhead + 2*(NextHopSize+1) + 1
	if size != expected {
		t.Errorf("onion size: got %d, want %d", size, expected)
	}
}

func alicePeerID(ik *crypto.IdentityKeypair) [32]byte {
	var id [32]byte
	copy(id[:], ik.DHPub[:])
	return id
}
