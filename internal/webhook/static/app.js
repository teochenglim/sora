const pill = document.getElementById("status-pill");
const statusGrid = document.getElementById("status-grid");
const metricsGrid = document.getElementById("metrics-grid");
const configBlock = document.getElementById("config-block");

function fmtUptime(seconds) {
  const d = Math.floor(seconds / 86400);
  const h = Math.floor((seconds % 86400) / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  const s = Math.floor(seconds % 60);
  return [d ? `${d}d` : "", h ? `${h}h` : "", m ? `${m}m` : "", `${s}s`]
    .filter(Boolean)
    .join(" ");
}

function fmtLabels(labels) {
  const entries = Object.entries(labels || {});
  if (entries.length === 0) return "";
  return entries.map(([k, v]) => `${k}="${v}"`).join(", ");
}

async function refreshStats() {
  try {
    const res = await fetch("/api/stats");
    if (!res.ok) throw new Error(`status ${res.status}`);
    const data = await res.json();

    pill.textContent = `mode=${data.mode} · up ${fmtUptime(data.uptime_seconds)}`;
    pill.className = "pill ok";

    statusGrid.innerHTML = "";
    const fields = [
      ["Mode", data.mode],
      ["Version", data.version],
      ["Uptime", fmtUptime(data.uptime_seconds)],
      ["Started at", new Date(data.started_at).toLocaleString()],
    ];
    for (const [label, value] of fields) {
      const dt = document.createElement("dt");
      dt.textContent = label;
      const dd = document.createElement("dd");
      dd.textContent = value;
      statusGrid.append(dt, dd);
    }

    metricsGrid.innerHTML = "";
    for (const m of data.metrics || []) {
      const el = document.createElement("div");
      el.className = "metric";
      el.innerHTML = `
        <div class="name">${m.name}</div>
        <div class="labels">${fmtLabels(m.labels)}</div>
        <div class="value">${m.value}</div>
      `;
      metricsGrid.appendChild(el);
    }
  } catch (err) {
    pill.textContent = `error: ${err.message}`;
    pill.className = "pill error";
  }
}

async function refreshConfig() {
  try {
    const res = await fetch("/api/config");
    if (!res.ok) throw new Error(`status ${res.status}`);
    const data = await res.json();
    configBlock.textContent = JSON.stringify(data, null, 2);
  } catch (err) {
    configBlock.textContent = `error loading config: ${err.message}`;
  }
}

refreshStats();
refreshConfig();
setInterval(refreshStats, 5000);
setInterval(refreshConfig, 30000);
