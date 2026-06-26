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
 .pos{color:#3ad17a}.neg{color:#e0556b}
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
  <h3>Strategy scorecards (net, by regime)</h3>
  <table><thead><tr><th>strategy</th><th>regime</th><th>trades</th><th>win%</th><th>net P&amp;L</th><th>expectancy</th><th>max DD</th></tr></thead><tbody id="scoreRows"></tbody></table>
  <h3>Projection (Monte-Carlo — past performance, not a guarantee)</h3>
  <div id="proj" style="padding:10px;background:#11151f;border:1px solid #1f2733;border-radius:10px;margin-bottom:8px">—</div>
  <svg id="coneSvg" viewBox="0 0 1000 120" preserveAspectRatio="none"><polyline id="coneP5" fill="none" stroke="#e0556b" stroke-width="1.5"/><polyline id="coneP50" fill="none" stroke="#3ad17a" stroke-width="2"/><polyline id="coneP95" fill="none" stroke="#5bc0eb" stroke-width="1.5"/></svg>
</main>
<script>
// API calls rely on the dash_token cookie being sent automatically with same-origin requests.
// No token is appended to URLs.
const q = p => fetch(p).then(r=>r.json());
const ctl = a => fetch('/api/'+a,{method:'POST'}).then(()=>refresh());
const fmt = n => (n>=0?'+':'') + Number(n).toFixed(2);
const t = ms => new Date(ms).toLocaleString();
async function refresh(){
  const [s,stat,eq,tr,sg,sc,pj] = await Promise.all([q('/api/status'),q('/api/stats'),q('/api/equity'),q('/api/trades'),q('/api/signals'),q('/api/scorecard'),q('/api/projection')]);
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
  const tb3=document.getElementById('scoreRows'); tb3.textContent=''; (sc||[]).forEach(x=>tb3.appendChild(mkRow([[x.Strategy],[x.Regime],[String(x.Trades)],[(x.Trades?(100*x.WinRate).toFixed(1):'0')+'%'],[fmt(x.NetPnL),x.NetPnL>=0?'pos':'neg'],[fmt(x.Expectancy),x.Expectancy>=0?'pos':'neg'],[Number(x.MaxDrawdown).toFixed(2)]])));
  if(pj&&pj.Steps>0){
    const projEl=document.getElementById('proj');
    const fmtX = v => Number(v).toFixed(2)+'x';
    const txt = 'median '+fmtX(pj.TermP50/10000)+', 5th '+fmtX(pj.TermP5/10000)+', 95th '+fmtX(pj.TermP95/10000)+', risk-of-ruin '+Number(pj.RiskOfRuinPct).toFixed(1)+'%';
    projEl.textContent = txt;
    const allVals=[...pj.P5,...pj.P50,...pj.P95];const lo=Math.min(...allVals),hi=Math.max(...allVals),rng=(hi-lo)||1;
    const toPts = arr => arr.map((v,i)=>(i*1000/(arr.length-1))+','+(115-110*(v-lo)/rng)).join(' ');
    document.getElementById('coneP5').setAttribute('points', toPts(pj.P5));
    document.getElementById('coneP50').setAttribute('points', toPts(pj.P50));
    document.getElementById('coneP95').setAttribute('points', toPts(pj.P95));
  }
}
refresh(); setInterval(refresh, 5000);
</script>
</body></html>`))

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	// If a valid ?token= param is present, set a HttpOnly cookie and redirect to /
	// to strip the token from the URL (browser history, Referer, proxy logs).
	// NOTE: Secure flag is omitted so this works over localhost http;
	// enable Secure when serving behind HTTPS/TLS termination.
	if tok := r.URL.Query().Get("token"); tok != "" && tokenEqual(tok, s.token) {
		http.SetCookie(w, &http.Cookie{
			Name:     "dash_token",
			Value:    tok,
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteStrictMode, // CSRF defense — required
		})
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = indexTmpl.Execute(w, nil)
}
