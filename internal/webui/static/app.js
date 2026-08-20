const $ = (id) => document.getElementById(id);

let defaults = null;
let snapshot = null;
let resultFilter = "all";
let directoryState = null;

const stateLabels = {
  idle: "待機中", running: "実行中", stopping: "停止待ち", completed: "完了",
  stopped: "停止済み", cancelled: "キャンセル", failed: "失敗",
};
const phaseLabels = {
  discovery: "候補を探索中", probe: "メディアを確認中", quality: "品質を解析中",
  encode: "エンコード中", validation: "出力を検証中", publish: "安全に公開中",
};
const resultLabels = { converted: "変換", skipped: "スキップ", failed: "失敗", cancelled: "キャンセル" };

async function request(path, options = {}) {
  const response = await fetch(path, options);
  const body = await response.json().catch(() => ({}));
  if (!response.ok) throw new Error(body.error || `${response.status} ${response.statusText}`);
  return body;
}

function setNotice(message = "") {
  $("notice").hidden = !message;
  $("notice").textContent = message;
}

function applyDefaults(config) {
  defaults = config;
  $("preset").value = config.preset;
  $("analysisPreset").value = config.analysis_preset || "";
  $("qualityMetric").value = config.quality_metric;
  $("vmafAverage").value = config.vmaf_average_min;
  $("vmafWorst").value = config.vmaf_worst_min;
  $("ssimAverage").value = config.ssim_average_min;
  $("ssimWorst").value = config.ssim_worst_min;
  $("sampleDuration").value = config.sample_duration;
  $("sampleCount").value = config.sample_count;
  $("minSavings").value = config.min_savings;
  $("fullDecodeCheck").checked = config.full_decode_check;
  $("keepOriginal").checked = config.keep_original;
  $("dryRun").checked = config.dry_run;
  $("ffmpegPath").value = config.ffmpeg_path;
  $("ffprobePath").value = config.ffprobe_path;
  $("qualityMode").value = "auto";
  updateQualityMode();
}

function updateQualityMode() {
  const mode = $("qualityMode").value;
  $("qualityValueField").hidden = mode === "auto";
  $("metricField").hidden = mode !== "auto";
  $("qualityValueLabel").textContent = mode === "cq" ? "CQ (0–51)" : "CRF (0–51)";
}

function jobConfig() {
  const mode = $("qualityMode").value;
  const quality = Number.parseInt($("qualityValue").value, 10);
  return {
    ...defaults,
    root: $("root").value.trim(),
    preset: $("preset").value.trim(),
    analysis_preset: $("analysisPreset").value.trim(),
    direct_crf: mode === "crf" ? quality : null,
    direct_cq: mode === "cq" ? quality : null,
    quality_metric: $("qualityMetric").value,
    vmaf_average_min: Number($("vmafAverage").value),
    vmaf_worst_min: Number($("vmafWorst").value),
    ssim_average_min: Number($("ssimAverage").value),
    ssim_worst_min: Number($("ssimWorst").value),
    sample_duration: Number($("sampleDuration").value),
    sample_count: Number.parseInt($("sampleCount").value, 10),
    min_savings: Number($("minSavings").value),
    full_decode_check: $("fullDecodeCheck").checked,
    keep_original: $("keepOriginal").checked,
    dry_run: $("dryRun").checked,
    ffmpeg_path: $("ffmpegPath").value.trim(),
    ffprobe_path: $("ffprobePath").value.trim(),
  };
}

function formatBytes(value) {
  if (!value) return "0 B";
  const units = ["B", "KiB", "MiB", "GiB", "TiB"];
  let amount = Math.abs(value), unit = 0;
  while (amount >= 1024 && unit < units.length - 1) { amount /= 1024; unit += 1; }
  return `${value < 0 ? "−" : ""}${amount.toFixed(unit ? 1 : 0)} ${units[unit]}`;
}

function formatDuration(seconds) {
  if (!Number.isFinite(seconds) || seconds <= 0) return "—";
  const rounded = Math.round(seconds);
  const h = Math.floor(rounded / 3600);
  const m = Math.floor((rounded % 3600) / 60);
  const s = rounded % 60;
  if (h) return `${h}時間 ${m}分`;
  if (m) return `${m}分 ${s}秒`;
  return `${s}秒`;
}

function render(next) {
  snapshot = next;
  const running = next.state === "running" || next.state === "stopping";
  const badge = $("stateBadge");
  badge.className = `state-badge ${next.state}`;
  badge.textContent = stateLabels[next.state] || next.state;
  $("startButton").disabled = running;
  $("stopControls").hidden = !running;
  $("jobForm").querySelectorAll("input, select, button").forEach((control) => {
    if (control.id !== "cancelNow" && control.id !== "gracefulStop") control.disabled = running;
  });

  const current = next.current || {};
  let phaseText = running ? (phaseLabels[current.phase] || "準備中") : stateLabels[next.state];
  if (current.phase === "quality" && current.sample_count) {
    phaseText += ` · 品質値 ${current.quality_value} · サンプル ${current.sample}/${current.sample_count}`;
  }
  $("phaseLabel").textContent = phaseText || "待機中";
  $("currentPath").textContent = current.path || (next.root ? `${next.root} の処理待ち` : "ジョブを開始すると、ここに現在のファイルが表示されます。");
  const percent = next.state === "completed" ? 100 : (current.phase === "encode" ? Math.max(0, Math.min(100, current.percent || 0)) : 0);
  $("percentLabel").textContent = `${percent.toFixed(percent && percent < 10 ? 1 : 0)}%`;
  $("progressBar").style.width = `${percent}%`;
  $("progressBar").parentElement.setAttribute("aria-valuenow", String(percent));
  $("fileETA").textContent = formatDuration(current.eta_seconds);
  $("batchETA").textContent = formatDuration(next.batch_eta_seconds);
  $("speed").textContent = current.speed || "—";

  const summary = next.summary || {};
  $("totalCount").textContent = summary.total || 0;
  $("remainingCount").textContent = Math.max(0, (summary.total || 0) - (summary.processed || 0));
  $("convertedCount").textContent = summary.converted || 0;
  $("skippedCount").textContent = summary.skipped || 0;
  $("failedCount").textContent = summary.failed || 0;
  $("savedBytes").textContent = formatBytes(summary.saved_bytes);
  renderResults(next.results || []);
  const logs = next.logs || [];
  $("logs").textContent = logs.length ? logs.join("\n") : "ログはまだありません。";
  $("logCount").textContent = `${logs.length} 行`;
  if (next.error) setNotice(next.error);
}

function renderResults(results) {
  const filtered = resultFilter === "all" ? results : results.filter((item) => item.status === resultFilter);
  const body = $("resultsBody");
  body.replaceChildren();
  if (!filtered.length) {
    const row = document.createElement("tr");
    row.className = "empty-row";
    const cell = document.createElement("td");
    cell.colSpan = 5;
    cell.textContent = results.length ? "この条件に一致する結果はありません" : "まだ結果はありません";
    row.append(cell);
    body.append(row);
    return;
  }
  for (const item of filtered) {
    const row = document.createElement("tr");
    const statusCell = document.createElement("td");
    const pill = document.createElement("span");
    pill.className = `result-pill ${item.status}`;
    pill.textContent = resultLabels[item.status] || item.status;
    statusCell.append(pill);
    const pathCell = document.createElement("td");
    pathCell.className = "file-cell";
    pathCell.textContent = item.path;
    const reasonCell = document.createElement("td");
    reasonCell.textContent = item.reason || "—";
    const sizeCell = document.createElement("td");
    sizeCell.textContent = item.output_bytes ? `${formatBytes(item.original_bytes)} → ${formatBytes(item.output_bytes)}` : formatBytes(item.original_bytes);
    const savedCell = document.createElement("td");
    savedCell.textContent = formatBytes(item.saved_bytes);
    row.append(statusCell, pathCell, reasonCell, sizeCell, savedCell);
    body.append(row);
  }
}

async function openDirectory(path = "") {
  directoryState = await request(`/api/directories?path=${encodeURIComponent(path)}`);
  $("browserPath").textContent = directoryState.path;
  $("parentButton").disabled = !directoryState.parent;
  const list = $("directoryList");
  list.replaceChildren();
  if (!directoryState.entries.length) {
    const empty = document.createElement("p");
    empty.className = "empty-row";
    empty.textContent = "子ディレクトリはありません";
    list.append(empty);
  }
  for (const entry of directoryState.entries) {
    const button = document.createElement("button");
    button.type = "button";
    button.className = "directory-entry";
    button.textContent = `▸  ${entry.name}`;
    button.addEventListener("click", () => openDirectory(entry.path).catch((error) => setNotice(error.message)));
    list.append(button);
  }
}

$("qualityMode").addEventListener("change", updateQualityMode);
$("resetButton").addEventListener("click", () => applyDefaults(defaults));
$("jobForm").addEventListener("submit", async (event) => {
  event.preventDefault();
  setNotice();
  if (!$("fullDecodeCheck").checked && !confirm("完全デコード検証を無効にします。生成ファイルの安全確認が弱くなりますが、続行しますか？")) return;
  try {
    const next = await request("/api/jobs", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(jobConfig()) });
    render(next);
  } catch (error) { setNotice(error.message); }
});
$("gracefulStop").addEventListener("click", async () => {
  try { render(await request("/api/jobs/stop", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ mode: "after-current" }) })); }
  catch (error) { setNotice(error.message); }
});
$("cancelNow").addEventListener("click", async () => {
  if (!confirm("現在の FFmpeg 処理を停止し、未公開の一時出力を破棄しますか？")) return;
  try { render(await request("/api/jobs/stop", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ mode: "now" }) })); }
  catch (error) { setNotice(error.message); }
});
document.querySelectorAll(".filter").forEach((button) => button.addEventListener("click", () => {
  resultFilter = button.dataset.filter;
  document.querySelectorAll(".filter").forEach((item) => item.classList.toggle("active", item === button));
  renderResults(snapshot?.results || []);
}));
$("browseButton").addEventListener("click", async () => {
  try {
    await openDirectory($("root").value.trim());
    $("directoryDialog").showModal();
  } catch (error) { setNotice(error.message); }
});
$("parentButton").addEventListener("click", () => openDirectory(directoryState.parent).catch((error) => setNotice(error.message)));
$("selectDirectory").addEventListener("click", () => {
  $("root").value = directoryState.path;
  $("directoryDialog").close();
});

async function initialize() {
  try {
    applyDefaults(await request("/api/defaults"));
    render(await request("/api/state"));
    const events = new EventSource("/api/events");
    events.onmessage = (event) => render(JSON.parse(event.data));
  } catch (error) { setNotice(error.message); }
}

initialize();
