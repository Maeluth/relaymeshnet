package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"fmt"
)

const (
	MaxSkip = 1000
)

type RatchetState struct {
	DHKeypair        DHKeypair
	DHRatchetPub     *[32]byte
	RootKey          [32]byte
	SendingChainKey  [32]byte
	ReceivingChainKey [32]byte
	SendMsgNum       uint32
	RecvMsgNum       uint32
	PrevSendMsgNum   uint32
	AD               []byte

	skippedKeys map[[32]byte][32]byte
}

func NewRatchet(sharedSecret [32]byte, ad []byte, ourKey *DHKeypair, theirRatchetPub [32]byte) *RatchetState {
	rs := &RatchetState{
		RootKey:     sharedSecret,
		AD:          ad,
		skippedKeys: make(map[[32]byte][32]byte),
	}
	rs.DHKeypair = *ourKey
	rs.DHRatchetPub = new([32]byte)
	*rs.DHRatchetPub = theirRatchetPub

	rs.SendingChainKey = computeChainKey(rs.RootKey, dhRatchet(rs.DHKeypair, *rs.DHRatchetPub))
	rs.ReceivingChainKey = rs.SendingChainKey
	rs.SendMsgNum = 0
	rs.RecvMsgNum = 0
	rs.PrevSendMsgNum = 0
	return rs
}

func (rs *RatchetState) Encrypt(plaintext []byte) ([]byte, error) {
	key, nextChain := deriveMessageKey(rs.SendingChainKey)
	rs.SendingChainKey = nextChain
	rs.SendMsgNum++

	mk := chainKeyToMessageKey(key)

	nonce := make([]byte, 12)
	copy(nonce[4:], Uint64BE(uint64(rs.SendMsgNum-1)))

	block, err := aes.NewCipher(mk[:])
	if err != nil {
		return nil, err
	}
	aesgcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	ciphertext := aesgcm.Seal(nil, nonce, plaintext, rs.AD)

	header := ratchetHeader(rs)
	out := make([]byte, 0, len(header)+len(ciphertext))
	out = append(out, header...)
	out = append(out, ciphertext...)

	if rs.SendMsgNum-rs.PrevSendMsgNum > 0 {
		rs.PrevSendMsgNum = rs.SendMsgNum
	}
	return out, nil
}

func (rs *RatchetState) Decrypt(packet []byte) ([]byte, error) {
	if len(packet) < 34 {
		return nil, fmt.Errorf("packet too short")
	}

	header := packet[:34]
	ciphertext := packet[34:]

	hasNewRatchetPub := header[0] == 1
	msgNum := uint32(header[1])<<16 | uint32(header[2])<<8 | uint32(header[3])

	if hasNewRatchetPub {
		var newPub [32]byte
		copy(newPub[:], header[2:34])
		rs.dhRatchetStep(newPub)
	}

	nonce := make([]byte, 12)
	copy(nonce[4:], Uint64BE(uint64(msgNum)))

	key, err := rs.trySkippedMessageKeys(ciphertext, nonce, msgNum)
	if err == nil {
		return key, nil
	}

	for skipped := rs.RecvMsgNum; skipped < msgNum; skipped++ {
		mk, nextChain := deriveMessageKey(rs.ReceivingChainKey)
		rs.ReceivingChainKey = nextChain
		messageKey := chainKeyToMessageKey(mk)
		skippedNonce := make([]byte, 12)
		copy(skippedNonce[4:], Uint64BE(uint64(skipped)))
		var msgKey [32]byte
		copy(msgKey[:], messageKey[:])
		rs.skippedKeys[msgKey] = messageKey
	}

	mk, nextChain := deriveMessageKey(rs.ReceivingChainKey)
	rs.ReceivingChainKey = nextChain
	messageKey := chainKeyToMessageKey(mk)
	rs.RecvMsgNum = msgNum + 1

	return aesGCMDecrypt(messageKey[:], nonce, ciphertext, rs.AD)
}

func (rs *RatchetState) dhRatchetStep(newPub [32]byte) {
	*rs.DHRatchetPub = newPub
	dhOut := dhRatchet(rs.DHKeypair, *rs.DHRatchetPub)

	newKey, _ := GenerateDH()
	rs.DHKeypair = *newKey

	var sendingKey, receivingKey [32]byte
	copy(sendingKey[:], KDF(dhOut[:], nil, nil, 32))
	copy(receivingKey[:], rs.RootKey[:])

	h1 := HMAC(sendingKey[:], rs.RootKey[:])
	h2 := HMAC(receivingKey[:], rs.RootKey[:])
	xorKey(h1, h2)
	copy(rs.RootKey[:], h1)

	rs.SendingChainKey = computeChainKey(rs.RootKey, dhRatchet(*newKey, *rs.DHRatchetPub))
	rs.ReceivingChainKey = computeChainKey(rs.RootKey, dhOut)
	rs.SendMsgNum = 0
	rs.PrevSendMsgNum = 0
}

func (rs *RatchetState) trySkippedMessageKeys(ciphertext, nonce []byte, msgNum uint32) ([]byte, error) {
	for mk, key := range rs.skippedKeys {
		plain, err := aesGCMDecrypt(key[:], nonce, ciphertext, rs.AD)
		if err == nil {
			delete(rs.skippedKeys, mk)
			return plain, nil
		}
	}
	return nil, fmt.Errorf("not in skipped")
}

func ratchetHeader(rs *RatchetState) []byte {
	h := make([]byte, 34)
	h[0] = 0
	h[1] = byte(rs.SendMsgNum >> 16)
	h[2] = byte(rs.SendMsgNum >> 8)
	h[3] = byte(rs.SendMsgNum)
	if rs.SendMsgNum == 0 || (rs.SendMsgNum > 0 && rs.SendMsgNum-rs.PrevSendMsgNum > 0) {
		h[0] = 1
		copy(h[2:34], rs.DHKeypair.PubKey[:])
		rs.SendMsgNum = 0
	}
	return h
}

func dhRatchet(kp DHKeypair, pub [32]byte) [32]byte {
	out, _ := DH(kp.PrivKey, pub)
	return out
}

func computeChainKey(rootKey, dhOutput [32]byte) [32]byte {
	material := make([]byte, 64)
	copy(material[:32], rootKey[:])
	copy(material[32:], dhOutput[:])
	return sha256.Sum256(material)
}

func deriveMessageKey(chainKey [32]byte) (messageKey, nextChainKey [32]byte) {
	h := sha256.Sum256(chainKey[:])
	copy(messageKey[:], h[:16])
	copy(nextChainKey[:], h[16:])
	return
}

func chainKeyToMessageKey(chain [32]byte) [32]byte {
	mk, _ := deriveMessageKey(chain)
	return mk
}

func aesGCMDecrypt(key, nonce, ciphertext, ad []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aesgcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return aesgcm.Open(nil, nonce, ciphertext, ad)
}

func xorKey(a, b []byte) {
	for i := 0; i < len(a) && i < len(b); i++ {
		a[i] ^= b[i]
	}
}

func isZero(b [32]byte) bool {
	for _, v := range b {
		if v != 0 {
			return false
		}
	}
	return true
}
