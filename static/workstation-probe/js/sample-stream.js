// sample-stream.js — 60-second loop test data generator for the
// workstation-probe webview demo. Produces a CombinedSample and a
// ProfileResponse matching the real server's JSON shape so the
// existing renderer works without modification.
//
// Trigger: when a panel config has api === "样例流数据", the poller
// calls this module instead of fetch().

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const LOOP_SECONDS = 60;
const TWO_PI = 2 * Math.PI;

const GB = 1024 * 1024 * 1024;
const TB = 1024 * GB;

// CPU
const CPU_CORES = 32;
const EPOCHS = 6;
const EPOCH_SEC = LOOP_SECONDS / EPOCHS; // 10s
const FADE_SEC = 2; // transition between epochs

// Memory
const MEM_TOTAL = 512 * GB; // 512 GB
const SWAP_TOTAL = 8 * GB;

// GPU
const GPU_MEM_TOTAL = 24 * GB;
const GPU_NAME = "Simulated Accelerator SG-24000";
const GPU_UUID = "GPU-SIM-SG24000-0001";

// Storage
const STORAGE_TOTAL = 1 * TB;
const STORAGE_USED = 0.42 * TB;

// ---------------------------------------------------------------------------
// Smooth signal helpers
// ---------------------------------------------------------------------------

// smoothSig returns a value in [mean-range, mean+range] that varies
// smoothly over time using a sum of two sinusoids.
function smoothSig(t, mean, range, freq1, phase1, freq2, phase2) {
  const w1 = 0.6, w2 = 0.4;
  const v = w1 * Math.sin(TWO_PI * t * freq1 + phase1) +
            w2 * Math.sin(TWO_PI * t * freq2 + phase2);
  return mean + range * v;
}

// clamp the result
function cl(val, lo, hi) {
  return Math.max(lo, Math.min(hi, val));
}

// smoothstep cubic
function smoothstep(t) {
  const x = cl(t, 0, 1);
  return x * x * (3 - 2 * x);
}

// ---------------------------------------------------------------------------
// Deterministic pseudo-random (seeded), used for epoch busy-core selection
// ---------------------------------------------------------------------------

function mulberry32(a) {
  return function() {
    a |= 0; a = a + 0x6D2B79F5 | 0;
    let t = Math.imul(a ^ a >>> 15, 1 | a);
    t = t + Math.imul(t ^ t >>> 7, 61 | t) ^ t;
    return ((t ^ t >>> 14) >>> 0) / 4294967296;
  };
}

// ---------------------------------------------------------------------------
// CPU busymap: for each epoch (0..5), deterministically pick 3-8 busy cores
// ---------------------------------------------------------------------------

function buildBusyPlan() {
  const rng = mulberry32(0xCAFE2026);
  const plan = [];
  for (let ep = 0; ep < EPOCHS; ep++) {
    const count = 3 + Math.floor(rng() * 6); // 3-8
    const busy = new Set();
    while (busy.size < count) {
      busy.add(Math.floor(rng() * CPU_CORES));
    }
    plan.push(busy);
  }
  return plan;
}

const BUSY_PLAN = buildBusyPlan();

// busyWeight(core, t) → [0, 1] how "busy" this core is right now
function busyWeight(core, t) {
  const epoch = Math.floor(t / EPOCH_SEC);
  const offset = t - epoch * EPOCH_SEC; // 0..10 within epoch

  const cur = BUSY_PLAN[cl(epoch, 0, EPOCHS - 1)];
  const next = BUSY_PLAN[(epoch + 1) % EPOCHS];

  const inCur = cur.has(core) ? 1 : 0;
  const inNext = next.has(core) ? 1 : 0;

  if (inCur === inNext) return inCur;

  // Fade transition
  if (offset < FADE_SEC) {
    const fade = smoothstep(1 - offset / FADE_SEC);
    return inCur * fade + inNext * (1 - fade);
  }
  if (offset > EPOCH_SEC - FADE_SEC) {
    const fade = smoothstep((offset - (EPOCH_SEC - FADE_SEC)) / FADE_SEC);
    return inCur * (1 - fade) + inNext * fade;
  }
  return inCur;
}

// coreUtil: per-core utilization at time t
function coreUtil(core, t) {
  const w = busyWeight(core, t);
  // busy core: 55-95% base, idle core: 1-8% base
  const baseIdle = 2 + smoothSig(t + core * 0.7, 3, 4, 0.03, core * 0.31, 0.07, core * 0.53);
  const busyExtra = 55 + smoothSig(t + core * 0.4, 15, 30, 0.05, core * 0.27, 0.12, core * 0.61);
  return cl(baseIdle + w * busyExtra, 0.5, 100);
}

// ---------------------------------------------------------------------------
// Memory signals
// ---------------------------------------------------------------------------

function memAvailable(t) {
  // mean ~0.40 of total, range ±0.05
  return smoothSig(t, 0.40 * MEM_TOTAL, 0.05 * MEM_TOTAL, 1/45, 0.3, 1/17, 1.7);
}

function memBuffers(t) {
  return smoothSig(t, 0.04 * MEM_TOTAL, 0.02 * MEM_TOTAL, 1/50, 2.1, 1/23, 0.8);
}

function memCached(t) {
  return smoothSig(t, 0.18 * MEM_TOTAL, 0.06 * MEM_TOTAL, 1/38, 0.7, 1/14, 2.5);
}

function memShared(t) {
  return smoothSig(t, 0.02 * MEM_TOTAL, 0.01 * MEM_TOTAL, 1/55, 3.2, 1/19, 1.1);
}

function swapUsed(t) {
  return smoothSig(t, 0.5 * GB, 0.4 * GB, 1/60, 1.3, 1/25, 2.9);
}

// ---------------------------------------------------------------------------
// GPU signals
// ---------------------------------------------------------------------------

function gpuUtil(t) {
  return cl(smoothSig(t, 45, 25, 1/35, 0.5, 1/8, 2.0), 5, 98);
}

function gpuMemUtil(t) {
  return cl(smoothSig(t, 50, 20, 1/42, 1.8, 1/11, 0.3), 10, 90);
}

function gpuMemUsed(t) {
  // VRAM almost fixed: 14 GB ± 50 MB
  return cl(smoothSig(t, 14 * GB, 50 * 1024 * 1024, 1/90, 0.9, 1/120, 2.3), 13.9 * GB, 14.1 * GB);
}

function gpuTemp(t) {
  return cl(smoothSig(t, 58, 13, 1/30, 1.0, 1/12, 3.0), 38, 78);
}

function gpuPower(t) {
  return cl(smoothSig(t, 150, 60, 1/28, 0.4, 1/15, 2.7), 60, 240);
}

// ---------------------------------------------------------------------------
// Storage — static
// ---------------------------------------------------------------------------

function storageSample(now) {
  return {
    timestamp: now.toISOString(),
    disks: [{
      path: "/",
      alias: "root",
      total_bytes: STORAGE_TOTAL,
      used_bytes: STORAGE_USED,
      free_bytes: STORAGE_TOTAL - STORAGE_USED,
      used_percent: (STORAGE_USED / STORAGE_TOTAL) * 100,
      fs_type: "ext4",
      error: "",
    }],
    error: "",
  };
}

// ---------------------------------------------------------------------------
// Public API
// ---------------------------------------------------------------------------

// generateMetrics returns a CombinedSample for the given timestamp.
export function generateMetrics(nowMs) {
  const t = (nowMs / 1000) % LOOP_SECONDS;
  const now = new Date(nowMs);

  // --- CPU ---
  const perCore = [];
  let sum = 0;
  for (let i = 0; i < CPU_CORES; i++) {
    const p = coreUtil(i, t);
    perCore.push(p);
    sum += p;
  }
  const cpuSample = {
    timestamp: now.toISOString(),
    overall_percent: +((sum / CPU_CORES).toFixed(2)),
    per_core_percent: perCore.map((v) => +v.toFixed(2)),
    core_count: CPU_CORES,
    error: "",
  };

  // --- Memory ---
  const available = memAvailable(t);
  const buffers = memBuffers(t);
  const cached = memCached(t);
  const shared = memShared(t);
  const used = MEM_TOTAL - available;
  const usedNoCache = Math.max(0, used - buffers - cached);
  const usedPercent = (used / MEM_TOTAL) * 100;
  const swapUsedBytes = swapUsed(t);

  const memSample = {
    timestamp: now.toISOString(),
    total_bytes: MEM_TOTAL,
    used_bytes: Math.round(used),
    used_no_cache_bytes: Math.round(usedNoCache),
    available_bytes: Math.round(available),
    used_percent: +usedPercent.toFixed(2),
    swap_total_bytes: SWAP_TOTAL,
    swap_used_bytes: Math.round(swapUsedBytes),
    buffers_bytes: Math.round(buffers),
    cached_bytes: Math.round(cached),
    shared_bytes: Math.round(shared),
    error: "",
  };

  // --- GPU ---
  const gpuUtilVal = gpuUtil(t);
  const gpuMemUtilVal = gpuMemUtil(t);
  const gpuMemUsedVal = gpuMemUsed(t);
  const gpuTempVal = gpuTemp(t);
  const gpuPowerVal = gpuPower(t);

  const gpuSample = {
    timestamp: now.toISOString(),
    devices: [{
      index: 0,
      uuid: GPU_UUID,
      name: GPU_NAME,
      utilization_gpu_percent: +gpuUtilVal.toFixed(2),
      utilization_memory_percent: +gpuMemUtilVal.toFixed(2),
      memory_total_bytes: GPU_MEM_TOTAL,
      memory_used_bytes: Math.round(gpuMemUsedVal),
      temperature_c: +gpuTempVal.toFixed(1),
      power_draw_watts: +gpuPowerVal.toFixed(1),
      power_limit_watts: 250,
      error: "",
    }],
    error: "",
  };

  // --- Storage (static) ---
  const storage = storageSample(now);

  return {
    server_time_local: now.toLocaleString("en-CA", { timeZone: "Asia/Shanghai", hour12: false }).replace(/,/, ""),
    server_time_unix_seconds: Math.floor(nowMs / 1000),
    cpu: cpuSample,
    memory: memSample,
    gpu: gpuSample,
    storage: storage,
  };
}

// generateProfile returns a static ProfileResponse.
export function generateProfile() {
  const now = new Date();
  const bootUnix = Math.floor(now.getTime() / 1000) - 86400 * 3; // booted 3 days ago
  const bootDate = new Date(bootUnix * 1000);

  return {
    hostname: "demo-workstation",
    go_version: "go1.25.0",
    started_at: new Date(bootUnix * 1000 + 3600 * 1000).toISOString(),
    server_timezone: "CST",
    server_timezone_offset_seconds: 28800,
    host_boot_time_unix_seconds: bootUnix,
    host_boot_time_local: bootDate.toLocaleString("en-CA", { timeZone: "Asia/Shanghai", hour12: false }).replace(/,/, ""),
    sampler: {
      interval_ms: 1000,
      history_capacity: 60,
    },
    modules: {
      cpu: {
        enabled: true,
        model_name: "Simulated CPU v2 (32-core)",
        vendor: "Simulink",
        architecture: "amd64",
        core_count: CPU_CORES,
        startup_error: "",
      },
      memory: {
        enabled: true,
        total_bytes: MEM_TOTAL,
        startup_error: "",
      },
      gpu: {
        enabled: true,
        disabled_reason: "",
        device_count: 1,
        devices: [{
          index: 0,
          uuid: GPU_UUID,
          name: GPU_NAME,
          memory_total_bytes: GPU_MEM_TOTAL,
        }],
      },
      storage: {
        enabled: true,
        mount_points: [{
          path: "/",
          alias: "root",
          device: "/dev/nvme0n1p2",
          fstype: "ext4",
        }],
      },
    },
  };
}
