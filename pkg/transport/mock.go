package transport

import (
	"sync"
	"time"
)

type MockTransport struct {
	mu      sync.RWMutex
	peerID  string
	inbox   chan *Packet
	peers   map[string]*MockTransport
	handler func(peerID string, payload []byte)
}

func NewMockTransport(peerID string) *MockTransport {
	return &MockTransport{
		peerID: peerID,
		inbox:  make(chan *Packet, 256),
		peers:  make(map[string]*MockTransport),
	}
}

func (m *MockTransport) Connect(other *MockTransport) {
	m.mu.Lock()
	other.mu.Lock()
	m.peers[other.peerID] = other
	other.peers[m.peerID] = m
	other.mu.Unlock()
	m.mu.Unlock()
}

func (m *MockTransport) Disconnect(other *MockTransport) {
	m.mu.Lock()
	other.mu.Lock()
	delete(m.peers, other.peerID)
	delete(other.peers, m.peerID)
	other.mu.Unlock()
	m.mu.Unlock()
}

func (m *MockTransport) SetHandler(fn func(peerID string, payload []byte)) {
	m.handler = fn
}

func (m *MockTransport) Send(peerID string, payload []byte) error {
	m.mu.RLock()
	peer, ok := m.peers[peerID]
	m.mu.RUnlock()
	if !ok {
		return nil
	}
	packet := &Packet{
		FromPeerID: m.peerID,
		Payload:    append([]byte{}, payload...),
		RSSI:       -40,
		SNR:        20,
		Channel:    ChannelWiFi,
		ReceivedAt: time.Now(),
	}
	select {
	case peer.inbox <- packet:
		if peer.handler != nil {
			peer.handler(m.peerID, payload)
		}
	default:
	}
	return nil
}

func (m *MockTransport) Broadcast(payload []byte) error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, peer := range m.peers {
		m.Send(peer.peerID, payload)
	}
	return nil
}

func (m *MockTransport) Recv() <-chan *Packet {
	return m.inbox
}

func (m *MockTransport) Neighbors() []NeighborInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var n []NeighborInfo
	for _, p := range m.peers {
		n = append(n, NeighborInfo{
			PeerID:   p.peerID,
			RSSI:     -40,
			LastSeen: time.Now(),
			Channel:  ChannelWiFi,
		})
	}
	return n
}

func (m *MockTransport) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	close(m.inbox)
	for _, p := range m.peers {
		p.mu.Lock()
		delete(p.peers, m.peerID)
		p.mu.Unlock()
	}
	m.peers = make(map[string]*MockTransport)
	return nil
}
