// render.js — per-module rendering. Each module's sub-panel inside a
// .workstation-probe-panel renders:
//   - cpu      : per-core tiles (color = current utilization), no chart
//   - memory   : htop-style stacked bar + meta
//   - gpu      : per-device cell grid (no big number, no chart —
//                dot / edge / meters carry the at-a-glance signal)
//   - storage  : per-mount row list + meta
//
// Each module writes into the meta via the data-wsp-sub-meta-text span;
// the CPU sub-panel additionally has a legend element rendered by
// boot.js that we leave untouched. Chart.js is imported lazily so the
// shell / loading skeleton can render without waiting for the CDN.

const HISTORY_LEN = 60;
const histories = new WeakMap();

const SUB_CLASS = 'workstation-probe-sub';
const SUB_STATE_ATTR = 'data-wsp-sub-state';
const VALUE_SELECTOR = '[data-wsp-value]';
const CORES_SELECTOR = '[data-wsp-cores]';
const MEMBAR_SELECTOR = '[data-wsp-membar]';
const STORAGE_SELECTOR = '[data-wsp-storage]';
const GPU_SELECTOR = '[data-wsp-gpu]';
const META_TEXT_SELECTOR = '[data-wsp-sub-meta-text]';
const MODULE_ATTR = 'data-workstation-probe-module';
const HEADER_FACT_TIME_SELECTOR = '[data-workstation-probe-header-fact-server-time]';
const HEADER_FACT_UPTIME_SELECTOR = '[data-workstation-probe-header-fact-uptime]';

export function renderPanel(panel, _cfg, data, profile = null) {
  renderHeaderFacts(panel, data, profile);
  for (const sub of panel.querySelectorAll(`.${SUB_CLASS}`)) {
    const mod = sub.getAttribute(MODULE_ATTR);
    renderSub(sub, mod, data, profile);
  }
}

function renderHeaderFacts(panel, data, profile) {
  const timeEl = panel.querySelector(HEADER_FACT_TIME_SELECTOR);
  const uptimeEl = panel.querySelector(HEADER_FACT_UPTIME_SELECTOR);

  let timeText = '';
  if (data.server_time_local) {
    const zone = formatServerZone(
      profile?.server_timezone || data.server_timezone,
      profile?.server_timezone_offset_seconds ?? data.server_timezone_offset_seconds,
    );
    timeText = zone ? `${data.server_time_local} ${zone}` : data.server_time_local;
  }
  setHeaderFact(timeEl, timeText);

  const uptime = hostUptimeSeconds(data, profile);
  setHeaderFact(uptimeEl, uptime > 0 ? `up ${formatDuration(uptime)}` : '');
}

function setHeaderFact(el, text) {
  if (!el) return;
  el.textContent = text;
  if (text) {
    el.removeAttribute('data-wsp-empty');
  } else {
    el.setAttribute('data-wsp-empty', 'true');
  }
}

function renderSub(sub, mod, data, profile) {
  clearLoading(sub);
  const metaEl = sub.querySelector(META_TEXT_SELECTOR);
  const canvas = sub.querySelector('canvas');
  switch (mod) {
    case 'cpu':
      renderCpu(sub, metaEl, data.cpu);
      break;
    case 'memory':
      renderMemory(sub, sub.querySelector(VALUE_SELECTOR), metaEl, canvas, data.memory);
      break;
    case 'gpu':
      renderGpu(sub, sub.querySelector(GPU_SELECTOR), metaEl, canvas, data.gpu, profile?.modules?.gpu);
      break;
    case 'storage':
      renderStorage(sub, sub.querySelector(VALUE_SELECTOR), metaEl, canvas, data.storage);
      break;
    default:
      if (metaEl) metaEl.textContent = 'unknown module';
      sub.setAttribute(SUB_STATE_ATTR, 'err');
  }
}

function clearLoading(sub) {
  for (const el of sub.querySelectorAll('[data-wsp-loading]')) {
    el.remove();
  }
}

function pushPoint(canvas, value) {
  let hist = histories.get(canvas);
  if (!hist) {
    hist = [];
    histories.set(canvas, hist);
  }
  hist.push({ x: Date.now(), y: value });
  while (hist.length > HISTORY_LEN) hist.shift();
  return hist;
}

function chartArgs(hist, color, max = 100) {
  return { points: hist, color, max };
}

let chartsModulePromise = null;

function ensureChart(canvas, dataset) {
  if (!chartsModulePromise) {
    chartsModulePromise = import('./charts.js');
  }
  chartsModulePromise
    .then((mod) => mod.ensureChart(canvas, dataset))
    .catch((err) => console.error('[workstation-probe] chart load failed:', err));
}

function setState(sub, state) {
  sub.setAttribute(SUB_STATE_ATTR, state);
}

function fmtBytes(n) {
  if (!n || n <= 0) return '0';
  const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'];
  let i = 0;
  let v = n;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return v.toFixed(v >= 100 ? 0 : v >= 10 ? 1 : 2) + ' ' + units[i];
}

function formatServerZone(name, offsetSeconds) {
  const parts = [];
  if (name) parts.push(name);
  if (typeof offsetSeconds === 'number') parts.push(formatUTCOffset(offsetSeconds));
  return parts.join(' ');
}

function formatUTCOffset(seconds) {
  const sign = seconds < 0 ? '-' : '+';
  const abs = Math.abs(seconds);
  const hh = String(Math.floor(abs / 3600)).padStart(2, '0');
  const mm = String(Math.floor((abs % 3600) / 60)).padStart(2, '0');
  return `UTC${sign}${hh}:${mm}`;
}

function hostUptimeSeconds(data, profile) {
  if (typeof data.host_uptime_seconds === 'number') {
    return data.host_uptime_seconds;
  }
  const serverNow = data.server_time_unix_seconds;
  const boot = profile?.host_boot_time_unix_seconds;
  if (typeof serverNow !== 'number' || typeof boot !== 'number') {
    return 0;
  }
  return Math.max(0, serverNow - boot);
}

function formatDuration(totalSeconds) {
  const sec = Math.max(0, Math.floor(totalSeconds));
  const days = Math.floor(sec / 86400);
  const hours = Math.floor((sec % 86400) / 3600);
  const mins = Math.floor((sec % 3600) / 60);
  if (days > 0) return `${days}d ${hours}h ${mins}m`;
  if (hours > 0) return `${hours}h ${mins}m`;
  return `${mins}m`;
}

function renderCpu(sub, metaEl, cpu) {
  const coresEl = sub.querySelector(CORES_SELECTOR);
  if (!cpu || cpu.error) {
    if (coresEl) {
      coresEl.innerHTML = '';
      const note = document.createElement('div');
      note.className = `${SUB_CLASS}-core`;
      note.title = cpu && cpu.error ? cpu.error : 'no data';
      coresEl.appendChild(note);
    }
    if (metaEl) metaEl.textContent = cpu && cpu.error ? cpu.error : '';
    setState(sub, 'err');
    return;
  }

  // per_core_percent is the per-core utilization array. Fall back to a
  // single uniform array if the API ever returns overall only.
  const perCore = Array.isArray(cpu.per_core_percent) && cpu.per_core_percent.length > 0
    ? cpu.per_core_percent
    : [cpu.overall_percent];

  let sum = 0;
  if (coresEl) {
    ensureCoreTiles(coresEl, perCore.length);
    for (let i = 0; i < perCore.length; i++) {
      const p = clamp(perCore[i], 0, 100);
      const tile = coresEl.children[i];
      // Color encodes utilization; the number in the centre is the core
      // index (stable across polls) so the eye doesn't have to re-parse
      // jittering digits every refresh.
      const bg = coreColor(p);
      tile.style.background = bg;
      tile.style.color = coreTextColor(bg);
      tile.textContent = String(i);
      tile.title = `core ${i}: ${p.toFixed(1)}%`;
      sum += p;
    }
  }

  const cores = perCore.length;
  const avg = cores > 0 ? sum / cores : 0;
  if (metaEl) metaEl.textContent = `${cores} ${cores === 1 ? 'core' : 'cores'} · ${avg.toFixed(1)}% avg`;
  setState(sub, 'ok');
}

// ensureCoreTiles grows or shrinks the grid to match the new core count,
// reusing existing tiles across polls to avoid layout thrash.
function ensureCoreTiles(container, n) {
  const current = container.children.length;
  if (current < n) {
    const frag = document.createDocumentFragment();
    for (let i = current; i < n; i++) {
      const tile = document.createElement('div');
      tile.className = `${SUB_CLASS}-core`;
      frag.appendChild(tile);
    }
    container.appendChild(frag);
  } else if (current > n) {
    while (container.children.length > n) {
      container.lastElementChild.remove();
    }
  }
}

// Matplotlib RdYlGn_r colormap (Red-Yellow-Green reversed).
// 11 canonical sRGB hex stops from matplotlib 3.x. Used to color each
// CPU core tile by its current utilization: 0% → deep green (#006837),
// 100% → deep red (#a50026), with yellow (#ffffbf) at the 50% midpoint.
// The exact 11-stop table lets the legend bar in the CSS gradient
// match each tile pixel-for-pixel.
const RD_YL_GN_R = [
  [0.0,  '#006837'],
  [0.1,  '#1a9850'],
  [0.2,  '#66bd63'],
  [0.3,  '#a6d96a'],
  [0.4,  '#d9ef8b'],
  [0.5,  '#ffffbf'],
  [0.6,  '#fee08b'],
  [0.7,  '#fdae61'],
  [0.8,  '#f46d43'],
  [0.9,  '#d73027'],
  [1.0,  '#a50026'],
];

function coreColor(percent) {
  const p = clamp(percent, 0, 100) / 100;
  for (let i = 0; i < RD_YL_GN_R.length - 1; i++) {
    const [t0, c0] = RD_YL_GN_R[i];
    const [t1, c1] = RD_YL_GN_R[i + 1];
    if (p <= t1) {
      return interpolateHex(c0, c1, (p - t0) / (t1 - t0));
    }
  }
  return RD_YL_GN_R[RD_YL_GN_R.length - 1][1];
}

function interpolateHex(hex0, hex1, f) {
  const r0 = parseInt(hex0.slice(1, 3), 16);
  const g0 = parseInt(hex0.slice(3, 5), 16);
  const b0 = parseInt(hex0.slice(5, 7), 16);
  const r1 = parseInt(hex1.slice(1, 3), 16);
  const g1 = parseInt(hex1.slice(3, 5), 16);
  const b1 = parseInt(hex1.slice(5, 7), 16);
  return `rgb(${Math.round(r0 + (r1 - r0) * f)}, ${Math.round(g0 + (g1 - g0) * f)}, ${Math.round(b0 + (b1 - b0) * f)})`;
}

// Pick near-black or white text based on the background luminance so
// the core number stays readable on every RdYlGn_r stop. The yellow
// midpoint at ~50% is the only stop that needs dark text; the rest of
// the colormap is dark enough to take white. Uses the sRGB relative
// luminance approximation (fast, good enough for threshold selection).
function coreTextColor(rgb) {
  const m = rgb.match(/\d+/g);
  if (!m) return '#ffffff';
  const r = +m[0], g = +m[1], b = +m[2];
  const lum = (0.299 * r + 0.587 * g + 0.114 * b) / 255;
  return lum > 0.6 ? '#1a1a1a' : '#ffffff';
}

function clamp(n, lo, hi) {
  return Math.max(lo, Math.min(hi, Number(n) || 0));
}

function renderMemory(sub, valueEl, metaEl, canvas, mem) {
  if (!mem || mem.error) {
    setValue(valueEl, 'no data', 'err');
    if (metaEl) metaEl.textContent = mem && mem.error ? mem.error : '';
    setState(sub, 'err');
    return;
  }

  // htop-style stacked bar: used / buffers / cached / available
  // segments side-by-side. The "used" segment is htop's "real used"
  // (Total - Free - Buffers - Cached), exposed by the server as
  // used_no_cache_bytes. Using gopsutil.Used here would double-count
  // the cache and the bar would visually exceed 100%. The "used"
  // segment's colour reflects pressure (green → amber → red).
  const usedNoCache = mem.used_no_cache_bytes ?? 0;
  const usedPct = mem.total_bytes > 0 ? (usedNoCache / mem.total_bytes) * 100 : 0;

  const membar = sub.querySelector(MEMBAR_SELECTOR);
  if (membar) renderMembar(membar, mem);

  // Big number: htop-style "used" as a percent of total.
  setValue(valueEl, `${usedPct.toFixed(0)}%`);

  // Meta: compact breakdown of every segment in human-readable units.
  if (metaEl) {
    const parts = [
      `${fmtBytes(usedNoCache)} / ${fmtBytes(mem.total_bytes)}`,
      `${fmtBytes(mem.available_bytes)} available`,
    ];
    if (mem.buffers_bytes) parts.push(`${fmtBytes(mem.buffers_bytes)} buffers`);
    if (mem.cached_bytes) parts.push(`${fmtBytes(mem.cached_bytes)} cached`);
    if (mem.shared_bytes) parts.push(`${fmtBytes(mem.shared_bytes)} shared`);
    metaEl.textContent = parts.join(' · ');
  }

  setState(sub, 'ok');
  // Sparkline tracks the htop-style "used" percent so the trend line
  // tells a coherent story with the bar above it.
  if (canvas) {
    const hist = pushPoint(canvas, usedPct);
    ensureChart(canvas, chartArgs(hist, '#3fb950'));
  }
}

// renderMembar fills the memory sub-panel's data-wsp-membar element
// with a htop-style stacked bar. The coloured segments are used,
// buffers, and cached. The black "available" segment is rendered last
// with flex:1 so it always fills the remainder and owns the right
// rounded corner. It represents kernel-available memory, not strictly
// MemFree; Linux treats reclaimable cache as available too. Shared is
// shown in the meta line as a number rather than as a bar segment
// because it overlaps the cached segment and htop shows it the same way.
//
// Segments smaller than 0.5% are skipped so a 64 KB buffer next to
// a 32 GB total doesn't render as a 1-pixel sliver.
function renderMembar(container, mem) {
  const total = mem.total_bytes;
  if (!total) {
    container.innerHTML = '';
    return;
  }
  const used = mem.used_no_cache_bytes ?? 0;
  const usedPct = total > 0 ? (used / total) * 100 : 0;
  const segs = [
    {
      key: 'used',
      bytes: used,
      color: pressureColor(usedPct),
    },
    { key: 'buffers', bytes: mem.buffers_bytes || 0, color: '#388bfd' },
    { key: 'cached', bytes: mem.cached_bytes || 0, color: '#d29922' },
  ];
  const inner = segs
    .map((s) => {
      const pct = (s.bytes / total) * 100;
      if (pct < 0.5) return '';
      return `<div class="${SUB_CLASS}-membar-seg" data-seg="${s.key}" style="width:${pct.toFixed(2)}%;background:${s.color}"></div>`;
    })
    .join('');
  const available = `<div class="${SUB_CLASS}-membar-seg" data-seg="available" style="flex:1 1 0;background:#21262d"></div>`;
  container.innerHTML = `<div class="${SUB_CLASS}-membar-track">${inner}${available}</div>`;
}

// pressureColor maps a used% to a traffic-light hex. Thresholds match
// Linux's notion of "you should worry about this" (60% amber, 80% red).
function pressureColor(percent) {
  if (percent < 60) return '#3fb950'; // green
  if (percent < 80) return '#d29922'; // amber
  return '#f85149';                   // red
}

function renderGpu(sub, grid, metaEl, _canvas, gpu, profileGpu) {
  // Four mutually exclusive states. Each writes the meta line and
  // either renders the cell grid or a centred placeholder in the
  // data-wsp-gpu container.
  if (!gpu) {
    const reason = profileGpu && profileGpu.enabled === false && profileGpu.disabled_reason
      ? profileGpu.disabled_reason
      : '';
    renderGpuPlaceholder(grid, 'disabled', reason);
    if (metaEl) metaEl.textContent = reason || 'disabled';
    setState(sub, 'err');
    return;
  }
  if (gpu.error) {
    renderGpuPlaceholder(grid, 'no data', gpu.error);
    if (metaEl) metaEl.textContent = gpu.error;
    setState(sub, 'err');
    return;
  }
  if (!gpu.devices || gpu.devices.length === 0) {
    const expected = typeof profileGpu?.device_count === 'number' ? `${profileGpu.device_count} profiled` : '';
    renderGpuPlaceholder(grid, 'no devices', expected);
    if (metaEl) metaEl.textContent = expected || '0 GPUs';
    setState(sub, 'err');
    return;
  }
  renderGpuCells(grid, gpu.devices);
  if (metaEl) {
    const n = gpu.devices.length;
    let maxGpuUtil = -Infinity;
    let maxTemp = -Infinity;
    let totalPower = 0;
    for (const d of gpu.devices) {
      if (typeof d.utilization_gpu_percent === 'number' && d.utilization_gpu_percent > maxGpuUtil) {
        maxGpuUtil = d.utilization_gpu_percent;
      }
      if (typeof d.temperature_c === 'number' && d.temperature_c > maxTemp) {
        maxTemp = d.temperature_c;
      }
      if (typeof d.power_draw_watts === 'number') {
        totalPower += d.power_draw_watts;
      }
    }
    const parts = [`${n} ${n === 1 ? 'GPU' : 'GPUs'}`];
    if (isFinite(maxGpuUtil) && maxGpuUtil >= 0) parts.push(`max GPU ${maxGpuUtil.toFixed(0)}%`);
    if (isFinite(maxTemp) && maxTemp >= 0) parts.push(`max ${maxTemp.toFixed(0)}°C`);
    if (totalPower > 0) parts.push(`${totalPower.toFixed(0)} W`);
    metaEl.textContent = parts.join(' · ');
  }
  setState(sub, 'ok');
}

// gpuStatusPercent returns max(gpu_util_pct, memory_occupancy_pct,
// power_draw_pct). When a denominator is 0 (e.g. consumer cards with
// no power cap, or a device whose mem total is 0), that leg contributes
// 0 — the max then falls back to whichever signal is available.
function gpuStatusPercent(dev) {
  const gpuPct = typeof dev.utilization_gpu_percent === 'number'
    ? clamp(dev.utilization_gpu_percent, 0, 100)
    : 0;
  const memPct = dev.memory_total_bytes > 0
    ? (dev.memory_used_bytes / dev.memory_total_bytes) * 100
    : 0;
  const powerPct = dev.power_limit_watts > 0
    ? (dev.power_draw_watts / dev.power_limit_watts) * 100
    : 0;
  return Math.max(gpuPct, memPct, powerPct);
}

// gpuStatus maps the percentage to one of three status buckets.
// Thresholds:
//   < 10%  → idle    (green)
//   10-60% → working (amber)
//   ≥ 60%  → busy    (red)
function gpuStatus(pct) {
  if (pct < 10) return 'idle';
  if (pct < 60) return 'working';
  return 'busy';
}

// renderGpuPlaceholder writes a single centred message into the GPU
// grid container. Used for the disabled / error / no-devices states.
function renderGpuPlaceholder(container, text, detail = '') {
  if (!container) return;
  const detailHTML = detail ? `<span class="${SUB_CLASS}-gpu-empty-detail">${escapeHtml(detail)}</span>` : '';
  container.innerHTML = `
    <div class="${SUB_CLASS}-gpu-empty">
      <span class="${SUB_CLASS}-gpu-empty-title">${escapeHtml(text)}</span>
      ${detailHTML}
    </div>
  `;
}

// renderGpuCells writes one neutral card per device into the GPU grid.
// Each card carries data-status for a small dot / edge treatment, then
// renders a compact header and four horizontal meters (GPU, VRAM, PWR,
// TEMP). The large status-colour background is deliberately avoided; it
// made the panel noisy when any GPU crossed a threshold.
function renderGpuCells(container, devices) {
  if (!container) return;
  container.innerHTML = devices.map(renderGpuCell).join('');
}

function renderGpuCell(dev) {
  const pct = gpuStatusPercent(dev);
  const status = dev.error ? 'error' : gpuStatus(pct);
  const statusColor = gpuStatusColor(status);
  const idx = escapeHtml(String(dev.index ?? '?'));
  const name = escapeHtml(dev.name || '');
  const gpuPct = typeof dev.utilization_gpu_percent === 'number'
    ? clamp(dev.utilization_gpu_percent, 0, 100)
    : 0;
  const gpuUtil = formatPercent(gpuPct);
  const temp = formatTemp(dev.temperature_c);
  const memPair = formatMem(dev.memory_used_bytes, dev.memory_total_bytes);
  const power = formatPower(dev.power_draw_watts);
  const memPct = dev.memory_total_bytes > 0
    ? clamp((dev.memory_used_bytes / dev.memory_total_bytes) * 100, 0, 100)
    : 0;
  const powerPct = dev.power_limit_watts > 0
    ? clamp((dev.power_draw_watts / dev.power_limit_watts) * 100, 0, 100)
    : 0;
  const powerDetail = dev.power_limit_watts > 0
    ? `${power} / ${dev.power_limit_watts.toFixed(0)} W`
    : power;
  const error = dev.error
    ? `<div class="${SUB_CLASS}-gpu-cell-error">${escapeHtml(dev.error)}</div>`
    : '';
  return `
    <div class="${SUB_CLASS}-gpu-cell" data-status="${status}" style="--wsp-gpu-status:${statusColor}">
      <div class="${SUB_CLASS}-gpu-cell-top">
        <div class="${SUB_CLASS}-gpu-cell-id">
          <span class="${SUB_CLASS}-gpu-cell-idx">${idx}</span>
          <span class="${SUB_CLASS}-gpu-cell-dot"></span>
          <span class="${SUB_CLASS}-gpu-cell-name" title="${name}">${name}</span>
        </div>
        <div class="${SUB_CLASS}-gpu-cell-kpis">
          <span><span>TEMP</span><strong style="color:${temp.color}">${temp.text}</strong></span>
        </div>
      </div>
      ${error}
      <div class="${SUB_CLASS}-gpu-meters">
        ${renderGpuMeter('GPU', gpuUtil, gpuPct, gpuStatusColor(gpuStatus(gpuPct)))}
        ${renderGpuMeter('VRAM', memPair.usedText, memPct, memPressureColor(memPct), memPair.subText)}
        ${renderGpuMeter('PWR', powerDetail, powerPct, powerPct > 0 ? memPressureColor(powerPct) : 'var(--wsp-fg-faint)', dev.power_limit_watts > 0 ? `${powerPct.toFixed(0)}%` : 'no cap')}
      </div>
    </div>
  `;
}

function renderGpuMeter(label, value, percent, color, detail = '') {
  const pct = clamp(percent, 0, 100);
  const detailText = detail ? ` <span class="${SUB_CLASS}-gpu-meter-detail">${escapeHtml(detail)}</span>` : '';
  return `
    <div class="${SUB_CLASS}-gpu-meter">
      <div class="${SUB_CLASS}-gpu-meter-head">
        <span class="${SUB_CLASS}-gpu-meter-label">${escapeHtml(label)}</span>
        <span class="${SUB_CLASS}-gpu-meter-value">${escapeHtml(value)}${detailText}</span>
      </div>
      <div class="${SUB_CLASS}-gpu-meter-track">
        <div class="${SUB_CLASS}-gpu-meter-fill" style="width:${pct.toFixed(2)}%;background:${color}"></div>
      </div>
    </div>
  `;
}

function gpuStatusColor(status) {
  switch (status) {
    case 'idle':
      return 'var(--wsp-ok)';
    case 'working':
      return 'var(--wsp-warn)';
    case 'busy':
    case 'error':
      return 'var(--wsp-err)';
    default:
      return 'var(--wsp-fg-faint)';
  }
}

// formatTemp returns the temperature as a short string and the
// pressure colour: <65 green, 65-80 amber, ≥80 red. NVML "shutdown"
// is typically 95°C; we use 80 as the visual-warn threshold so the
// cell text starts to flag before throttle.
function formatTemp(c) {
  if (typeof c !== 'number' || c <= 0) return { text: '—', color: 'var(--wsp-fg-faint)' };
  const text = `${c.toFixed(0)}°C`;
  let color;
  if (c < 65) color = 'var(--wsp-ok)';
  else if (c < 80) color = 'var(--wsp-warn)';
  else color = 'var(--wsp-err)';
  return { text, color };
}

function formatMem(used, total) {
  if (!total || total <= 0) return { usedText: '—', subText: '—' };
  const usedText = `${fmtBytes(used || 0)} / ${fmtBytes(total)}`;
  const pct = ((used || 0) / total) * 100;
  const subText = `${pct.toFixed(0)}%`;
  return { usedText, subText };
}

function formatPower(w) {
  if (typeof w !== 'number' || w <= 0) return '—';
  return `${w.toFixed(0)} W`;
}

function formatPercent(pct) {
  if (typeof pct !== 'number' || pct < 0) return '—';
  return `${clamp(pct, 0, 100).toFixed(0)}%`;
}

// memPressureColor mirrors pressureColor() in spirit but is local to
// the GPU cell so the GPU cell's MEM bar is visually independent from
// the global memory module's membar. Same 60/80 thresholds.
function memPressureColor(percent) {
  if (percent < 60) return '#3fb950';
  if (percent < 80) return '#d29922';
  return '#f85149';
}

function renderStorage(sub, _valueEl, metaEl, _canvas, storage) {
  if (!storage || storage.error || !storage.disks || storage.disks.length === 0) {
    if (metaEl) metaEl.textContent = storage && storage.error ? storage.error : 'no mounts';
    setState(sub, 'err');
    return;
  }

  // Per-mount row layout: alias · path · (thick used + thin free) bar · percent.
  // The bar visually shows capacity vs. usage; colour thresholds for
  // used follow the spec — green (<70%), amber (70–90%), red (≥90%).
  const storageEl = sub.querySelector(STORAGE_SELECTOR);
  if (storageEl) renderStorageRows(storageEl, storage.disks);

  if (metaEl) {
    const n = storage.disks.length;
    metaEl.textContent = `${n} ${n === 1 ? 'mount' : 'mounts'}`;
  }

  setState(sub, 'ok');
}

// renderStorageRows fills the storage sub-panel's data-wsp-storage
// element with one row per mount. CSS makes every row participate in
// one shared grid so alias, path, bar, and size columns align to the
// widest value in the list. The bar itself contains a thick coloured
// "used" segment and a thin grey "free" segment that share the same
// horizontal extent.
function renderStorageRows(container, disks) {
  const html = disks.map((d) => {
    const pct = clamp(d.used_percent || 0, 0, 100);
    const freePct = 100 - pct;
    const usedColor = storagePressureColor(pct);
    const alias = d.alias || d.path || '?';
    const path = d.path || '';
    const fsType = d.fs_type || '';
    const pair = fmtBytesPair(d.used_bytes || 0, d.total_bytes || 0);
    const sizeText = pair.unit.includes('/')
      ? `${pair.used} ${pair.unit}  /  ${pair.total} ${pair.unit.split(' / ')[1]}`
      : `${pair.used} / ${pair.total} ${pair.unit}`;
    const tip = `${pair.used} / ${pair.total} ${pair.unit.replace(' / ', ' / ')}${fsType ? ' · ' + fsType : ''}`;
    return `
      <div class="${SUB_CLASS}-row" title="${escapeHtml(tip)}">
        <span class="${SUB_CLASS}-row-alias">${escapeHtml(alias)}</span>
        <span class="${SUB_CLASS}-row-path" title="${escapeHtml(path)}">${escapeHtml(path)}</span>
        <div class="${SUB_CLASS}-row-bar">
          <div class="${SUB_CLASS}-row-bar-track">
            <div class="${SUB_CLASS}-row-bar-used" style="width:${pct.toFixed(2)}%;background:${usedColor}"></div>
            <div class="${SUB_CLASS}-row-bar-free" style="width:${freePct.toFixed(2)}%"></div>
          </div>
        </div>
        <span class="${SUB_CLASS}-row-pct" style="color:${usedColor}">${escapeHtml(sizeText)}</span>
      </div>
    `;
  }).join('');
  container.innerHTML = html;
}

// storagePressureColor maps a used% to a traffic-light hex. Thresholds
// match the user spec: green at <70%, amber at 70–90%, red at ≥90%.
function storagePressureColor(percent) {
  if (percent >= 90) return '#f85149'; // red
  if (percent >= 70) return '#d29922'; // amber
  return '#3fb950';                     // green
}

function setValue(valueEl, text, state) {
  if (!valueEl) return;
  valueEl.textContent = text;
  if (state) {
    valueEl.setAttribute('data-state', state);
    valueEl.removeAttribute('data-loading');
  } else {
    valueEl.removeAttribute('data-state');
    valueEl.removeAttribute('data-loading');
  }
}
function escapeHtml(s) {
  return String(s).replace(/[&<>"]/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' }[c]));
}

// fmtBytesPair returns "X / Y unit" where unit is chosen so the total
// value reads as a normal number (≥ 1, < 1024). The used value is
// shown in the same unit. If the used value would round to 0 in that
// unit, a smaller unit is used for it instead so the bar's numeric
// label stays informative. The "0.X" trailing zero is stripped so
// "1.0" displays as "1".
function fmtBytesPair(used, total) {
  if (!total || total <= 0) return { used: '0', total: '0', unit: 'B' };
  const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'];
  let i = 0;
  let v_total = total;
  while (v_total >= 1024 && i < units.length - 1) {
    v_total /= 1024;
    i++;
  }
  const v_used = (used || 0) / Math.pow(1024, i);
  let unit = units[i];
  let used_val = fmtNum(v_used);
  // If the used value would round to 0 in the chosen unit, fall back
  // to a smaller unit just for the used number. The two units shown
  // side-by-side is honest: the file is small relative to the disk.
  if (v_used < 0.05 && i > 0) {
    const smaller = (used || 0) / Math.pow(1024, i - 1);
    if (smaller >= 0.05) {
      used_val = fmtNum(smaller);
      unit = `${units[i - 1]} / ${units[i]}`;
    }
  }
  return { used: used_val, total: fmtNum(v_total), unit };
}

function fmtNum(v) {
  if (!isFinite(v) || v <= 0) return '0';
  if (v >= 100) return v.toFixed(0);
  return v.toFixed(1).replace(/\.0$/, '');
}
