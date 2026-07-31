# RelayMeshNet (RMN)

**Decentralized mesh network with built-in tokenomics — designed to work without internet.**

[![Architecture](https://img.shields.io/badge/modules-29-blue)]()
[![Simulation](https://img.shields.io/badge/simulation-Go-green)]()
[![Stage](https://img.shields.io/badge/stage-design%20%2B%20simulation-orange)]()

## What is RMN?

A self-organizing mesh network where every router becomes a relay node. Nodes communicate via LoRa (long-range radio) and WiFi, forming a resilient topology without internet access. Built-in onion routing provides anonymity. A mutual credit economy incentivizes relay participation.

**Think of it as: Tor + Helium + BitTorrent — but local, offline, and self-sustaining.**

## Architecture

```
┌─────────────────────────────────────────────┐
│  L5: Apps — Chat, files, web services, SDK │
├─────────────────────────────────────────────┤
│  L4: Services — DHT, service discovery     │
├─────────────────────────────────────────────┤
│  L3: Content — Addressed storage, erasure   │
├─────────────────────────────────────────────┤
│  L2: Routing — Onion routing, DHT, PoH     │
├─────────────────────────────────────────────┤
│  L1: Transport — LoRa, WiFi, B.A.T.M.A.N.  │
└─────────────────────────────────────────────┘
```

## Key Features

- **Offline-first** — Works without internet. Nodes discover each other via LoRa/WiFi
- **Onion routing** — Every message goes through at least 1 relay hop (like Tor)
- **E2E encryption** — X3DH + Double Ratchet (Signal Protocol) for forward secrecy
- **Content-addressed storage** — Files identified by hash, distributed with erasure coding
- **Self-sovereign identity** — `PeerID = SHA256(pubkey)`, no central authority
- **Mutual credit economy** — Earn by relaying, spend by sending. No blockchain needed
- **Proof of History** — SHA256 chain as trustless timestamp (from Solana)
- **Cover traffic** — Mandatory Poisson-process traffic masks real activity (Nym model)

## Economy (Tokenomics)

| Mechanism | Description |
|-----------|-------------|
| **Confirm-N** | To send a message, relay N others' packets. Earns emission + reputation |
| **RELAY credits** | For files > 2KB. Sender pays, relays earn. 1% burn per transaction |
| **Reputation demurrage** | ~1% monthly decay (×2 when offline). Prevents reputation hoarding |
| **Transfers** | Credits flow from relay-rich to relay-poor nodes naturally |

No premine. No ICO. No blockchain. Credits emerge from relay work.

## Simulation

Go-based simulation with WebUI that models:
- Radio physics (Friis path loss, SNR, LoRa spreading factors SF7-SF12)
- LoRa duty cycle (1%) and fragmentation
- Onion routing with multi-hop paths
- Complete economy (emission, burn, confirm-N, transfers, reputation)
- 500+ nodes on a configurable grid

```bash
cd simulation
go run .
# Open http://localhost:8080
```

## Documentation

23 modules covering every layer:

| # | Module |
|---|--------|
| 01 | Transport (LoRa, WiFi, B.A.T.M.A.N., SNR-based flooding) |
| 02 | Onion routing (circuit building, cover traffic, exponential jitter) |
| 03 | DHT & service discovery (S/Kademlia, self-certifying names) |
| 04 | Content storage (erasure coding, BitTorrent-like distribution) |
| 05 | Tokenomics (mutual credit, emission, burn, reputation) |
| 06 | Proof of History (trustless timestamp, checkpoint storage) |
| 07 | Bridge (external world, mailbox protocol, key rotation) |
| 08 | Threat model (9 attack vectors, defense in depth) |
| 09 | SDK (Go API for developers) |
| 10 | Pending queue (offline transactions, FIFO, TTL) |
| 11 | Confirm-N economy (two-tier: free + paid) |
| 12 | Bootstrapping (first contact, verification, anti-fake) |
| 13 | Hybrid economy (emission + burn + mandatory relay) |
| 14 | Web services (CDN layer, HTTP proxy, static/dynamic routing) |
| 15 | Devices & nodes (three participant types, hosting) |
| 16 | Crypto & identity (X3DH, Double Ratchet, trust levels) |
| 17 | Protocol spec (wire format, onion packet, DHT RPC, state machine) |
| 18 | Versioning & compatibility (cap negotiation, 3 profiles) |
| 19 | Link verification (bidirectional check, node classes) |
| 20 | Economic attacks (Sybil reset, self-mining, cross-network migration) |
| 21 | Hardware design (3 device models, modularity, roadmap) |
| 22 | Deployment params (frequencies, EIRP, LoRa range, cross-compilation) |
| 23 | Firmware ecosystem (multi-developer, Ed25519 signing, BitTorrent distribution) |
| 24 | QoS & Multi-path (TCP/UDP modes, WiFi+LoRa striping, LoRa budget, compression) |
| 25 | Node lifecycle (BOOTSTRAP → SANDBOX → RAMP-UP → ACTIVE → TRUSTED) |
| 26 | Identity export/import (encrypted backup, cross-device sync, physical transfer) |
| 27 | Cold restart (recovery phases, partial/full restart, fresh network bootstrap) |
| 28 | Sovereignty & clans (social trust layer, white/grey/black zones, ban lists) |

## Status

**Pre-MVP.** Architecture and simulation are complete. Physical prototype targeted for early 2027.

## Author

Maeluth — system architecture, protocol design, simulation.

## License

Open source. See individual modules for details.
