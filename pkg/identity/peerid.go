package identity

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
)

func PeerID(pub ed25519.PublicKey) string {
	h := sha256.Sum256(pub)
	return hex.EncodeToString(h[:16])
}

func Verify(pub ed25519.PublicKey, peerID string) bool {
	return PeerID(pub) == peerID
}
