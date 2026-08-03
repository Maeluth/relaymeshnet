const canvas = document.getElementById('canvas');
const ctx = canvas.getContext('2d');
const tooltip = document.getElementById('tooltip');

let state = null;
let configTimer = null;
let history = [];
let flowHistory = [];
let sysHistory = [];
let supplyHistory = [];
let giniHistory = [];
let lastGraphTick = 0;
let prevStats = null;

const sliderKeys = ['speed','gridWidth','gridHeight','wifiRange','loraRange','cellWidth','cellHeight','wallAtten','floorAtten'];

const sliders = {
  speed:     {el:document.getElementById('speed'),     val:document.getElementById('speedVal'),    fmt:v=>v+'x',   def:'10'},
  gridWidth: {el:document.getElementById('gridWidth'), val:document.getElementById('gwVal'),      fmt:v=>v,       def:'50'},
  gridHeight:{el:document.getElementById('gridHeight'),val:document.getElementById('ghVal'),      fmt:v=>v,       def:'10'},
  wifiRange: {el:document.getElementById('wifiRange'), val:document.getElementById('wifiVal'),    fmt:v=>v+'м',   def:'30'},
  loraRange: {el:document.getElementById('loraRange'), val:document.getElementById('loraVal'),    fmt:v=>v+'м',   def:'5000'},
  cellWidth: {el:document.getElementById('cellWidth'), val:document.getElementById('cwVal'),     fmt:v=>v+'м',   def:'20'},
  cellHeight:{el:document.getElementById('cellHeight'),val:document.getElementById('chVal'),     fmt:v=>v+'м',   def:'4'},
  wallAtten: {el:document.getElementById('wallAtten'), val:document.getElementById('waVal'),     fmt:v=>v+'dB',  def:'10'},
  floorAtten:{el:document.getElementById('floorAtten'),val:document.getElementById('faVal'),     fmt:v=>v+'dB',  def:'15'},
};

// Восстановление значений из localStorage
function loadSettings() {
  const saved = JSON.parse(localStorage.getItem('rmn-sim-settings') || '{}');
  Object.entries(sliders).forEach(([key, s]) => {
    const val = saved[key] || s.def;
    s.el.value = val;
    s.val.textContent = s.fmt(val);
  });
  setSpeed(parseInt(sliders.speed.el.value, 10));
}
function saveSettings() {
  const obj = {};
  sliderKeys.forEach(k => obj[k] = sliders[k].el.value);
  localStorage.setItem('rmn-sim-settings', JSON.stringify(obj));
}

loadSettings();

Object.entries(sliders).forEach(([key, s]) => {
  s.el.addEventListener('input', () => {
    s.val.textContent = s.fmt(s.el.value);
    saveSettings();
    if (key === 'speed') setSpeed(parseInt(s.el.value));
    else scheduleConfigUpdate();
  });
});

function scheduleConfigUpdate() {
  clearTimeout(configTimer);
  configTimer = setTimeout(applyConfigLive, 400);
}

function buildConfig() {
  return {
    gridWidth:parseInt(sliders.gridWidth.el.value),
    gridHeight:parseInt(sliders.gridHeight.el.value),
    cellWidth:parseInt(sliders.cellWidth.el.value),
    cellHeight:parseInt(sliders.cellHeight.el.value),
    wifiRange:parseInt(sliders.wifiRange.el.value),
    loraRange:parseInt(sliders.loraRange.el.value),
    nodesPerCell:1, nodeUptime:0.95,
    jammingEnabled:false, jammingCells:[],
    wifiTxPower:20, loraTxPower:14, noiseFloor:-95,
    wallAtten:parseInt(sliders.wallAtten.el.value),
    floorAtten:parseInt(sliders.floorAtten.el.value),
    defaultHops:3,
    creditBase:0, relayReward:1, sendCost:1, storageReward:0.01, emissionRate:0.1, burnRate:0.01,
    confirmThreshold:2048, relayChunkSize:512,
  };
}

function applyConfigLive() {
  fetch('/api/config', {method:'POST', headers:{'Content-Type':'application/json'}, body:JSON.stringify(buildConfig())});
}

async function setSpeed(v) { await fetch('/api/speed',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({speed:v})}); }
async function resetSim() {
  await fetch('/api/config?reset=true', {method:'POST', headers:{'Content-Type':'application/json'}, body:JSON.stringify(buildConfig())});
}

function resize() { canvas.width = canvas.parentElement.clientWidth; canvas.height = canvas.parentElement.clientHeight; }
window.addEventListener('resize', () => { resize(); });

// --- Rendering ---

function nodeColor(n) {
  if (!n.online) return '#484f58';
  if (n.balance > 10) return '#3fb950';
  if (n.balance < -50) return '#f85149';
  if (n.balance < 0) return '#f0883e';
  return '#d29922';
}

function nodeRadius(n) {
  if (!n.online) return 2.5;
  return 3 + Math.min(n.reputation, 15) * 0.3;
}

function nodePos(n) {
  // Нормализуем: клетки всегда квадратные на экране, масштабируем под высоту
  const cols = state.gridWidth;
  const rows = state.gridHeight;
  const pad = 30;
  const availW = canvas.width - pad * 2;
  const availH = canvas.height - pad * 2;
  // Размер клетки = min(ширина/колонки, высота/строки), чтобы все влезло
  const cellSize = Math.min(availW / cols, availH / rows);
  // Центрируем сетку
  const gridW = cellSize * cols;
  const gridH = cellSize * rows;
  const offsetX = pad + (availW - gridW) / 2;
  const offsetY = pad + (availH - gridH) / 2;

  return {
    x: offsetX + n.x * cellSize + cellSize / 2,
    y: offsetY + (rows - 1 - n.y) * cellSize + cellSize / 2,
  };
}

function render() {
  if (!state) return;
  const W = canvas.width, H = canvas.height;
  ctx.clearRect(0, 0, W, H);
  ctx.fillStyle = '#0d1117';
  ctx.fillRect(0, 0, W, H);

  const pad = 35, gw = W-pad*2, gh = H-pad*2;

  // Этажные линии
  ctx.strokeStyle = '#161b22'; ctx.lineWidth = 0.5;
  for (let y = 0; y <= state.gridHeight; y++) {
    const py = pad + gh - (y/state.gridHeight)*gh;
    ctx.beginPath(); ctx.moveTo(pad, py); ctx.lineTo(pad+gw, py); ctx.stroke();
  }
  // Подписи этажей
  ctx.fillStyle = '#30363d'; ctx.font = '9px monospace';
  for (let y = 0; y < state.gridHeight; y++) {
    const py = pad + gh - (y/state.gridHeight)*gh - gh/state.gridHeight/2;
    ctx.fillText('эт.'+(y+1), 2, py+3);
  }

  // Связи
  const online = state.nodes.filter(n=>n.online);
  for (let i = 0; i < online.length; i++) {
    for (let j = i+1; j < online.length; j++) {
      const a = online[i], b = online[j];
      const dcol = Math.abs(a.x-b.x), drow = Math.abs(a.y-b.y);
      if (dcol > 2 || drow > 2) continue;
      const dx = dcol*state.cellWidth, dy = drow*state.cellHeight;
      const dist = Math.sqrt(dx*dx+dy*dy);
      if (dist <= state.wifiRange) {
        const pa = nodePos(a), pb = nodePos(b);
        ctx.beginPath(); ctx.moveTo(pa.x, pa.y); ctx.lineTo(pb.x, pb.y);
        ctx.strokeStyle = 'rgba(88,166,255,0.1)'; ctx.lineWidth = 0.5; ctx.stroke();
      } else if (dist <= (state.loraRange||5000) && (dcol>1 || drow>0)) {
        const pa = nodePos(a), pb = nodePos(b);
        ctx.beginPath(); ctx.moveTo(pa.x, pa.y); ctx.lineTo(pb.x, pb.y);
        ctx.strokeStyle = 'rgba(240,136,62,0.08)'; ctx.lineWidth = 0.5;
        ctx.setLineDash([2, 4]); ctx.stroke(); ctx.setLineDash([]);
      }
    }
  }

  // Сообщения — анимированные с полным путём
  for (const msg of state.messages) {
    const pathNodes = [];
    for (const pid of msg.path) {
      const n = state.nodes.find(nn => pid.startsWith(nn.id));
      if (n) pathNodes.push(n);
    }
    if (pathNodes.length < 2) continue;

    const progress = msg.total > 0 ? 1 - msg.remaining / msg.total : 1;
    const isFile = msg.msgType === 'file';

    // Строим полный путь как ломаную
    const points = pathNodes.map(n => nodePos(n));
    const totalLen = [];
    let sum = 0;
    for (let i = 1; i < points.length; i++) {
      const dx = points[i].x - points[i-1].x;
      const dy = points[i].y - points[i-1].y;
      const seg = Math.sqrt(dx*dx + dy*dy);
      totalLen.push(seg);
      sum += seg;
    }

    // Отрисовка ломаной линии
    ctx.beginPath();
    ctx.moveTo(points[0].x, points[0].y);
    for (let i = 1; i < points.length; i++) {
      ctx.lineTo(points[i].x, points[i].y);
    }
    const lineCol = isFile ? 'rgba(248,81,73,0.2)' : 'rgba(247,119,186,0.15)';
    ctx.strokeStyle = lineCol;
    ctx.lineWidth = isFile ? 1 : 0.7;
    ctx.setLineDash(isFile ? [4, 3] : [3, 6]);
    ctx.stroke();
    ctx.setLineDash([]);

    // Рисуем relay-узлы на пути (маленькие точки)
    for (let i = 1; i < points.length - 1; i++) {
      ctx.beginPath();
      ctx.arc(points[i].x, points[i].y, 2, 0, Math.PI*2);
      ctx.fillStyle = 'rgba(240,136,62,0.5)';
      ctx.fill();
    }

    // Позиция точки вдоль ломаной
    const targetDist = progress * sum;
    let acc = 0, mx = points[0].x, my = points[0].y;
    for (let i = 0; i < totalLen.length; i++) {
      if (acc + totalLen[i] >= targetDist) {
        const t = (targetDist - acc) / totalLen[i];
        mx = points[i].x + (points[i+1].x - points[i].x) * t;
        my = points[i].y + (points[i+1].y - points[i].y) * t;
        break;
      }
      acc += totalLen[i];
    }

    const dotR = isFile ? 5 : 3.5;
    const glow = ctx.createRadialGradient(mx, my, 0, mx, my, dotR*2);
    const dc = isFile ? '248,81,73' : '247,119,186';
    glow.addColorStop(0, `rgba(${dc},0.6)`);
    glow.addColorStop(1, `rgba(${dc},0)`);
    ctx.beginPath(); ctx.arc(mx, my, dotR*2, 0, Math.PI*2);
    ctx.fillStyle = glow; ctx.fill();

    ctx.beginPath(); ctx.arc(mx, my, dotR, 0, Math.PI*2);
    ctx.fillStyle = isFile ? '#f85149' : '#f778ba';
    ctx.fill();

    if (isFile) {
      ctx.fillStyle = '#f85149'; ctx.font = '8px monospace';
      ctx.fillText(Math.round(progress*100)+'%', mx+8, my-8);
    }
  }

  // Узлы
  for (const n of state.nodes) {
    const pos = nodePos(n);
    const r = nodeRadius(n);

    if (n.online && n.reputation > 0.5) {
      ctx.beginPath(); ctx.arc(pos.x, pos.y, r+3, 0, Math.PI*2);
      ctx.strokeStyle = `rgba(88,166,255,${Math.min(n.reputation/15,0.6)})`;
      ctx.lineWidth = 1; ctx.stroke();
    }
    if (n.online && n.relayOut > 3) {
      ctx.beginPath(); ctx.arc(pos.x, pos.y, r+6, 0, Math.PI*2);
      ctx.strokeStyle = `rgba(63,185,80,${Math.min(n.relayOut/30,0.5)})`;
      ctx.lineWidth = 1.5; ctx.stroke();
    }

    ctx.beginPath(); ctx.arc(pos.x, pos.y, r, 0, Math.PI*2);
    ctx.fillStyle = nodeColor(n); ctx.fill();
    if (!n.online) { ctx.strokeStyle = '#30363d'; ctx.lineWidth = 1; ctx.stroke(); }
  }

  // События мыши
  canvas.onclick = (e) => {
    const rect = canvas.getBoundingClientRect();
    const mx = e.clientX-rect.left, my = e.clientY-rect.top;
    let best=null, bd=20;
    for(const n of state.nodes){const p=nodePos(n);const d=Math.sqrt((mx-p.x)**2+(my-p.y)**2);if(d<bd){bd=d;best=n;}}
    if(best) toggleNode(best.x,best.y);
  };

  canvas.onmousemove = (e) => {
    const rect = canvas.getBoundingClientRect();
    const mx = e.clientX-rect.left, my = e.clientY-rect.top;
    let best=null, bd=16;
    for(const n of state.nodes){const p=nodePos(n);const d=Math.sqrt((mx-p.x)**2+(my-p.y)**2);if(d<bd){bd=d;best=n;}}
    if(best){
      tooltip.style.display='block';
      tooltip.style.left=Math.min(e.clientX+15,window.innerWidth-220)+'px';
      tooltip.style.top=(e.clientY-110)+'px';
      const prof = ['Chatter','Normal','Lurker','Relay','Unstable'][best.profile]||'?';
      tooltip.innerHTML =
        best.name+' ['+prof+']\n'+
        'ID: '+best.id+'\n'+
        'Баланс: '+best.balance.toFixed(1)+' | Доступно: '+best.available.toFixed(1)+'\n'+
        'Репутация: '+best.reputation.toFixed(2)+' | Relay: '+best.relayOut+' KB\n'+
        'Отпр: '+best.sentCount+' | Получ: '+best.recvCount;
    } else { tooltip.style.display='none'; }
  };
}

function updateStats() {
  if(!state)return;
  const s=state.stats;
  const done = s.totalReceived + s.totalFailed;
  const rate = done > 0 ? ((s.totalReceived/done)*100).toFixed(1) : '100';
  const mins = Math.floor(state.tick*0.6/60);
  const hrs = Math.floor(mins/60);
  const timeStr = hrs > 0 ? hrs+'ч '+mins%60+'м' : mins+'м';
  document.getElementById('stats').innerHTML =
    `<div class="stat"><span class="l">Тик</span><span class="v">${state.tick} (${s.tps} tps)</span></div>`+
    `<div class="stat"><span class="l">Время</span><span class="v">${timeStr}</span></div>`+
    `<div class="stat"><span class="l">Онлайн</span><span class="v g">${s.onlineNodes}/${state.nodes.length}</span></div>`+
    `<div class="stat"><span class="l" style="color:#3fb950">■ Зелёных</span><span class="v g">${s.greenCount}</span></div>`+
    `<div class="stat"><span class="l" style="color:#d29922">■ Жёлтых</span><span class="v y">${s.yellowCount}</span></div>`+
    `<div class="stat"><span class="l" style="color:#f85149">■ Красных</span><span class="v r">${s.redCount}</span></div>`+
    `<div class="stat"><span class="l">Отправлено</span><span class="v g">${s.totalSent}</span></div>`+
    `<div class="stat"><span class="l">Доставлено</span><span class="v g">${s.totalReceived}</span></div>`+
    `<div class="stat"><span class="l">Потеряно</span><span class="v r">${s.totalFailed}</span></div>`+
    `<div class="stat"><span class="l">Коллизий</span><span class="v" style="color:#f778ba">${(s.totalCollisions||0)} (${((s.collisionRate||0)*100).toFixed(1)}%)</span></div>`+
    `<div class="stat"><span class="l">Успешность</span><span class="v" style="color:${rate>90?'#3fb950':rate>70?'#d29922':'#f85149'}">${rate}%</span></div>`+
    `<div class="stat"><span class="l">Relay (confirm-N)</span><span class="v">${s.totalRelayed}</span></div>`+
    `<div class="stat"><span class="l">RELAY в обороте</span><span class="v y">${s.totalRelay.toFixed(0)}</span></div>`+
    `<div class="stat"><span class="l">В пути</span><span class="v" style="color:#f778ba">${s.activeMsgs}</span></div>`+
    `<div class="stat"><span class="l">Supply</span><span class="v y">${(s.totalSupply||0).toFixed(0)}</span></div>`+
    `<div class="stat"><span class="l">Net эмиссия</span><span class="v" style="color:${(s.netEmission||0)>0?'#3fb950':'#f85149'}">${(s.netEmission||0).toFixed(1)}</span></div>`+
    `<div class="stat"><span class="l">Emission / Burn</span><span class="v" style="font-size:9px">+${(s.totalEmission||0).toFixed(1)} / -${(s.totalBurn||0).toFixed(1)}</span></div>`+
    `<div class="stat"><span class="l">Трансферов</span><span class="v" style="color:#58a6ff">${(s.totalTransfers||0)}</span></div>`+
    `<div class="stat"><span class="l">Gini баланса</span><span class="v" style="color:${(s.giniBalance||0)<0.5?'#3fb950':'#f85149'}">${(s.giniBalance||0).toFixed(3)}</span></div>`+
    `<div class="stat"><span class="l">Gini репутации</span><span class="v" style="color:${(s.giniReputation||0)<0.5?'#3fb950':'#f85149'}">${(s.giniReputation||0).toFixed(3)}</span></div>`+
    `<div class="stat"><span class="l">Jain fairness</span><span class="v" style="color:${(s.jainFairness||1)>0.7?'#3fb950':'#f85149'}">${(s.jainFairness||1).toFixed(3)}</span></div>`;
}

function updateEvents() {
  if(!state||!state.events)return;
  const el=document.querySelector('#events');
  let h='<h2>События</h2>';
  const max=Math.min(state.events.length,12);
  for(let i=state.events.length-max;i<state.events.length;i++){
    const e=state.events[i];
    const mins=Math.floor(e.tick*0.6/60);
    h+=`<div class="e ${e.type}">[${mins}м] ${e.type}: ${e.msg}</div>`;
  }
  el.innerHTML=h;
}

async function toggleNode(x,y){
  await fetch('/api/toggle-node',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({x,y})});
}

// === Scenario Functions ===
async function scenarioJam(){
  const raw=document.getElementById('jamCells').value;
  const cells=raw.split(';').map(s=>s.trim()).filter(s=>s).map(s=>{const p=s.split(',');return[parseInt(p[0]),parseInt(p[1])]});
  await fetch('/api/scenario/jam',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({cells})});
}
async function scenarioJamClear(){
  document.getElementById('jamCells').value='';
  await fetch('/api/scenario/jam',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({cells:[]})});
}
async function scenarioPartition(blocked){
  const raw=document.getElementById('partRegion').value;
  const parts=raw.split(' ').map(s=>s.split(',').map(Number));
  if(parts.length<2)return;
  await fetch('/api/scenario/partition',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({x1:parts[0][0],y1:parts[0][1],x2:parts[1][0],y2:parts[1][1],blocked})});
}
async function scenarioSybil(){
  const count=parseInt(document.getElementById('sybilCount').value)||20;
  await fetch('/api/scenario/sybil',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({count})});
}
async function scenarioLoad(){
  const raw=document.getElementById('loadParams').value;
  const parts=raw.split(',').map(Number);
  await fetch('/api/scenario/load',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({nodeCount:parts[0]||50,fileSize:(parts[1]||100)*1024})});
}
async function scenarioDayNight(prob){
  const p=prob||parseInt(document.getElementById('dayNightProb').value)||30;
  await fetch('/api/scenario/daynight',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({onlineProb:p})});
}

async function exportCSV() {
  const resp = await fetch('/api/export/csv');
  const blob = await resp.blob();
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = 'rmn_simulation.csv';
  a.click();
  URL.revokeObjectURL(url);
}

async function runSweep() {
  const param = document.getElementById('sweepParam').value;
  const rangeRaw = document.getElementById('sweepRange').value;
  const steps = parseInt(document.getElementById('sweepSteps').value) || 5;
  const ticks = parseInt(document.getElementById('sweepTicks').value) || 5000;
  const rangeParts = rangeRaw.split('-').map(Number);
  const rangeMin = rangeParts[0] || 0.01;
  const rangeMax = rangeParts[1] || rangeParts[0]*10 || 0.2;

  const resEl = document.getElementById('sweepResult');
  resEl.innerHTML = '<span style="color:#d29922">Запуск sweep...</span>';

  try {
    const r = await fetch('/api/sweep', {
      method: 'POST',
      headers: {'Content-Type': 'application/json'},
      body: JSON.stringify({
        iterations: 1,
        ticksPerRun: ticks,
        paramToSweep: param,
        rangeMin: rangeMin,
        rangeMax: rangeMax,
        steps: steps
      })
    });
    const data = await r.json();
    if (!data.values) { resEl.innerHTML = 'Нет данных'; return; }

    let html = '<b>Результаты sweep (' + param + ')</b><br>';
    html += '<table style="width:100%;border-collapse:collapse;font-size:9px">';
    html += '<tr style="color:#8b949e"><td>Значение</td><td>Отпр</td><td>Дост</td><td>Coll%</td><td>GiniBal</td><td>Jain</td><td>Supply</td></tr>';
    for (const p of data.values) {
      const col = p.collisionRate > 0.3 ? '#f85149' : '#3fb950';
      html += `<tr style="border-bottom:1px solid #21262d">
        <td>${p.paramValue.toFixed(3)}</td>
        <td>${p.totalSent}</td>
        <td style="color:#3fb950">${p.totalReceived}</td>
        <td style="color:${col}">${(p.collisionRate*100).toFixed(1)}%</td>
        <td>${p.giniBalance.toFixed(3)}</td>
        <td>${p.jainFairness.toFixed(3)}</td>
        <td>${p.totalSupply.toFixed(0)}</td>
      </tr>`;
    }
    html += '</table>';
    resEl.innerHTML = html;
  } catch(e) {
    resEl.innerHTML = '<span style="color:#f85149">Ошибка sweep</span>';
  }
}

function renderGraphs(){
  // Graph 1: балансы (зелёные/жёлтые/красные)
  if(history.length > 1){
    drawGraph('graph', [
      {data: history.map(h=>h.green), color:'#3fb950', label:'зел'},
      {data: history.map(h=>h.yellow), color:'#d29922', label:'жёл'},
      {data: history.map(h=>h.red), color:'#f85149', label:'крас'},
    ], state.nodes.length);
    const g1 = document.getElementById('graph');
    const ctx1 = g1.getContext('2d');
    ctx1.fillStyle = '#8b949e';
    ctx1.font = '10px monospace';
    ctx1.fillText('Балансы узлов', 5, 12);
  }

  if(flowHistory.length > 1){
    const maxFlow = Math.max(1, ...flowHistory.map(h=>Math.max(h.sentPM, h.recvPM, h.relayPM, h.creditPM)));
    drawGraph('graph2', [
      {data: flowHistory.map(h=>h.sentPM), color:'#3fb950', label:'отпр/мин'},
      {data: flowHistory.map(h=>h.recvPM), color:'#58a6ff', label:'дост/мин'},
      {data: flowHistory.map(h=>h.relayPM), color:'#f0883e', label:'relay/мин'},
      {data: flowHistory.map(h=>h.creditPM), color:'#f778ba', label:'credit/мин'},
    ], maxFlow);
    const g2 = document.getElementById('graph2');
    const ctx2 = g2.getContext('2d');
    ctx2.fillStyle = '#8b949e';
    ctx2.font = '10px monospace';
    ctx2.fillText('Поток (в минуту)', 5, 12);

    const maxSys = Math.max(1, ...flowHistory.map(h=>Math.max(h.tps, h.transit, h.success*2, h.online)));
    drawGraph('graph3', [
      {data: flowHistory.map(h=>h.tps/2), color:'#8b949e', label:'tps/2'},
      {data: flowHistory.map(h=>h.transit), color:'#f778ba', label:'в пути'},
      {data: flowHistory.map(h=>h.success*2), color:'#58a6ff', label:'успех%×2'},
      {data: flowHistory.map(h=>h.online), color:'#3fb950', label:'онлайн'},
    ], maxSys);
    const g3 = document.getElementById('graph3');
    const ctx3 = g3.getContext('2d');
    ctx3.fillStyle = '#8b949e';
    ctx3.font = '10px monospace';
    ctx3.fillText('Системные метрики', 5, 12);
  }

  if(supplyHistory.length > 1){
    const maxSupply = Math.max(1, ...supplyHistory.map(h=>Math.max(h.supply, h.netEmission||0)));
    drawGraph('graph4', [
      {data: supplyHistory.map(h=>h.supply), color:'#d29922', label:'total supply'},
      {data: supplyHistory.map(h=>h.netEmission||0), color:'#58a6ff', label:'net emission'},
    ], maxSupply);
    // Добавляем заголовок
    const g4 = document.getElementById('graph4');
    const ctx4 = g4.getContext('2d');
    ctx4.fillStyle = '#8b949e';
    ctx4.font = '10px monospace';
    ctx4.fillText('Supply (сумма балансов)', 5, 12);
  }

  if(giniHistory.length > 1){
    drawGraph('graph5', [
      {data: giniHistory.map(h=>h.giniB), color:'#f85149', label:'Gini баланса'},
      {data: giniHistory.map(h=>h.giniR), color:'#f0883e', label:'Gini репутации'},
      {data: giniHistory.map(h=>h.jainF), color:'#3fb950', label:'Jain fairness'},
      {data: giniHistory.map(h=>h.collR*10), color:'#f778ba', label:'Collision% x 10'},
    ], 1.0);
    const g5 = document.getElementById('graph5');
    const ctx5 = g5.getContext('2d');
    ctx5.fillStyle = '#8b949e';
    ctx5.font = '10px monospace';
    ctx5.fillText('Gini / Fairness / Collisions', 5, 12);
  }
}

function drawGraph(canvasId, lines, maxVal){
  const g = document.getElementById(canvasId);
  if(!g) return;
  const ctx = g.getContext('2d');
  const W = g.width, H = g.height;
  ctx.clearRect(0,0,W,H);
  if(lines[0].data.length < 2) return;

  const stepX = W / Math.max(lines[0].data.length-1, 1);
  for(const line of lines){
    ctx.beginPath();
    ctx.strokeStyle = line.color;
    ctx.lineWidth = 1;
    for(let i=0; i<line.data.length; i++){
      const x = i*stepX;
      const y = H - (line.data[i]/maxVal)*H;
      if(i===0) ctx.moveTo(x,y); else ctx.lineTo(x,y);
    }
    ctx.stroke();
  }
  // Подписи
  ctx.fillStyle = '#484f58'; ctx.font = '7px monospace';
  let ly = 8;
  for(const line of lines){
    ctx.fillStyle = line.color;
    ctx.fillText(line.label, 2, ly);
    ly += 10;
  }
}

// --- Poll loop ---
async function poll(){
  try{
    const r=await fetch('/api/state');
    const ns=await r.json();
    if(ns&&ns.nodes){
      state=ns;
      updateStats();
      // Сбор данных для графика (каждые 100 тиков, или сразу при старте)
      if(state.stats && (lastGraphTick === 0 || state.tick - lastGraphTick >= 100)){
        lastGraphTick = state.tick;
        const s = state.stats;
        // Graph 1: балансы
        history.push({tick:state.tick, green:s.greenCount, yellow:s.yellowCount, red:s.redCount});
        if(history.length > 200) history.shift();
        // Graph 2: поток (per min)
        let sentPM=0, recvPM=0, lostPM=0, relayPM=0, creditPM=0;
        if(prevStats){
          const dt = Math.max(1, (state.tick - prevStats.tick)/500); // минуты
          sentPM = Math.round((s.totalSent - prevStats.totalSent)/dt);
          recvPM = Math.round((s.totalReceived - prevStats.totalReceived)/dt);
          lostPM = Math.round((s.totalFailed - prevStats.totalFailed)/dt);
          relayPM = Math.round((s.totalRelayed - prevStats.totalRelayed)/dt);
          creditPM = Math.round(((s.totalRelay||0) - (prevStats.totalRelay||0))/dt);
        }
        prevStats = {tick:state.tick, totalSent:s.totalSent, totalReceived:s.totalReceived, totalFailed:s.totalFailed, totalRelayed:s.totalRelayed, totalRelay:s.totalRelay||0};
        flowHistory.push({sentPM, recvPM, lostPM, relayPM, creditPM, tps:s.tps, transit:s.activeMsgs, online:s.onlineNodes, success:parseFloat((s.totalReceived/(s.totalReceived+s.totalFailed+0.001)*100).toFixed(1))});
        if(flowHistory.length > 200) flowHistory.shift();
        supplyHistory.push({tick:state.tick, supply:s.totalSupply||0, netEmission:s.netEmission||0});
        if(supplyHistory.length > 200) supplyHistory.shift();
        giniHistory.push({tick:state.tick, giniB:s.giniBalance||0, giniR:s.giniReputation||0, jainF:s.jainFairness||1, collR:s.collisionRate||0});
        if(giniHistory.length > 200) giniHistory.shift();
      }
    }
  }catch(e){}
}

function loop(){
  updateStats();
  render();
  updateEvents();
  renderGraphs();
  requestAnimationFrame(loop);
}

setInterval(poll, 300);
poll();
requestAnimationFrame(loop);
