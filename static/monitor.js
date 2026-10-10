/* Profile — System Monitor. Small vanilla JS, no libraries.
   Ported from the v2.1 prototype script. Without JS the page is the static Profile
   sheet; Resources still renders server-side and is reached via #resources (:target).

   Data: the feed embedded in <script id="cl-stats"> (same JSON as /api/stats.json).
   It is refreshed from /api/stats.json every 60 s while the tab is visible.

   URL hash: #profile | #resources picks a tab (plain links work too);
   range=7d starts the history graph on the 7-day range. */
(() => {
  'use strict';
  const $ = (s, r = document) => r.querySelector(s);
  const $$ = (s, r = document) => [...r.querySelectorAll(s)];
  const NS = 'http://www.w3.org/2000/svg';
  const el = (tag, attrs, parent, svg) => {
    const e = svg ? document.createElementNS(NS, tag) : document.createElement(tag);
    for (const k in attrs) k === 'text' ? (e.textContent = attrs[k]) : e.setAttribute(k, attrs[k]);
    if (parent) parent.appendChild(e);
    return e;
  };
  const set = (id, v) => { $('#' + id).textContent = v; };
  // 4477200 -> "4.5M", 182300 -> "182k", 4710 -> "4.7k"
  const fmt = n => n >= 1e6 ? (n / 1e6).toFixed(1) + 'M' : n >= 1e4 ? Math.round(n / 1e3) + 'k'
    : n >= 1e3 ? (n / 1e3).toFixed(1) + 'k' : String(n);
  const sum = a => a.reduce((s, v) => s + (v || 0), 0);
  const WD = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'];
  const MON = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];
  const TABS = ['profile', 'resources'];
  const REFRESH_MS = 60 * 1000;

  let data = JSON.parse($('#cl-stats').textContent);
  let m = null;                       // derived view of `data`, rebuilt on every refresh
  let range = '24h';

  function derive(d) {
    const T = d.tokens;
    const hourTot = T.hourly.input.map((v, i) => v + T.hourly.output[i]);
    const dayTot = T.daily.input.map((v, i) => v == null ? null : v + T.daily.output[i]);
    let last = dayTot.length - 1; while (last > 0 && dayTot[last] == null) last--;   // index of "today"
    return {
      T, live: !d.sample, hourTot, dayTot, last,
      H0: parseInt(T.hourly.start.slice(11, 13), 10),
      todayTot: T.today.input + T.today.output,
      snap: `${d.generatedAt.slice(0, 16).replace('T', ' ')} (UTC+8)`,
    };
  }
  const hourLabel = i => String((m.H0 + i) % 24).padStart(2, '0') + ':00';
  // dates are read as plain calendar values so the viewer's own time zone never shifts them
  const dayAt = i => new Date(Date.parse(m.T.daily.start + 'T00:00:00Z') + i * 864e5);
  const dayLabel = d => `${WD[d.getUTCDay()]} ${String(d.getUTCDate()).padStart(2, '0')}`;
  const iso = d => d.toISOString().slice(0, 10);

  /* ---------- badges, status line, toolbar sparkline ---------- */
  function renderBadges() {
    const live = m.live;
    document.documentElement.classList.toggle('is-live', live);
    const sp = $('.spark');
    sp.title = live ? 'tokens per hour, last 24h (via magpie) — open Resources'
      : 'tokens per hour, last 24h (sample data) — open Resources';
    sp.setAttribute('aria-label', live ? 'token use, last 24 hours: open Resources'
      : 'token use, last 24 hours, sample data: open Resources');
    $('.sb-mon').title = live ? 'tokens from magpie' : 'sample data: fake numbers, not a live feed';
  }
  function renderStatus() {
    set('sb-today', fmt(m.todayTot));
    const svg = $('#spark'), W = 72, H = 24, mx = Math.max(1, ...m.hourTot);
    svg.textContent = '';
    const pts = m.hourTot.map((v, i) => `${(i * W / (m.hourTot.length - 1)).toFixed(1)},${(H - 1 - v / mx * (H - 4)).toFixed(1)}`).join(' ');
    el('polygon', { points: `0,${H} ${pts} ${W},${H}`, class: 'sp-a' }, svg, 1);
    el('polyline', { points: pts, class: 'sp-l' }, svg, 1);
  }

  /* ---------- notebook tabs (ARIA tabs, automatic activation, hash = tab) ---------- */
  function selectTab(name, focus) {
    TABS.forEach(n => {
      const on = n === name, tab = $('#tab-' + n);
      tab.setAttribute('aria-selected', on);
      tab.tabIndex = on ? 0 : -1;
      $('#' + n).classList.toggle('on', on);
    });
    if (focus) $('#tab-' + name).focus();
    if (name === 'resources') drawGraph();                       // needs a laid-out width
  }
  TABS.forEach((n, i) => {
    const tab = $('#tab-' + n);
    tab.addEventListener('click', () => { selectTab(n); setHash(n); });
    tab.addEventListener('keydown', e => {
      const to = { ArrowRight: i + 1, ArrowLeft: i - 1, Home: 0, End: TABS.length - 1 }[e.key];
      if (to === undefined) return;
      e.preventDefault();
      const next = TABS[(to + TABS.length) % TABS.length];
      selectTab(next, true); setHash(next);
    });
  });
  const setHash = n => history.replaceState(null, '', '#' + n);   // no history spam, no scroll jump
  function applyHash() {
    const o = {};
    location.hash.slice(1).split('&').forEach(t => { const [k, v] = t.split('='); if (v === undefined) o.tab = k; else o[k] = v; });
    if (TABS.includes(o.tab)) selectTab(o.tab);
    else if (o.tab) {                    // e.g. #main or #h-who: show the panel that holds it
      const t = document.getElementById(o.tab), p = t && t.closest('[role=tabpanel]');
      if (p) selectTab(p.id);
    }
    return o;
  }
  window.addEventListener('hashchange', applyHash);

  /* ---------- Resources: history graph (SVG, redrawn on resize / range change / refresh) ---------- */
  const niceStep = v => { const p = 10 ** Math.floor(Math.log10(v)); return p * [1, 1.2, 1.5, 2, 2.5, 3, 4, 5, 6, 8, 10].find(k => k * p >= v); };
  function series() {
    const { T, last } = m;
    if (range === '24h') return { inp: T.hourly.input, out: T.hourly.output, label: hourLabel, step: 3,
      cap: 'tokens per hour, last 24 hours' };
    const a = Math.max(0, last - 6);
    return { inp: T.daily.input.slice(a, last + 1), out: T.daily.output.slice(a, last + 1), step: 1,
      label: i => dayLabel(dayAt(a + i)), cap: 'tokens per day, last 7 days' };
  }
  function drawGraph() {
    const svg = $('#graph'), W = svg.clientWidth, H = svg.clientHeight;
    if (!W || !m) return;                                           // panel hidden
    const s = series(), n = s.inp.length, SMP = m.live ? '' : ' (sample)', SMPD = m.live ? '' : ', sample data';
    const L = 46, R = 12, TOP = 8, B = 22, pw = W - L - R, ph = H - TOP - B;
    const step = niceStep(Math.max(1, ...s.inp, ...s.out) / 4), max = step * 4;
    const x = i => L + i * pw / (n - 1), y = v => TOP + ph - v / max * ph;
    svg.setAttribute('viewBox', `0 0 ${W} ${H}`);
    svg.textContent = '';
    el('rect', { x: L + .5, y: TOP + .5, width: pw, height: ph, class: 'g-plot' }, svg, 1);
    for (let g = 0; g <= 4; g++) {                                  // horizontal grid + y labels
      const gy = Math.round(TOP + ph * g / 4) + .5;
      if (g && g < 4) el('line', { x1: L, x2: L + pw, y1: gy, y2: gy, class: 'g-grid' }, svg, 1);
      el('text', { x: L - 6, y: gy + 4, 'text-anchor': 'end', class: 'g-ax', text: fmt(max * (4 - g) / 4) }, svg, 1);
    }
    const every = s.step * (W < 520 && n > 12 ? 2 : 1);             // fewer x labels when narrow
    for (let i = 0; i < n; i++) {
      if (i % every) continue;
      const gx = Math.round(x(i)) + .5;
      if (i) el('line', { x1: gx, x2: gx, y1: TOP, y2: TOP + ph, class: 'g-grid' }, svg, 1);
      el('text', { x: gx, y: H - 6, 'text-anchor': !i ? 'start' : i === n - 1 ? 'end' : 'middle', class: 'g-ax', text: s.label(i) }, svg, 1);
    }
    [['in', s.inp], ['out', s.out]].forEach(([k, arr]) => {          // filled area + line per series
      const pts = arr.map((v, i) => `${x(i).toFixed(1)},${y(v).toFixed(1)}`).join(' ');
      el('polygon', { points: `${L},${TOP + ph} ${pts} ${L + pw},${TOP + ph}`, class: 'a-' + k }, svg, 1);
      el('polyline', { points: pts, class: 'l-' + k }, svg, 1);
      el('circle', { cx: x(n - 1), cy: y(arr[n - 1]), r: 3, class: 'd-' + k }, svg, 1);
    });
    for (let i = 0; i < n; i++) {                                    // hover strips with native tooltips
      const w = pw / (n - 1);
      const hit = el('rect', { x: Math.max(L, x(i) - w / 2), y: TOP, width: w / (i && i < n - 1 ? 1 : 2), height: ph, class: 'g-hit' }, svg, 1);
      el('title', { text: `${s.label(i)}: input ${fmt(s.inp[i])}, output ${fmt(s.out[i])} tokens${SMP}` }, hit, 1);
    }
    set('graph-cap', s.cap);
    svg.setAttribute('aria-label', `${s.cap}${SMPD}. input peaks at ${fmt(Math.max(...s.inp))}, output at ${fmt(Math.max(...s.out))}.`);
  }
  $$('[data-range]').forEach(b => b.addEventListener('click', () => {
    range = b.dataset.range;
    $$('[data-range]').forEach(o => o.setAttribute('aria-pressed', o === b));
    drawGraph();
  }));
  let resizeTimer;
  window.addEventListener('resize', () => { clearTimeout(resizeTimer); resizeTimer = setTimeout(drawGraph, 120); });

  /* ---------- Resources: readouts + legend values ---------- */
  function renderReadouts() {
    const { T, hourTot, dayTot, last, todayTot, snap, live } = m;
    const nowH = hourTot.length - 1, peak = hourTot.indexOf(Math.max(...hourTot));
    set('lg-in', `today ${fmt(T.today.input)} · this hour ${fmt(T.hourly.input[nowH])}`);
    set('lg-out', `today ${fmt(T.today.output)} · this hour ${fmt(T.hourly.output[nowH])}`);
    set('r-today', fmt(todayTot)); set('r-in', fmt(T.today.input)); set('r-out', fmt(T.today.output));
    set('r-hour', fmt(hourTot[nowH])); set('r-peak', `${hourLabel(peak)} · ${fmt(hourTot[peak])}`);
    set('r-7d', fmt(sum(dayTot.slice(Math.max(0, last - 6), last + 1))));
    set('ro-note', `${live ? 'magpie' : 'sample'} snapshot ${snap}; "this hour" is still filling.`);
  }

  /* ---------- Resources: 16-week heatmap (columns = weeks, Mon..Sun) ---------- */
  function renderHeat() {
    const { dayTot } = m, heat = $('#heat');
    const hmax = Math.max(0, ...dayTot.filter(v => v != null));
    heat.textContent = '';
    ['', 'Mon', '', 'Wed', '', 'Fri', '', ''].forEach(t => el('span', t ? { class: 'dl', text: t } : {}, heat));
    dayTot.forEach((v, i) => {
      const d = dayAt(i);
      if (i % 7 === 0) {                                             // month label above each week column
        const mo = d.getUTCMonth(), show = i === 0 || mo !== dayAt(i - 7).getUTCMonth();
        el('span', { class: 'hm', text: show ? MON[mo] : '' }, heat);
      }
      const lvl = v == null ? 'fut' : 'l' + (hmax ? Math.min(4, Math.ceil(v / hmax * 4)) : 0);
      const label = `${WD[d.getUTCDay()]} ${iso(d)}: ${v == null ? 'not yet' : fmt(v) + ' tokens'}`;
      el('span', { class: 'hc ' + lvl, title: label, 'data-label': label }, heat);
    });
    const best = dayTot.indexOf(hmax);
    heat.setAttribute('aria-label', `token activity per day for 16 weeks${m.live ? '' : ', sample data'}. busiest day ${iso(dayAt(best))} with ${fmt(hmax)} tokens; today ${fmt(dayTot[m.last])}.`);
  }
  $('#heat').addEventListener('mouseover', e => { if (e.target.dataset.label) set('heat-read', e.target.dataset.label); });

  /* ---------- render everything from the current feed ---------- */
  function render() {
    m = derive(data);
    renderBadges(); renderStatus(); renderReadouts(); renderHeat(); renderGraph();
  }
  function renderGraph() { drawGraph(); }

  /* ---------- live refresh ---------- */
  async function refresh() {
    if (document.visibilityState !== 'visible') return;
    try {
      const res = await fetch('/api/stats.json', { cache: 'no-store', headers: { accept: 'application/json' } });
      if (!res.ok) return;
      data = await res.json();
      render();
    } catch (e) {
      // offline or a blip: keep the last snapshot on screen
    }
  }

  /* ---------- window controls: minimise / maximise / close ---------- */
  const win = $('.window'), dlg = $('#quit-dlg'), info = $('#infobar');
  const bMin = $('.wm .min'), bMax = $('.wm .max'), bCls = $('.wm .cls');
  $$('.wm button').forEach(b => { b.disabled = false; });
  function minimise(on) {                    // window -> task chip on a GNOME 2 bottom panel, and back
    win.hidden = on; $('#gpanel').hidden = !on; $('#desk-note').hidden = !on;
    (on ? $('#taskchip') : bMin).focus();
  }
  function maximise(on) { win.classList.toggle('wide', on); bMax.setAttribute('aria-pressed', on); drawGraph(); }
  bMin.addEventListener('click', () => minimise(true));
  $('#taskchip').addEventListener('click', () => minimise(false));
  bMax.addEventListener('click', () => maximise(bMax.getAttribute('aria-pressed') !== 'true'));
  // <dialog>.showModal() makes the page inert (focus trap) and Esc cancels; we restore focus on close
  bCls.addEventListener('click', () => { dlg.returnValue = ''; dlg.showModal(); });
  dlg.addEventListener('close', () => {
    if (dlg.returnValue === 'quit') { info.hidden = false; $('#info-ok').focus(); } else bCls.focus();
  });
  $('#info-ok').addEventListener('click', () => { info.hidden = true; bCls.focus(); });

  /* ---------- start: hash picks tab and range ---------- */
  render();
  const o = applyHash();
  if (o.range === '7d') $('[data-range="7d"]').click();
  setInterval(refresh, REFRESH_MS);
})();
