package main

import (
	"math"
)

type RadioType int

const (
	RadioWiFi RadioType = iota
	RadioLoRa
)

type RadioModel struct {
	TxPower   float64
	Frequency float64 // MHz
	Sensitivity float64 // dBm, минимальный SNR для приёма
}

func NewWiFiRadio() RadioModel {
	return RadioModel{
		TxPower:     20.0,
		Frequency:   2400.0,
		Sensitivity: -90.0,
	}
}

func NewLoRaRadio() RadioModel {
	return RadioModel{
		TxPower:     14.0,
		Frequency:   868.0,
		Sensitivity: -137.0,
	}
}

func (r RadioModel) PathLoss(distanceKm float64) float64 {
	if distanceKm < 0.001 {
		distanceKm = 0.001
	}
	return 20*math.Log10(distanceKm) + 20*math.Log10(r.Frequency) + 32.45
}

func WallLoss(wallsBetween, floorsBetween int, wallAtten, floorAtten float64) float64 {
	return float64(wallsBetween)*wallAtten + float64(floorsBetween)*floorAtten
}

func (r RadioModel) SNR(distanceMeters float64, wallsBetween, floorsBetween int, wallAtten, floorAtten, noiseFloor float64) float64 {
	distanceKm := distanceMeters / 1000.0
	pathLoss := r.PathLoss(distanceKm)
	wallLoss := WallLoss(wallsBetween, floorsBetween, wallAtten, floorAtten)
	rxPower := r.TxPower - pathLoss - wallLoss
	return rxPower - noiseFloor
}

func PacketSuccessRate(snr float64) float64 {
	k := 0.5
	x0 := 3.0
	return 1.0 / (1.0 + math.Exp(-k*(snr-x0)))
}

type LoRaSF int

const (
	SF7  LoRaSF = 7
	SF9  LoRaSF = 9
	SF12 LoRaSF = 12
)

func (sf LoRaSF) Bitrate() float64 {
	switch sf {
	case SF7:
		return 5470.0
	case SF9:
		return 1760.0
	case SF12:
		return 293.0
	}
	return 293.0
}

func (sf LoRaSF) MaxPayload() int {
	switch sf {
	case SF7:
		return 222
	case SF9:
		return 115
	case SF12:
		return 51
	}
	return 51
}

func (sf LoRaSF) AirTime(bytes int) float64 {
	payloadSymbNb := 8.0 + math.Max(math.Ceil((8.0*float64(bytes)-4.0*float64(sf)+28.0)/float64(4*(sf-2)))*float64(5)/4, 0)
	symbolDuration := math.Pow(2, float64(sf)) / 125000.0
	return payloadSymbNb * symbolDuration
}

func FragmentsNeeded(packetSize int, sf LoRaSF) int {
	maxPayload := sf.MaxPayload()
	if packetSize <= maxPayload {
		return 1
	}
	return (packetSize + maxPayload - 1) / maxPayload
}

func DutyCycleDelay(airTime float64, dutyCycle float64) float64 {
	return airTime / dutyCycle
}
