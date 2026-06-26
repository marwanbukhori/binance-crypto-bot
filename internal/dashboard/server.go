package dashboard

import (
	"html/template"
	"net/http"
)

var indexTmpl = template.Must(template.New("index").Parse(`<!DOCTYPE html>
<html lang="en"><head><meta charset="utf-8"><title>tradebot</title>
<style>
 body{font:14px/1.5 ui-sans-serif,system-ui,sans-serif;margin:0;background:#0b0e14;color:#d7dce5}
 header{display:flex;gap:16px;align-items:center;padding:12px 20px;background:#11151f;border-bottom:1px solid #1f2733}
 .pill{padding:2px 10px;border-radius:999px;background:#1b6e3a;color:#fff;font-weight:600}
 main{padding:20px;max-width:1100px;margin:0 auto}
 .tiles{display:grid;grid-template-columns:repeat(4,1fr);gap:12px;margin-bottom:18px}
 .tile{background:#11151f;border:1px solid #1f2733;border-radius:10px;padding:14px}
 .tile b{display:block;font-size:22px;margin-top:4px}
 table{width:100%;border-collapse:collapse;margin-top:8px;background:#11151f;border-radius:10px;overflow:hidden}
 th,td{padding:8px 10px;text-align:left;border-bottom:1px solid #1f2733;font-variant-numeric:tabular-nums}
 .pos{color:#3ad17a}.neg{color:# e0556b}
 button{background:#243044;color:#d7dce5;border:1px solid #33425c;border-radius:8px;padding:8px 14px;cursor:pointer}
 button.kill{background:#7a1f2b;border-color:#a3303f}
 svg{width:100%;height:120px;background:#11151f;border:1px solid #1f2733;border-radius:10px}
 h3{margin:18px 0 6px}
</style></head>
<body>
<header>
  <span class="pill" id="state">…</span>
  <span id="statusline" style="opacity:.85"></span>
  <span style="margin-left:auto;display:flex;gap:8px">
    <button onclick="ctl('pause')">Pause</button>
    <button onclick="ctl('resume')">Resume</button>
    <button class="kill" onclick="if(confirm('Trip the kill-switch?'))ctl('kill')">Kill</button>
  </span>
</header>
<main>
  <div class="tiles">
    <div class="tile">Equity<b id="equity">—</b></div>
    <div class="tile">Net P&amp;L (after fees)<b id="netpnl">—</b></div>
    <div class="tile">Win rate<b id="winrate">—</b></div>
    <div class="tile">Trades<b id="trades">—</b></div>
  </div>
  <h3>Equity</h3>
  <svg id="chart" viewBox="0 0 1000 120" preserveAspectRatio="none"><polyline id="curve" fill="none" stroke="#3ad17a" stroke-width="2"/></svg>
  <h3>Recent trades</h3>
  <table><thead><tr><th>time</th><th>symbol</th><th>strategy</th><th>reason</th><th>net</th></tr></thead><tbody id="tradeRows"></tbody></table>
  <h3>Recent decisions</h3>
  <table><thead><tr><th>time</th><th>symbol</th><th>strategy</th><th>action</th><th>reason</th></tr></thead><tbody id="sigRows"></tbody></table>
</main>
<script>
const TOK = new URLSearchParams(location.search).get('token') || '';
const q = p => fetch(p + (p.includes('?')?'&':'?') + 'token=' + encodeURIComponent(TOK)).then(r=>r.json());
const ctl = a => fetch('/api/'+a+'?token='+encodeURIComponent(TOK),{method:'POST'}).then(()=>refresh());
const fmt = n => (n>=0?'+':'') + Number(n).toFixed(2);
const t = ms => new Date(ms).toLocaleString();
async function refresh(){
  const [s,stat,eq,tr,sg] = await Promise.all([q('/api/status'),q('/api/stats'),q('/api/equity'),q('/api/trades'),q('/api/signals')]);
  document.getElementById('state').textContent = s.paused?'PAUSED':'RUNNING';
  document.getElementById('state').style.background = s.paused?'#7a5a1f':'#1b6e3a';
  document.getElementById('statusline').textContent = s.status||'';
  document.getElementById('netpnl').textContent = fmt(stat.NetPnL);
  document.getElementById('winrate').textContent = (stat.Trades?(100*stat.WinRate).toFixed(1):'0')+'%';
  document.getElementById('trades').textContent = stat.Trades||0;
  const pts = eq||[];
  document.getElementById('equity').textContent = pts.length?Number(pts[pts.length-1].Equity).toFixed(2):'—';
  if(pts.length>1){const xs=pts.map((_,i)=>i*1000/(pts.length-1));const ys=pts.map(p=>p.Equity);const lo=Math.min(...ys),hi=Math.max(...ys),rng=(hi-lo)||1;
    document.getElementById('curve').setAttribute('points', pts.map((p,i)=>xs[i]+','+(115-110*(p.Equity-lo)/rng)).join(' '));}
  const mkRow = cells => { const tr2=document.createElement('tr'); cells.forEach(([txt,cls])=>{ const td=document.createElement('td'); td.textContent=txt; if(cls)td.className=cls; tr2.appendChild(td); }); return tr2; };
  const tb1=document.getElementById('tradeRows'); tb1.textContent=''; (tr||[]).forEach(x=>tb1.appendChild(mkRow([[t(x.TS)],[x.Symbol],[x.Strategy],[x.Reason],[fmt(x.NetPnL),x.NetPnL>=0?'pos':'neg']])));
  const tb2=document.getElementById('sigRows');  tb2.textContent=''; (sg||[]).forEach(x=>tb2.appendChild(mkRow([[t(x.TS)],[x.Symbol],[x.Strategy],[x.Action],[x.Reason]])));
}
refresh(); setInterval(refresh, 5000);
</script>
</body></html>`))

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = indexTmpl.Execute(w, nil)
}
