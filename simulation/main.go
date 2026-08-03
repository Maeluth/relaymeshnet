package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

var world *World

func main() {
	cfg := DefaultConfig()
	world = NewWorld(cfg)

	http.HandleFunc("/api/state", cors(handleState))
	http.HandleFunc("/api/config", cors(handleConfig))
	http.HandleFunc("/api/toggle-node", cors(handleToggleNode))
	http.HandleFunc("/api/speed", cors(handleSpeed))
	http.HandleFunc("/api/scenario/jam", cors(handleScenarioJam))
	http.HandleFunc("/api/scenario/partition", cors(handleScenarioPartition))
	http.HandleFunc("/api/scenario/sybil", cors(handleScenarioSybil))
	http.HandleFunc("/api/scenario/load", cors(handleScenarioLoad))
	http.HandleFunc("/api/scenario/daynight", cors(handleScenarioDayNight))
	http.HandleFunc("/api/export/csv", cors(handleExportCSV))
	http.HandleFunc("/api/sweep", cors(handleSweep))
	http.Handle("/", http.FileServer(http.Dir("ui")))

	var lastTickCount int
	var lastTime time.Time

	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		lastTime = time.Now()
		for range ticker.C {
			world.mu.RLock()
			speed := world.Speed
			world.mu.RUnlock()
			if speed > 0 {
				steps := int(float64(speed) * TicksPerSecond * 0.1)
				if steps < 1 { steps = 1 }
				world.RunSteps(steps)
			}
			now := time.Now()
			elapsed := now.Sub(lastTime).Seconds()
			if elapsed >= 5 {
				world.mu.Lock()
				world.TPS = int(float64(world.Tick-lastTickCount) / elapsed)
				world.mu.Unlock()
				lastTickCount = world.Tick
				lastTime = now
			}
		}
	}()

	fmt.Println("RelayMeshNet (RMN) Simulation: http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func cors(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == "OPTIONS" {
			return
		}
		h(w, r)
	}
}

func handleState(w http.ResponseWriter, r *http.Request) {
	state := worldToState(world)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(state)
}

var configMu sync.Mutex

func handleConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		configMu.Lock()
		defer configMu.Unlock()

		var cfg Config
		if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
			http.Error(w, "bad config", 400)
			return
		}

		forceReset := r.URL.Query().Get("reset") == "true"

		world.mu.RLock()
		gridChanged := cfg.GridWidth != world.Config.GridWidth ||
			cfg.GridHeight != world.Config.GridHeight ||
			cfg.CellWidth != world.Config.CellWidth ||
			cfg.CellHeight != world.Config.CellHeight ||
			cfg.NodesPerCell != world.Config.NodesPerCell
		world.mu.RUnlock()

		if gridChanged || forceReset {
			oldSpeed := world.Speed
			world = NewWorld(cfg)
			world.mu.Lock()
			world.Speed = oldSpeed
			world.mu.Unlock()
		} else {
			world.mu.Lock()
			world.Config.WiFiRange = cfg.WiFiRange
			world.Config.LoRaRange = cfg.LoRaRange
			world.Config.WallAtten = cfg.WallAtten
			world.Config.FloorAtten = cfg.FloorAtten
			world.Config.NodeUptime = cfg.NodeUptime
			world.Config.DefaultHops = cfg.DefaultHops
			world.Config.CreditBase = cfg.CreditBase
			world.Config.RelayReward = cfg.RelayReward
			world.Config.SendCost = cfg.SendCost
			world.Config.StorageReward = cfg.StorageReward
			world.Config.BurnRate = cfg.BurnRate
			world.Config.ConfirmThreshold = cfg.ConfirmThreshold
			world.Config.RelayChunkSize = cfg.RelayChunkSize
			world.mu.Unlock()
		}
	}
	state := worldToState(world)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(state)
}

func handleToggleNode(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		return
	}
	var req struct {
		X int `json:"x"`
		Y int `json:"y"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return
	}
	world.ToggleNode(req.X, req.Y)
	w.WriteHeader(200)
}

func handleSpeed(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		var req struct {
			Speed int `json:"speed"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err == nil {
			world.mu.Lock()
			world.Speed = req.Speed
			world.mu.Unlock()
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]int{"speed": world.Speed})
}

// === Scenario Handlers ===

func handleScenarioJam(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" { return }
	var req struct {
		Cells [][2]int `json:"cells"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil { return }
	world.SetJamming(req.Cells)
	w.WriteHeader(200)
}

func handleScenarioPartition(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" { return }
	var req struct {
		X1, Y1, X2, Y2 int  `json:"x1,y1,x2,y2"`
		Blocked         bool `json:"blocked"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil { return }
	world.Partition(req.X1, req.Y1, req.X2, req.Y2, req.Blocked)
	w.WriteHeader(200)
}

func handleScenarioSybil(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" { return }
	var req struct {
		Count int `json:"count"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil { return }
	world.SpawnSybil(req.Count)
	w.WriteHeader(200)
}

func handleScenarioLoad(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" { return }
	var req struct {
		NodeCount int `json:"nodeCount"`
		FileSize  int `json:"fileSize"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil { return }
	world.LoadTest(req.NodeCount, req.FileSize)
	w.WriteHeader(200)
}

func handleScenarioDayNight(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" { return }
	var req struct {
		OnlineProb float64 `json:"onlineProb"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil { return }
	world.SetDayNight(req.OnlineProb)
	w.WriteHeader(200)
}

type UIState struct {
	Tick       int        `json:"tick"`
	Speed      int        `json:"speed"`
	GridWidth  int        `json:"gridWidth"`
	GridHeight int        `json:"gridHeight"`
	CellWidth  float64    `json:"cellWidth"`
	CellHeight float64    `json:"cellHeight"`
	WiFiRange  float64    `json:"wifiRange"`
	LoRaRange  float64    `json:"loraRange"`
	Nodes      []UINode   `json:"nodes"`
	Messages   []UIMessage `json:"messages"`
	Events     []UIEvent  `json:"events"`
	Stats      UIStats    `json:"stats"`
}

type UINode struct {
	ID         string  `json:"id"`
	X          int     `json:"x"`
	Y          int     `json:"y"`
	Name       string  `json:"name"`
	Online     bool    `json:"online"`
	Profile    int     `json:"profile"`
	Balance    float64 `json:"balance"`
	Reputation float64 `json:"reputation"`
	Available  float64 `json:"available"`
	RelayOut   float64 `json:"relayOut"`
	SentCount  int     `json:"sentCount"`
	RecvCount  int     `json:"recvCount"`
}

type UIMessage struct {
	ID        string   `json:"id"`
	From      string   `json:"from"`
	To        string   `json:"to"`
	Path      []string `json:"path"`
	Remaining int      `json:"remaining"`
	Total     int      `json:"total"`
	Size      int      `json:"size"`
	MsgType   string   `json:"msgType"`
}

type UIEvent struct {
	Tick int    `json:"tick"`
	Type string `json:"type"`
	Msg  string `json:"msg"`
}

type UIStats struct {
	TotalSent      int     `json:"totalSent"`
	TotalReceived  int     `json:"totalReceived"`
	TotalFailed    int     `json:"totalFailed"`
	TotalRelayed   int     `json:"totalRelayed"`
	TotalRELAY     float64 `json:"totalRelay"`
	TotalCollisions int    `json:"totalCollisions"`
	CollisionRate  float64 `json:"collisionRate"`
	GiniBalance    float64 `json:"giniBalance"`
	GiniReputation float64 `json:"giniReputation"`
	JainFairness   float64 `json:"jainFairness"`
	OnlineNodes    int     `json:"onlineNodes"`
	ActiveMsgs     int     `json:"activeMsgs"`
	GreenCount     int     `json:"greenCount"`
	YellowCount    int     `json:"yellowCount"`
	RedCount       int     `json:"redCount"`
	TotalSupply    float64 `json:"totalSupply"`
	TotalTransfers  int     `json:"totalTransfers"`
	TPS             int     `json:"tps"`
	NetEmission     float64 `json:"netEmission"`
	TotalEmission   float64 `json:"totalEmission"`
	TotalBurn       float64 `json:"totalBurn"`
}

func worldToState(w *World) UIState {
	w.mu.RLock()
	defer w.mu.RUnlock()

	state := UIState{
		Tick:       w.Tick,
		Speed:      w.Speed,
		GridWidth:  w.Config.GridWidth,
		GridHeight: w.Config.GridHeight,
		CellWidth:  w.Config.CellWidth,
		CellHeight: w.Config.CellHeight,
		WiFiRange:  w.Config.WiFiRange,
		LoRaRange:  w.Config.LoRaRange,
		Stats: UIStats{
			TotalSent:      int(atomic.LoadInt64(&w.TotalSent)),
			TotalReceived:  int(atomic.LoadInt64(&w.TotalReceived)),
			TotalFailed:    int(atomic.LoadInt64(&w.TotalFailed)),
			TotalRelayed:   int(atomic.LoadInt64(&w.TotalRelayed)),
			TotalRELAY:     float64(atomic.LoadInt64(&w.TotalRELAY)) / 100.0,
			TotalCollisions: int(atomic.LoadInt64(&w.TotalCollisions)),
			CollisionRate:  w.CollisionRate(),
			GiniBalance:    w.GiniBalance(),
			GiniReputation: w.GiniReputation(),
			JainFairness:   w.JainFairness(),
			OnlineNodes:    len(w.OnlineNodes()),
			ActiveMsgs:     len(w.ActiveMessages),
			GreenCount:     w.CountByBalance(10, 1e9),
			YellowCount:    w.CountByBalance(-50, 10),
			RedCount:       w.CountByBalance(-1e9, -50),
			TotalSupply:    w.TotalSupply(),
			TotalTransfers: int(atomic.LoadInt64(&w.TotalTransfers)),
			TPS:            w.TPS,
			TotalEmission:  float64(atomic.LoadInt64(&w.TotalEmission)) / 100.0,
			TotalBurn:      float64(atomic.LoadInt64(&w.TotalBurn)) / 100.0,
			NetEmission:    (float64(atomic.LoadInt64(&w.TotalEmission)) - float64(atomic.LoadInt64(&w.TotalBurn))) / 100.0,
		},
	}

	for _, n := range w.AllNodes() {
		state.Nodes = append(state.Nodes, UINode{
			ID:         n.PeerID()[:8],
			X:          n.X,
			Y:          n.Y,
			Name:       n.Name,
			Online:     n.Status == StatusOnline,
			Profile:    int(n.Profile),
			Balance:    math.Round(n.Balance*100) / 100,
			Reputation: math.Round(n.Reputation*100) / 100,
			Available:  math.Round(n.Available()*100) / 100,
			RelayOut:   math.Round(n.RelayBytesOut/1024*100) / 100,
			SentCount:  n.SentCount,
			RecvCount:  n.ReceivedCount,
		})
	}

	for _, msg := range w.ActiveMessages {
		state.Messages = append(state.Messages, UIMessage{
			ID:        msg.ID,
			From:      msg.From[:8],
			To:        msg.To[:8],
			Path:      msg.Path,
			Remaining: msg.Remaining,
			Total:     msg.Total,
			Size:      msg.Size,
			MsgType:   msg.MsgType,
		})
	}

	start := 0
	if len(w.Events) > 50 {
		start = len(w.Events) - 50
	}
	for i := start; i < len(w.Events); i++ {
		e := w.Events[i]
		state.Events = append(state.Events, UIEvent{
			Tick: e.Tick,
			Type: e.Type,
			Msg:  e.Msg,
		})
	}

	return state
}

// === CSV Export ===

func handleExportCSV(w http.ResponseWriter, r *http.Request) {
	state := worldToState(world)
	s := state.Stats

	csv := "metric,value\n"
	csv += fmt.Sprintf("tick,%d\n", state.Tick)
	csv += fmt.Sprintf("totalSent,%d\n", s.TotalSent)
	csv += fmt.Sprintf("totalReceived,%d\n", s.TotalReceived)
	csv += fmt.Sprintf("totalFailed,%d\n", s.TotalFailed)
	csv += fmt.Sprintf("totalCollisions,%d\n", s.TotalCollisions)
	csv += fmt.Sprintf("collisionRate,%.4f\n", s.CollisionRate)
	csv += fmt.Sprintf("relayCount,%d\n", s.TotalRelayed)
	csv += fmt.Sprintf("relayVolume,%.0f\n", s.TotalRELAY)
	csv += fmt.Sprintf("giniBalance,%.4f\n", s.GiniBalance)
	csv += fmt.Sprintf("giniReputation,%.4f\n", s.GiniReputation)
	csv += fmt.Sprintf("jainFairness,%.4f\n", s.JainFairness)
	csv += fmt.Sprintf("totalSupply,%.0f\n", s.TotalSupply)
	csv += fmt.Sprintf("netEmission,%.1f\n", s.NetEmission)
	csv += fmt.Sprintf("totalEmission,%.1f\n", s.TotalEmission)
	csv += fmt.Sprintf("totalBurn,%.1f\n", s.TotalBurn)
	csv += fmt.Sprintf("totalTransfers,%d\n", s.TotalTransfers)
	csv += fmt.Sprintf("onlineNodes,%d\n", s.OnlineNodes)
	csv += fmt.Sprintf("greenNodes,%d\n", s.GreenCount)
	csv += fmt.Sprintf("yellowNodes,%d\n", s.YellowCount)
	csv += fmt.Sprintf("redNodes,%d\n", s.RedCount)
	csv += fmt.Sprintf("activeMsgs,%d\n", s.ActiveMsgs)
	csv += fmt.Sprintf("tps,%d\n", s.TPS)

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=rmn_simulation.csv")
	w.Write([]byte(csv))
}

// === Parameter Sweep ===

type SweepRequest struct {
	Iterations   int     `json:"iterations"`
	TicksPerRun  int     `json:"ticksPerRun"`
	ParamToSweep string  `json:"paramToSweep"` // "emissionRate", "burnRate", "nodes", etc.
	RangeMin     float64 `json:"rangeMin"`
	RangeMax     float64 `json:"rangeMax"`
	Steps        int     `json:"steps"`
}

type SweepResult struct {
	Values []SweepPoint `json:"values"`
}

type SweepPoint struct {
	ParamValue     float64 `json:"paramValue"`
	TotalSent      int     `json:"totalSent"`
	TotalReceived  int     `json:"totalReceived"`
	CollisionRate  float64 `json:"collisionRate"`
	GiniBalance    float64 `json:"giniBalance"`
	GiniReputation float64 `json:"giniReputation"`
	JainFairness   float64 `json:"jainFairness"`
	TotalSupply     float64 `json:"totalSupply"`
	NetEmission     float64 `json:"netEmission"`
	TotalTransfers  int     `json:"totalTransfers"`
	GreenCount     int     `json:"greenCount"`
	RedCount       int     `json:"redCount"`
}

func handleSweep(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		w.WriteHeader(405)
		return
	}
	var req SweepRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request: "+err.Error(), 400)
		return
	}
	if req.Iterations < 1 { req.Iterations = 1 }
	if req.TicksPerRun < 100 { req.TicksPerRun = 5000 }
	if req.Steps < 2 { req.Steps = 5 }

	results := runSweep(req)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(results)
}

func runSweep(req SweepRequest) SweepResult {
	var result SweepResult
	step := (req.RangeMax - req.RangeMin) / float64(req.Steps-1)

	for i := 0; i < req.Steps; i++ {
		paramVal := req.RangeMin + float64(i)*step
		var sentSum, recvSum, supplySum, transfersSum, emissionSum, burnSum int64
		var collSum, giniBSum, giniRSum, jainSum float64
		var greenSum, redSum int64

		for iter := 0; iter < req.Iterations; iter++ {
			cfg := DefaultConfig()
			// Применяем параметр
			switch req.ParamToSweep {
			case "emissionRate":
				cfg.EmissionRate = paramVal
			case "burnRate":
				cfg.BurnRate = paramVal
			case "nodes":
				cfg.GridWidth = int(paramVal)
				if cfg.GridWidth < 2 { cfg.GridWidth = 2 }
			case "gridWidth":
				cfg.GridWidth = int(paramVal)
				if cfg.GridWidth < 2 { cfg.GridWidth = 2 }
			case "gridHeight":
				cfg.GridHeight = int(paramVal)
				if cfg.GridHeight < 2 { cfg.GridHeight = 2 }
			case "wifiRange":
				cfg.WiFiRange = paramVal
			case "loraRange":
				cfg.LoRaRange = paramVal
			case "sendCost":
				cfg.SendCost = paramVal
			case "relayReward":
				cfg.RelayReward = paramVal
			case "defaultHops":
				cfg.DefaultHops = int(paramVal)
			default:
				cfg.EmissionRate = paramVal // default sweep
			}

			w2 := NewWorld(cfg)
			w2.RunSteps(req.TicksPerRun)

			sentSum += atomic.LoadInt64(&w2.TotalSent)
			recvSum += atomic.LoadInt64(&w2.TotalReceived)
			supplySum += int64(w2.TotalSupply())
			transfersSum += atomic.LoadInt64(&w2.TotalTransfers)
			emissionSum += atomic.LoadInt64(&w2.TotalEmission)
			burnSum += atomic.LoadInt64(&w2.TotalBurn)
			collSum += w2.CollisionRate()
			giniBSum += w2.GiniBalance()
			giniRSum += w2.GiniReputation()
			jainSum += w2.JainFairness()
			greenSum += int64(w2.CountByBalance(10, 1e9))
			redSum += int64(w2.CountByBalance(-1e9, -50))
		}

		n := float64(req.Iterations)
		result.Values = append(result.Values, SweepPoint{
			ParamValue:     paramVal,
			TotalSent:      int(float64(sentSum) / n),
			TotalReceived:  int(float64(recvSum) / n),
			CollisionRate:  collSum / n,
			GiniBalance:    giniBSum / n,
			GiniReputation: giniRSum / n,
			JainFairness:   jainSum / n,
			TotalSupply:    float64(supplySum) / n,
			NetEmission:    (float64(emissionSum) - float64(burnSum)) / n / 100.0,
			TotalTransfers: int(float64(transfersSum) / n),
			GreenCount:     int(float64(greenSum) / n),
			RedCount:       int(float64(redSum) / n),
		})
	}
	return result
}
