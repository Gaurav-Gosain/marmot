// marmot play: the editor, Run, and the stages view. No framework: a textarea over a
// highlighted pre, and the server's JSON API (/api/initial, /api/stage, /api/run).
'use strict';

const $ = (id) => document.getElementById(id);
const src = $('src'), layer = $('layer'), gutter = $('gutter'), stageEl = $('stage');
const labels = { ssa: 'SSA', mlir: 'MLIR', llvm: 'LLVM IR', asm: 'Assembly', size: 'Size' };
const modeLabels = { dev: 'dev', O2: '-O 2', release: '-release' };
const state = { mode: 'dev', stage: 'mlir', stages: [], modes: [], result: null, diags: [], seq: 0, hot: 0, tints: new Map() };

const esc = (s) => s.replace(/[&<>]/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;' })[c]);

// ---- highlighting: small tokenizers, one line at a time ----

const goKeywords = new Set('break case chan const continue default defer else fallthrough for func go goto if import interface map package range return select struct switch type var'.split(' '));
const goTypes = new Set('bool byte complex64 complex128 error float32 float64 int int8 int16 int32 int64 rune string uint uint8 uint16 uint32 uint64 uintptr any comparable true false nil iota'.split(' '));

// highlightGo returns the HTML of text, starting in state st ("" or "block" or "raw"), and the
// state at its end.
function highlightGo(text, st) {
  let out = '', i = 0;
  const put = (cls, s) => { out += cls ? `<span class="${cls}">${esc(s)}</span>` : esc(s); };
  while (i < text.length) {
    if (st === 'block') {
      const j = text.indexOf('*/', i);
      const end = j < 0 ? text.length : j + 2;
      put('c', text.slice(i, end)); i = end; if (j >= 0) st = ''; continue;
    }
    if (st === 'raw') {
      const j = text.indexOf('`', i);
      const end = j < 0 ? text.length : j + 1;
      put('s', text.slice(i, end)); i = end; if (j >= 0) st = ''; continue;
    }
    const rest = text.slice(i);
    let m;
    if (rest.startsWith('//')) { put('c', rest); break; }
    if (rest.startsWith('/*')) { st = 'block'; continue; }
    if (rest[0] === '`') { put('s', '`'); i++; st = 'raw'; continue; }
    if ((m = /^"(?:[^"\\]|\\.)*"?|^'(?:[^'\\]|\\.)*'?/.exec(rest))) { put('s', m[0]); i += m[0].length; continue; }
    if ((m = /^(?:0[xXbBoO][\w]+|\d[\d_]*(?:\.\d*)?(?:[eE][+-]?\d+)?i?)/.exec(rest))) { put('n', m[0]); i += m[0].length; continue; }
    if ((m = /^[A-Za-z_]\w*/.exec(rest))) {
      const w = m[0];
      const call = text[i + w.length] === '(';
      put(goKeywords.has(w) ? 'k' : goTypes.has(w) ? 't' : call ? 'f' : '', w); i += w.length; continue;
    }
    put('', text[i]); i++;
  }
  return [out, st];
}

const stageRules = {
  ssa: [[/^;.*/, 'c'], [/^"(?:[^"\\]|\\.)*"/, 's'], [/^\bt\d+\b/, 'f'], [/^\b(?:if|jump|return|phi|call|invoke|go|defer|panic|make\w*|new|local|alloc|range|next|lookup|extract|convert|change\w*|slice|index\w*|field\w*|store|send|select)\b/, 'k'], [/^-?\b\d+(?:\.\d+)?\b/, 'n'], [/^[A-Za-z_][\w./*]*(?=\()/, 'f']],
  mlir: [[/^\/\/.*/, 'c'], [/^"(?:[^"\\]|\\.)*"/, 's'], [/^%[\w.#$-]+/, 'f'], [/^@[\w."$-]+/, 't'], [/^\b(?:go|llvm|arith|func|cf|builtin|scf)\.[\w.]+/, 'k'], [/^!llvm\.[\w<>]+|^\bi\d+\b|^\bf(?:16|32|64)\b|^\bindex\b/, 't'], [/^-?\b\d+(?:\.\d+)?(?:e[+-]?\d+)?\b/, 'n']],
  llvm: [[/^;.*/, 'c'], [/^c?"(?:[^"\\]|\\.)*"/, 's'], [/^%[\w.$"-]+/, 'f'], [/^@[\w.$"-]+/, 't'], [/^\b(?:define|declare|ret|br|switch|call|tail|musttail|load|store|getelementptr|inbounds|icmp|fcmp|phi|select|alloca|add|sub|mul|sdiv|udiv|srem|urem|shl|lshr|ashr|and|or|xor|zext|sext|trunc|bitcast|ptrtoint|inttoptr|fadd|fsub|fmul|fdiv|extractvalue|insertvalue|extractelement|insertelement|shufflevector|unreachable|label|nsw|nuw|nneg|disjoint|exact|eq|ne|slt|sle|sgt|sge|ult|ule|ugt|uge|private|internal|linkonce_odr|constant|global|unnamed_addr|local_unnamed_addr|noundef)\b/, 'k'], [/^\bi\d+\b|^\bptr\b|^\bvoid\b|^\b(?:float|double|half)\b|^<\d+ x \w+>/, 't'], [/^-?\b\d+(?:\.\d+)?\b/, 'n'], [/^#\d+/, 'c']],
  asm: [[/^;.*/, 'c'], [/^\/\/.*/, 'c'], [/^#-?(?:0x[\da-f]+|\d+)/, 'n'], [/^-?\b0x[\da-f]+\b|^-?\b\d+\b/, 'n'], [/^\b(?:[xwvqdsbh]\d+|sp|lr|fp|xzr|wzr|r[a-z]{2}|e[a-z]{2}|r\d+[dwb]?|[xy]mm\d+)\b/, 't'], [/^<[^>]*>/, 'f']],
};

function highlightStage(kind, text) {
  if (kind === 'asm') {
    if (/:$/.test(text.trim())) return `<span class="f">${esc(text)}</span>`;
    const m = /^(\s*)([\w.]+)(.*)$/.exec(text);
    if (m) return esc(m[1]) + `<span class="k">${esc(m[2])}</span>` + rules(kind, m[3]);
  }
  if (kind === 'ssa' && /^\d+:/.test(text)) return `<span class="t">${esc(text)}</span>`;
  return rules(kind, text);
}

function rules(kind, text) {
  const rs = stageRules[kind] || [];
  let out = '', i = 0, plain = '';
  const flush = () => { if (plain) { out += esc(plain); plain = ''; } };
  outer: while (i < text.length) {
    const rest = text.slice(i);
    const prev = i > 0 ? text[i - 1] : ' ';
    for (const [re, cls] of rs) {
      if (/\w/.test(prev) && /^\w/.test(rest)) break;
      const m = re.exec(rest);
      if (m && m[0].length) { flush(); out += `<span class="${cls}">${esc(m[0])}</span>`; i += m[0].length; continue outer; }
    }
    plain += text[i]; i++;
  }
  flush();
  return out;
}

// ---- the editor ----

function lines() { return src.value.split('\n'); }

function renderEditor() {
  const ls = lines();
  const byLine = new Map();
  for (const d of state.diags) if (d.line > 0) { if (!byLine.has(d.line)) byLine.set(d.line, []); byLine.get(d.line).push(d); }
  let st = '', html = '', gut = '';
  ls.forEach((text, k) => {
    const n = k + 1;
    let h = '';
    const ds = byLine.get(n);
    if (ds) {
      // underline the first diagnostic's span on this line (to the end of the line when the
      // span ends later)
      const d = ds[0];
      const c0 = Math.max(0, d.col - 1);
      let c1 = d.endLine === n && d.endCol > d.col ? d.endCol - 1 : (d.endLine > n ? text.length : c0 + Math.max(1, (/^\w+/.exec(text.slice(c0)) || [''])[0].length));
      c1 = Math.min(Math.max(c1, c0 + 1), Math.max(text.length, c0 + 1));
      let a, b, c;
      [a, st] = highlightGo(text.slice(0, c0), st);
      [b, st] = highlightGo(text.slice(c0, c1) || ' ', st);
      [c, st] = highlightGo(text.slice(c1), st);
      h = a + `<span class="e" title="${esc(d.message)}">${b}</span>` + c;
    } else {
      [h, st] = highlightGo(text, st);
    }
    const tint = state.tints.has(n) ? ` tint-${state.tints.get(n)}` : '';
    const hot = state.hot === n ? ' hot' : '';
    html += `<span class="ln${tint}${hot}" data-n="${n}">${h || ' '}</span>`;
    gut += `<div class="${ds ? 'err' : ''}${tint}${hot}">${n}</div>`;
  });
  layer.innerHTML = html;
  gutter.innerHTML = gut;
}

function renderDiags() {
  const box = $('diags');
  box.innerHTML = '';
  for (const d of state.diags) {
    const el = document.createElement('div');
    el.className = 'diag';
    const where = d.line ? `main.go:${d.line}:${d.col}: ` : '';
    el.innerHTML = `<span class="sev ${d.severity}">${esc(d.severity || 'error')}${d.code ? '[' + esc(d.code) + ']' : ''}</span> ${esc(where + d.message)}`;
    el.title = d.rendered || '';
    if (d.line) el.onclick = () => gotoLine(d.line, d.col);
    box.appendChild(el);
  }
}

function gotoLine(line, col) {
  const ls = lines();
  let off = 0;
  for (let i = 0; i < line - 1 && i < ls.length; i++) off += ls[i].length + 1;
  off += Math.max(0, (col || 1) - 1);
  src.focus();
  src.setSelectionRange(off, off);
}

src.addEventListener('input', () => { renderEditor(); schedule(); });
src.addEventListener('keydown', (e) => {
  if ((e.metaKey || e.ctrlKey) && e.key === 'Enter') { e.preventDefault(); run(); return; }
  if (e.key === 'Tab') {
    e.preventDefault();
    document.execCommand('insertText', false, '\t');
  } else if (e.key === 'Enter') {
    const v = src.value, at = src.selectionStart;
    const lineStart = v.lastIndexOf('\n', at - 1) + 1;
    let indent = /^\t*/.exec(v.slice(lineStart, at))[0];
    if (v[at - 1] === '{' || v[at - 1] === '(') indent += '\t';
    e.preventDefault();
    document.execCommand('insertText', false, '\n' + indent);
  }
});

// hovering a source line lights it and its instructions in the stage
const lineHeight = () => parseFloat(getComputedStyle(layer).lineHeight) || 20;
$('editor').addEventListener('mousemove', (e) => {
  const r = layer.getBoundingClientRect();
  const n = Math.floor((e.clientY - r.top - parseFloat(getComputedStyle(layer).paddingTop)) / lineHeight()) + 1;
  setHot(n >= 1 && n <= lines().length ? n : 0);
});
$('editor').addEventListener('mouseleave', () => setHot(0));

function setHot(n) {
  if (state.hot === n) return;
  state.hot = n;
  for (const el of layer.querySelectorAll('.ln.hot, .gutter .hot')) el.classList.remove('hot');
  for (const el of gutter.querySelectorAll('.hot')) el.classList.remove('hot');
  for (const el of stageEl.querySelectorAll('.row.hot')) el.classList.remove('hot');
  if (!n) return;
  layer.querySelector(`.ln[data-n="${n}"]`)?.classList.add('hot');
  gutter.children[n - 1]?.classList.add('hot');
  let first = null;
  for (const el of stageEl.querySelectorAll(`.row[data-src="${n}"]`)) { el.classList.add('hot'); first = first || el; }
  if (first && !inView(first, stageEl)) first.scrollIntoView({ block: 'center' });
}

function inView(el, box) {
  const a = el.getBoundingClientRect(), b = box.getBoundingClientRect();
  return a.top >= b.top && a.bottom <= b.bottom;
}

// ---- stages ----

let timer = 0;
function schedule() { clearTimeout(timer); timer = setTimeout(loadStage, 700); }

async function post(path, body) {
  const r = await fetch(path, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
  if (!r.ok) throw new Error(await r.text());
  return r.json();
}

async function loadStage() {
  const seq = ++state.seq;
  const stage = state.stage;
  $('status').textContent = 'compiling';
  if (!state.result) stageEl.innerHTML = '<div class="busy">compiling</div>';
  try {
    const res = await post('/compile', { source: src.value, mode: state.mode, stages: [stage] });
    if (seq !== state.seq) return;
    state.result = res;
    state.diags = (res.diagnostics || []).filter((d) => !d.file || d.file === 'main.go');
    $('status').textContent = !res.ok ? 'does not build' : `${labels[stage]} in ${res.millis} ms`;
    const st = res.stages && res.stages[stage] || {};
    renderStage(stage, { ...st, ok: res.ok, error: res.ok ? st.error : (res.error || 'build failed'), diagnostics: res.diagnostics, size: res.size });
  } catch (err) {
    if (seq !== state.seq) return;
    $('status').textContent = '';
    stageEl.innerHTML = `<div class="msg err">${esc(String(err))}</div>`;
  }
  renderEditor();
  renderDiags();
}

function renderStage(stage, res) {
  $('note').textContent = res.note || '';
  state.tints = new Map();
  if (res.error) {
    stageEl.innerHTML = `<div class="msg err">${esc(res.diagnostics && res.diagnostics.length ? 'The program does not build: see the messages under the editor.' : res.error)}</div>`;
    return;
  }
  if (stage === 'size') { renderSize(res.size); return; }
  const text = res.text.split('\n');
  const map = res.lines || [];
  let rank = 0;
  for (const n of map) if (n > 0 && !state.tints.has(n)) state.tints.set(n, rank++ % 6);
  const rows = text.map((t, i) => {
    const n = map[i] || 0;
    const tint = n ? ` tint-${state.tints.get(n)}` : '';
    return `<div class="row${tint}" data-src="${n}"><span class="src">${n || ''}</span><span class="txt">${highlightStage(stage, t.replace(/\t/g, '    ')) || ' '}</span></div>`;
  });
  stageEl.innerHTML = `<pre>${rows.join('')}</pre>`;
}

stageEl.addEventListener('mouseover', (e) => {
  const row = e.target.closest('.row');
  if (row) setHot(+row.dataset.src || 0);
});
stageEl.addEventListener('mouseleave', () => setHot(0));

function human(n) {
  if (n >= 1 << 20) return (n / (1 << 20)).toFixed(1) + ' MB';
  if (n >= 1 << 10) return (n / (1 << 10)).toFixed(1) + ' KB';
  return n + ' B';
}

function renderSize(s) {
  const max = Math.max(...s.packages.map((p) => p.size), 1);
  const rows = (list, total) => list.map((p) => `<tr><td class="num">${human(p.size)}</td><td class="num">${(100 * p.size / total).toFixed(1)}%</td><td>${esc(p.name)}</td><td class="bar"><div class="b" style="width:${Math.max(1, 100 * p.size / max)}%"></div></td></tr>`).join('');
  stageEl.innerHTML = `<div class="size">
    <h3>binary</h3><div class="total">${human(s.file)}</div><div class="muted">${human(s.text)} of code</div>
    <h3>code by package</h3><table>${rows(s.packages, s.text)}</table>
    <h3>sections</h3><table>${s.sections.map((x) => `<tr><td class="num">${human(x.size)}</td><td>${esc(x.name)}</td></tr>`).join('')}</table>
  </div>`;
}

// ---- run ----

async function run() {
  const btn = $('run');
  btn.disabled = true;
  $('runinfo').textContent = 'building';
  const out = $('output');
  try {
    const res = await post('/api/run', { source: src.value, mode: state.mode });
    state.diags = res.diagnostics || [];
    renderEditor();
    renderDiags();
    if (!res.run) {
      out.innerHTML = `<span class="stderr">${esc(res.error || 'The program does not build: see the messages under the editor.')}</span>`;
      $('runinfo').textContent = `build failed in ${res.buildMillis} ms`;
      return;
    }
    const r = res.run;
    out.innerHTML = esc(r.stdout) + (r.stderr ? `<span class="stderr">${esc(r.stderr)}</span>` : '') +
      `\n<span class="exit">${r.timedOut ? 'killed: the program ran too long' : 'exit status ' + r.exit}</span>`;
    $('runinfo').textContent = `built in ${res.buildMillis} ms, ran in ${r.millis} ms, ${human(res.size || 0)}`;
  } catch (err) {
    out.innerHTML = `<span class="stderr">${esc(String(err))}</span>`;
    $('runinfo').textContent = '';
  } finally {
    btn.disabled = false;
  }
}
$('run').onclick = run;

// ---- setup ----

function setupTabs() {
  const tabs = $('tabs');
  state.stages.forEach((s, i) => {
    const b = document.createElement('button');
    b.textContent = labels[s] || s;
    b.setAttribute('role', 'tab');
    b.style.setProperty('--ramp', `var(--stage-${Math.min(i + 1, 5)})`);
    b.onclick = () => selectStage(s);
    tabs.appendChild(b);
  });
}

function selectStage(s) {
  state.stage = s;
  state.result = null;
  const i = state.stages.indexOf(s);
  [...$('tabs').children].forEach((b, j) => b.setAttribute('aria-selected', String(j === i)));
  stageEl.style.setProperty('--ramp', `var(--stage-${Math.min(i + 1, 5)})`);
  try { localStorage.setItem('marmot-play-stage', s); } catch {}
  loadStage();
}

function setupModes() {
  const box = $('modes');
  for (const m of state.modes) {
    const b = document.createElement('button');
    b.textContent = modeLabels[m] || m;
    b.setAttribute('role', 'radio');
    b.onclick = () => {
      state.mode = m;
      for (const x of box.children) x.setAttribute('aria-checked', String(x === b));
      try { localStorage.setItem('marmot-play-mode', m); } catch {}
      state.result = null;
      loadStage();
    };
    b.setAttribute('aria-checked', String(m === state.mode));
    box.appendChild(b);
  }
}

(async function init() {
  const r = await fetch('/api/initial').then((x) => x.json());
  state.modes = r.modes;
  state.stages = r.stages;
  try {
    const m = localStorage.getItem('marmot-play-mode'), s = localStorage.getItem('marmot-play-stage');
    if (state.modes.includes(m)) state.mode = m;
    if (state.stages.includes(s)) state.stage = s;
  } catch {}
  src.value = r.source;
  setupModes();
  setupTabs();
  renderEditor();
  selectStage(state.stage);
})();
