package main

import (
	"fmt"
	"time"

	"github.com/maeluth/relaymeshnet/pkg/crypto"
	"github.com/maeluth/relaymeshnet/pkg/dht"
	"github.com/maeluth/relaymeshnet/pkg/economy"
	"github.com/maeluth/relaymeshnet/pkg/identity"
	"github.com/maeluth/relaymeshnet/pkg/onion"
	"github.com/maeluth/relaymeshnet/pkg/poh"
	"github.com/maeluth/relaymeshnet/pkg/protocol"
	"github.com/maeluth/relaymeshnet/pkg/relay"
	"github.com/maeluth/relaymeshnet/pkg/transport"
)

func main() {
	fmt.Println("=== RelayMeshNet Production Code Demo ===")
	fmt.Println()

	// 1. Identity & Crypto
	fmt.Println("1. Generating identities...")
	alice, _ := crypto.GenerateIdentity()
	bob, _ := crypto.GenerateIdentity()

	aliceID := identity.PeerID(alice.PubKey)
	bobID := identity.PeerID(bob.PubKey)

	fmt.Printf("   Alice: %s\n", aliceID[:16])
	fmt.Printf("   Bob:   %s\n\n", bobID[:16])

	// 2. Proof of History
	fmt.Println("2. Starting PoH generators...")
	alicePoH := poh.NewGenerator(alice.DHKey)
	bobPoH := poh.NewGenerator(bob.DHKey)

	for i := 0; i < 100; i++ {
		alicePoH.Tick()
		bobPoH.Tick()
	}

	fmt.Printf("   Alice PoH tick: %d\n", alicePoH.CurrentTick())
	fmt.Printf("   Bob PoH tick:   %d\n\n", bobPoH.CurrentTick())

	// 3. DHT
	fmt.Println("3. Building DHT routing table...")
	aliceDHT := dht.NewTable(dht.KeyFromPeerID(aliceID))
	bobDHT := dht.NewTable(dht.KeyFromPeerID(bobID))

	bobNode := &dht.Node{
		ID:       dht.KeyFromPeerID(bobID),
		PeerID:   bobID,
		Address:  "192.168.1.100:8080",
		LastSeen: time.Now(),
	}
	aliceDHT.Add(bobNode)

	aliceNode := &dht.Node{
		ID:       dht.KeyFromPeerID(aliceID),
		PeerID:   aliceID,
		Address:  "192.168.1.101:8080",
		LastSeen: time.Now(),
	}
	bobDHT.Add(aliceNode)

	fmt.Printf("   Alice DHT: %d nodes\n", aliceDHT.Count())
	fmt.Printf("   Bob DHT:   %d nodes\n\n", bobDHT.Count())

	// 4. Economy
	fmt.Println("4. Testing economy...")
	aliceBalance := economy.NewBalance()
	bobBalance := economy.NewBalance()

	aliceReputation := economy.NewReputation()
	_ = economy.NewReputation() // bobReputation for future use

	// Alice does some relay work
	aliceReputation.AddWork(10000)
	aliceBalance.Add(economy.CalculateEmission(5))

	// Bob sends a message (pays cost)
	msgBytes := 500
	hops := 3
	cost := economy.CalculateSendCost(msgBytes, hops)
	burn := economy.CalculateBurn(cost)

	bobBalance.Subtract(cost)
	aliceBalance.Add(cost - burn) // Alice relays

	fmt.Printf("   Alice balance: %.2f RELAY\n", aliceBalance.Total())
	fmt.Printf("   Bob balance:   %.2f RELAY\n", bobBalance.Total())
	fmt.Printf("   Alice reputation: %.2f\n", aliceReputation.Score())
	fmt.Printf("   Alice credit limit: %.2f RELAY\n\n", aliceReputation.CreditLimit())

	// 5. Protocol
	fmt.Println("5. Building protocol frame...")
	frame := &protocol.Frame{
		Version: 1,
		Type:    protocol.MsgChat,
		Payload: []byte("Hello Bob!"),
	}

	data, _ := frame.Marshal()
	fmt.Printf("   Frame size: %d bytes\n", len(data))

	decoded, _ := protocol.Unmarshal(data)
	fmt.Printf("   Decoded: type=%d, payload=%q\n\n", decoded.Type, decoded.Payload)

	// 6. Onion Routing
	fmt.Println("6. Building onion packet...")
	hops2 := []onion.Hop{
		{PeerID: [32]byte{1}, PubKey: alice.DHPub},
		{PeerID: [32]byte{2}, PubKey: bob.DHPub},
	}

	onionPkt, err := onion.BuildCircuit([]byte("Secret message"), hops2, alice.DHKey)
	if err != nil {
		fmt.Printf("   Error: %v\n", err)
	} else {
		fmt.Printf("   Onion packet size: %d bytes\n", len(onionPkt.Data))
		fmt.Printf("   Expected size: %d bytes\n\n", onion.OnionSize(14, 2))
	}

	// 7. Relay Queue
	fmt.Println("7. Testing relay queue...")
	queue := relay.NewQueue(100)

	pkt1 := &relay.Packet{
		From:     aliceID,
		To:       bobID,
		Data:     []byte("Message 1"),
		Priority: relay.PriorityNormal,
	}
	pkt2 := &relay.Packet{
		From:     bobID,
		To:       aliceID,
		Data:     []byte("Message 2"),
		Priority: relay.PriorityHigh,
	}

	queue.Push(pkt1)
	queue.Push(pkt2)

	fmt.Printf("   Queue size: %d\n", queue.Len())

	popped := queue.Pop()
	fmt.Printf("   Popped: priority=%d (high=0, normal=1)\n\n", popped.Priority)

	// 8. Transport
	fmt.Println("8. Testing transport...")
	aliceTransport := transport.NewMockTransport(aliceID)
	bobTransport := transport.NewMockTransport(bobID)

	aliceTransport.Connect(bobTransport)

	go func() {
		for pkt := range bobTransport.Recv() {
			fmt.Printf("   Bob received: %q from %s\n", pkt.Payload, pkt.FromPeerID[:16])
		}
	}()

	aliceTransport.Send(bobID, []byte("Hello via transport!"))
	time.Sleep(100 * time.Millisecond)

	fmt.Println()
	fmt.Println("=== Demo Complete ===")
	fmt.Println()
	fmt.Println("Production packages:")
	fmt.Println("  ✓ pkg/crypto     - Ed25519, X3DH, Double Ratchet")
	fmt.Println("  ✓ pkg/identity   - PeerID generation")
	fmt.Println("  ✓ pkg/protocol   - Wire format, message types")
	fmt.Println("  ✓ pkg/onion      - Onion routing")
	fmt.Println("  ✓ pkg/dht        - Distributed hash table")
	fmt.Println("  ✓ pkg/poh        - Proof of History")
	fmt.Println("  ✓ pkg/economy    - Tokenomics, reputation")
	fmt.Println("  ✓ pkg/relay      - Relay queue, hop tracking")
	fmt.Println("  ✓ pkg/transport  - Transport interface + mock")
}
