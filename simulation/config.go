package main

const TicksPerSecond = 500.0 / 60.0  // ~8.33 ticks/sec
const SecondsPerTick = 60.0 / 500.0   // 0.12 seconds

type Config struct {
	GridWidth   int
	GridHeight  int
	CellWidth   float64
	CellHeight  float64

	NodesPerCell   int
	NodeUptime     float64
	JammingEnabled bool
	JammingCells   [][2]int

	WiFiRange   float64
	LoRaRange   float64
	WiFiTXPower float64
	LoRaTXPower float64
	NoiseFloor  float64
	WallAtten   float64
	FloorAtten  float64

	DefaultHops int

	CreditBase       float64
	RelayReward      float64
	SendCost         float64
	StorageReward    float64
	EmissionRate     float64
	BurnRate         float64
	MinRelayHops     int
	MaxRelayHops     int
	ConfirmThreshold int
	RelayChunkSize   int
}

func DefaultConfig() Config {
	return Config{
		GridWidth:   40,
		GridHeight:  10,
		CellWidth:   20.0,
		CellHeight:  4.0,

		NodesPerCell:   1,
		NodeUptime:     0.95,
		JammingEnabled: false,
		JammingCells:   [][2]int{},

		WiFiRange:   30.0,
		LoRaRange:   75.0,
		WiFiTXPower: 20.0,
		LoRaTXPower: 14.0,
		NoiseFloor:  -95.0,
		WallAtten:   10.0,
		FloorAtten:  15.0,

		DefaultHops: 3,

		CreditBase:       0,
		RelayReward:      1.0,
		SendCost:         1.0,
		StorageReward:    0.01,
		EmissionRate:     0.5,
		BurnRate:         0.01,
		MinRelayHops:     1,
		MaxRelayHops:     3,
		ConfirmThreshold: 2048,
		RelayChunkSize:   512,
	}
}
