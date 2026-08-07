package transport

import "time"

type Channel string

const (
	ChannelWiFi    Channel = "wifi"
	ChannelLoRaSF7 Channel = "lora_sf7"
	ChannelLoRaSF9 Channel = "lora_sf9"
	ChannelLoRaSF12 Channel = "lora_sf12"
)

type Packet struct {
	FromPeerID string
	Payload    []byte
	RSSI       float64
	SNR        float64
	Channel    Channel
	ReceivedAt time.Time
}

type NeighborInfo struct {
	PeerID   string
	RSSI     float64
	LastSeen time.Time
	Channel  Channel
}

type Transport interface {
	Send(peerID string, payload []byte) error
	Broadcast(payload []byte) error
	Recv() <-chan *Packet
	Neighbors() []NeighborInfo
	Close() error
}
