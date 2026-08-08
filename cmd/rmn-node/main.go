package main

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/maeluth/relaymeshnet/pkg/crypto"
	"github.com/maeluth/relaymeshnet/pkg/identity"
	"github.com/maeluth/relaymeshnet/pkg/transport"
)

type Node struct {
	mu          sync.RWMutex
	Identity    *crypto.IdentityKeypair
	PeerID      string
	Transport   transport.Transport
	PreKeyStore *crypto.PreKeyBundle
	SPK         *crypto.DHKeypair           // current signed pre-key
	PreKeys     map[uint32]*crypto.DHKeypair // one-time pre-keys
	Ratchet     *crypto.RatchetState

	peers   map[string]*PeerSession
	inbox   []string
}

type PeerSession struct {
	PeerID         string
	IdentityPub    []byte
	Ratchet        *crypto.RatchetState
}

type ChatMsg struct {
	Type string `json:"type"`
	From string `json:"from"`
	Text string `json:"text"`
}

func NewNode(name string, tr transport.Transport) (*Node, error) {
	ik, err := crypto.GenerateIdentity()
	if err != nil {
		return nil, fmt.Errorf("generate identity: %w", err)
	}

	spk, err := crypto.GenerateDH()
	if err != nil {
		return nil, err
	}

	n := &Node{
		Identity:    ik,
		PeerID:      identity.PeerID(ik.PubKey),
		Transport:   tr,
		SPK:         spk,
		PreKeys:     make(map[uint32]*crypto.DHKeypair),
		peers:       make(map[string]*PeerSession),
	}

	otks := make([][32]byte, 5)
	for i := uint32(0); i < 5; i++ {
		pk, _ := crypto.GenerateDH()
		n.PreKeys[i] = pk
		otks[i] = pk.PubKey
	}
	n.PreKeyStore = crypto.NewPreKeyBundle(ik, spk.PubKey, otks)

	fmt.Printf("[%s] PeerID: %s\n", name, n.PeerID[:8])
	return n, nil
}

func (n *Node) InitiateSession(peerID string, bundle *crypto.PreKeyBundle) error {
	result, err := crypto.X3DHSend(n.Identity, bundle)
	if err != nil {
		return fmt.Errorf("X3DH send: %w", err)
	}

	ourDH, _ := crypto.GenerateDH()
	ratchet := crypto.NewRatchet(result.SharedSecret, result.AD, ourDH, bundle.SignedPreKey)

	n.mu.Lock()
	n.peers[peerID] = &PeerSession{
		PeerID:      peerID,
		IdentityPub: bundle.IdentityKey,
		Ratchet:     ratchet,
	}
	n.mu.Unlock()

	initData, _ := json.Marshal(map[string]string{
		"type":         "x3dh_init",
		"from":        n.PeerID,
		"ik_pub":      hex.EncodeToString(n.Identity.PubKey),
		"dh_pub":      hex.EncodeToString(n.Identity.DHPub[:]),
		"ek_pub":      hex.EncodeToString(result.EKPub[:]),
		"prekey_id":   fmt.Sprintf("%d", result.PreKeyID),
	})
	n.Transport.Send(peerID, initData)

	return nil
}

func (n *Node) HandleIncoming(peerID string, payload []byte) {
	var msg ChatMsg
	if err := json.Unmarshal(payload, &msg); err != nil {
		return
	}

	switch msg.Type {
	case "x3dh_init":
		n.handleX3DHReceive(peerID, payload)
	case "chat":
		n.handleChat(peerID, payload)
	}
}

func (n *Node) handleX3DHReceive(peerID string, payload []byte) {
	var initData map[string]string
	if err := json.Unmarshal(payload, &initData); err != nil {
		return
	}

	ikPubBytes, _ := hex.DecodeString(initData["ik_pub"])
	dhPubBytes, _ := hex.DecodeString(initData["dh_pub"])
	ekPubBytes, _ := hex.DecodeString(initData["ek_pub"])

	var dhPub, ekPub [32]byte
	copy(dhPub[:], dhPubBytes)
	copy(ekPub[:], ekPubBytes)

	ad := make([]byte, 0, 96)
	ad = append(ad, ikPubBytes...)
	ad = append(ad, n.Identity.PubKey...)
	ad = append(ad, ekPub[:]...)

	preKeys := make([]*crypto.DHKeypair, 5)
	for i := uint32(0); i < 5; i++ {
		if pk, ok := n.PreKeys[i]; ok {
			preKeys[i] = pk
		}
	}

	result := &crypto.X3DHResult{
		SharedSecret: [32]byte{},
		AD:           ad,
		EKPub:        ekPub,
		PreKeyID:     0,
	}

	sharedSecret, adOut, err := crypto.X3DHReceive(n.Identity, n.SPK, preKeys, result)
	if err != nil {
		fmt.Printf("X3DH receive error: %v\n", err)
		return
	}

	ourDH, _ := crypto.GenerateDH()
	ratchet := crypto.NewRatchet(sharedSecret, adOut, ourDH, ekPub)

	var identityPub []byte
	n.mu.RLock()
	if p, ok := n.peers[peerID]; ok {
		identityPub = p.IdentityPub
	}
	n.mu.RUnlock()

	n.mu.Lock()
	n.peers[peerID] = &PeerSession{
		PeerID:      peerID,
		IdentityPub: identityPub,
		Ratchet:     ratchet,
	}
	n.mu.Unlock()

	// Re-parse result's AD for the ratchet
	// The Ratchet's AD is set from the X3DH receive
}

func (n *Node) handleChat(peerID string, raw []byte) {
	n.mu.RLock()
	session, ok := n.peers[peerID]
	n.mu.RUnlock()

	if !ok {
		fmt.Printf("[!] No session for %s\n", peerID[:8])
		return
	}

	plain, err := session.Ratchet.Decrypt(raw[34:]) // Skip header
	if err != nil {
		fmt.Printf("[!] Decrypt error: %v\n", err)
		return
	}

	fmt.Printf("\n[%s] %s\n> ", peerID[:8], string(plain))
}

func (n *Node) SendChat(peerID, text string) error {
	n.mu.RLock()
	session, ok := n.peers[peerID]
	n.mu.RUnlock()

	if !ok {
		return fmt.Errorf("no session with %s", peerID[:8])
	}

	encrypted, err := session.Ratchet.Encrypt([]byte(text))
	if err != nil {
		return fmt.Errorf("encrypt: %w", err)
	}

	msg, _ := json.Marshal(ChatMsg{
		Type: "chat",
		From: n.PeerID,
		Text: hex.EncodeToString(encrypted),
	})

	return n.Transport.Send(peerID, msg)
}

func (n *Node) GetPreKeyBundle() *crypto.PreKeyBundle {
	return n.PreKeyStore
}

func main() {
	mode := "alice"
	if len(os.Args) > 1 {
		mode = os.Args[1]
	}

	aliceTr := transport.NewMockTransport("alice")
	bobTr := transport.NewMockTransport("bob")
	aliceTr.Connect(bobTr)

	alice, err := NewNode("Alice", aliceTr)
	if err != nil {
		panic(err)
	}
	bob, err := NewNode("Bob", bobTr)
	if err != nil {
		panic(err)
	}

	bobTr.SetHandler(func(peerID string, payload []byte) {
		bob.HandleIncoming(peerID, payload)
	})
	aliceTr.SetHandler(func(peerID string, payload []byte) {
		alice.HandleIncoming(peerID, payload)
	})

	// X3DH handshake: Alice initiates with Bob
	err = alice.InitiateSession(bob.PeerID, bob.GetPreKeyBundle())
	if err != nil {
		panic(err)
	}
	time.Sleep(100 * time.Millisecond)

	// Send test messages
	go func() {
		for i := 1; i <= 3; i++ {
			time.Sleep(500 * time.Millisecond)
			text := fmt.Sprintf("Hello Bob! Message #%d", i)
			fmt.Printf("[Alice] Sending: %s\n", text)
			alice.SendChat(bob.PeerID, text)
		}
	}()

	go func() {
		for i := 1; i <= 2; i++ {
			time.Sleep(700 * time.Millisecond)
			text := fmt.Sprintf("Hey Alice! Reply #%d", i)
			fmt.Printf("[Bob] Sending: %s\n", text)
			bob.SendChat(alice.PeerID, text)
		}
	}()

	// HTTP API for debugging
	http.HandleFunc("/api/info", func(w http.ResponseWriter, r *http.Request) {
		info := map[string]interface{}{
			"alice_peerid": alice.PeerID,
			"bob_peerid":   bob.PeerID,
			"alice_peers":  len(alice.peers),
			"bob_peers":    len(bob.peers),
		}
		json.NewEncoder(w).Encode(info)
	})

	port := "18080"
	fmt.Printf("\n=== RelayMeshNet Node ===\n")
	fmt.Printf("Alice PeerID: %s\n", alice.PeerID)
	fmt.Printf("Bob   PeerID: %s\n", bob.PeerID)
	fmt.Printf("API: http://localhost:%s/api/info\n", port)
	fmt.Printf("Messages will appear above...\n")
	fmt.Printf("Type 'exit' to stop.\n> ")

	go func() {
		for {
			inbox := (<-bobTr.Recv())
			if inbox != nil {
				bob.HandleIncoming(inbox.FromPeerID, inbox.Payload)
			}
		}
	}()

	go func() {
		for {
			inbox := (<-aliceTr.Recv())
			if inbox != nil {
				alice.HandleIncoming(inbox.FromPeerID, inbox.Payload)
			}
		}
	}()

	go http.ListenAndServe(":"+port, nil)

	// Interactive input
	input := make([]byte, 1024)
	for {
		n, err := os.Stdin.Read(input)
		if err != nil || n == 0 {
			if err == io.EOF {
				break
			}
			continue
		}
		text := string(input[:n])
		text = text[:len(text)-1] // trim newline in Windows
		if len(text) == 0 {
			continue
		}
		if text == "exit" || text == "quit" {
			break
		}
		if mode == "alice" {
			alice.SendChat(bob.PeerID, text)
		} else {
			bob.SendChat(alice.PeerID, text)
		}
		fmt.Print("> ")
	}

	aliceTr.Close()
	bobTr.Close()
}
