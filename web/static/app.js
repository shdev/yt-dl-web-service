"use strict";

const $ = (id) => document.getElementById(id);

let probeResult = null;
let currentSettings = { default_profile: "best" };

async function api(path, options = {}) {
  const res = await fetch(path, {
    headers: { "Content-Type": "application/json" },
    ...options,
  });
  if (res.status === 204) return null;
  const body = await res.json().catch(() => ({}));
  if (!res.ok) throw new Error(body.error || `HTTP ${res.status}`);
  return body;
}

function show(el) { el.classList.remove("d-none"); }
function hide(el) { el.classList.add("d-none"); }

function esc(s) {
  // Escapt auch Quotes — der Wert landet teils in Attribut-Kontexten.
  return String(s ?? "").replace(/[&<>"'`]/g, (c) => ({
    "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;", "`": "&#96;",
  }[c]));
}

function humanSize(bytes) {
  if (!bytes) return "";
  const units = ["B", "KiB", "MiB", "GiB"];
  let i = 0;
  let n = bytes;
  while (n >= 1024 && i < units.length - 1) { n /= 1024; i++; }
  return `${n.toFixed(n >= 10 ? 0 : 1)} ${units[i]}`;
}

// --- Einstellungen -----------------------------------------------------------

async function loadSettings() {
  try {
    const s = await api("/api/settings");
    if (s && s.default_profile) currentSettings = s;
  } catch { /* Defaults behalten */ }
  $("default-profile").value = currentSettings.default_profile;
}
loadSettings();

$("settings-btn").addEventListener("click", () => {
  $("settings-card").classList.toggle("d-none");
});

$("default-profile").addEventListener("change", async () => {
  const value = $("default-profile").value;
  try {
    await api("/api/settings", { method: "PUT", body: JSON.stringify({ default_profile: value }) });
    currentSettings = { default_profile: value };
    show($("settings-saved"));
    setTimeout(() => hide($("settings-saved")), 1500);
  } catch (err) {
    alert(err.message);
    $("default-profile").value = currentSettings.default_profile;
  }
});

// --- Analyse ---------------------------------------------------------------

$("probe-btn").addEventListener("click", probe);
$("url-input").addEventListener("keydown", (e) => { if (e.key === "Enter") probe(); });

async function probe() {
  const url = $("url-input").value.trim();
  hide($("probe-error"));
  hide($("select-card"));
  if (!url) return;
  $("probe-btn").disabled = true;
  $("probe-btn").textContent = "Analysiere…";
  try {
    probeResult = await api("/api/probe", { method: "POST", body: JSON.stringify({ url }) });
    renderSelectCard();
  } catch (err) {
    $("probe-error").textContent = err.message;
    show($("probe-error"));
  } finally {
    $("probe-btn").disabled = false;
    $("probe-btn").textContent = "Analysieren";
  }
}

function renderSelectCard() {
  hide($("start-error"));
  if (probeResult.type === "playlist") {
    const pl = probeResult.playlist;
    $("select-title").textContent = pl.title || "Playlist";
    $("select-subtitle").textContent = `Playlist · ${pl.entries.length} Videos`;
    hide($("video-thumb"));
    hide($("video-options"));
    $("profile-select").value = currentSettings.default_profile;
    show($("playlist-options"));
  } else {
    const v = probeResult.video;
    $("select-title").textContent = v.title;
    $("select-subtitle").textContent = "";
    if (v.thumbnail) {
      $("video-thumb").src = v.thumbnail;
      show($("video-thumb"));
    } else {
      hide($("video-thumb"));
    }
    fillFormatSelects(v.formats || []);
    $("mode-profile").checked = true;
    $("video-profile").value = currentSettings.default_profile;
    show($("video-options"));
    hide($("playlist-options"));
    updateModeVisibility();
  }
  show($("select-card"));
}

function fillFormatSelects(formats) {
  const isVideo = (f) => f.vcodec && f.vcodec !== "none";
  const isAudio = (f) => f.acodec && f.acodec !== "none" && (!f.vcodec || f.vcodec === "none");
  const byRate = (a, b) => (b.tbr || b.abr || 0) - (a.tbr || a.abr || 0);

  const vids = formats.filter(isVideo).sort(byRate);
  const auds = formats.filter(isAudio).sort(byRate);

  $("video-format").innerHTML = vids.map((f) => {
    const label = [f.resolution, f.fps ? `${f.fps}fps` : "", f.vcodec, f.ext, humanSize(f.filesize)]
      .filter(Boolean).join(" · ");
    return `<option value="${esc(f.format_id)}">${esc(label)}</option>`;
  }).join("");
  $("audio-format").innerHTML = auds.map((f) => {
    const label = [f.acodec, f.abr ? `${Math.round(f.abr)} kbit/s` : "", f.ext, humanSize(f.filesize)]
      .filter(Boolean).join(" · ");
    return `<option value="${esc(f.format_id)}">${esc(label)}</option>`;
  }).join("");
}

document.querySelectorAll('input[name="mode"]').forEach((el) =>
  el.addEventListener("change", updateModeVisibility)
);

function currentMode() {
  return document.querySelector('input[name="mode"]:checked').value;
}

function updateModeVisibility() {
  const mode = currentMode();
  if (mode === "profile") {
    show($("video-profile-wrap"));
    hide($("format-selects"));
  } else if (mode === "manual") {
    hide($("video-profile-wrap"));
    show($("format-selects"));
    show($("video-col"));
  } else if (mode === "audio") {
    hide($("video-profile-wrap"));
    show($("format-selects"));
    hide($("video-col"));
  }
}

// --- Download starten ------------------------------------------------------

$("start-btn").addEventListener("click", start);

async function start() {
  hide($("start-error"));
  try {
    if (probeResult.type === "playlist") {
      const res = await api("/api/jobs", {
        method: "POST",
        body: JSON.stringify({
          type: "playlist",
          profile: $("profile-select").value,
          playlist_title: probeResult.playlist.title,
          entries: probeResult.playlist.entries,
        }),
      });
      hide($("select-card"));
      $("url-input").value = "";
      await refreshJobs();
      if (res && res.skipped > 0) {
        alert(`${res.skipped} Eintrag/Einträge übersprungen (bereits in der Warteschlange oder ohne URL).`);
      }
    } else {
      const mode = currentMode();
      const payload = mode === "profile"
        ? {
            type: "video",
            url: $("url-input").value.trim(),
            title: probeResult.video.title,
            profile: $("video-profile").value,
          }
        : {
            type: "video",
            url: $("url-input").value.trim(),
            title: probeResult.video.title,
            audio_only: mode === "audio",
            format_video: mode === "manual" ? $("video-format").value : "",
            format_audio: $("audio-format").value,
            format_label: formatLabel(mode),
          };
      await api("/api/jobs", { method: "POST", body: JSON.stringify(payload) });
      hide($("select-card"));
      $("url-input").value = "";
      await refreshJobs();
    }
  } catch (err) {
    $("start-error").textContent = err.message;
    show($("start-error"));
  }
}

// formatLabel wird nur für die Modi "manual" und "audio" gebraucht —
// bei "profile" liefert der Server das Label zum gewählten Profil.
function formatLabel(mode) {
  const audioText = $("audio-format").selectedOptions[0]?.textContent.trim() || "beste";
  if (mode === "audio") return `Nur Audio (${audioText})`;
  const videoText = $("video-format").selectedOptions[0]?.textContent.trim() || "";
  return `${videoText} + ${audioText}`;
}

// --- Jobs-Tabelle ----------------------------------------------------------

const STATE_BADGES = {
  queued:   ["text-bg-secondary", "Wartet"],
  running:  ["text-bg-primary", "Lädt"],
  done:     ["text-bg-success", "Fertig"],
  error:    ["text-bg-danger", "Fehler"],
  canceled: ["text-bg-warning", "Abgebrochen"],
};

async function refreshJobs() {
  try {
    const body = await api("/api/jobs");
    renderJobs(body.jobs || []);
  } catch {
    // Polling-Fehler ignorieren; der nächste Tick versucht es erneut.
  }
}

function renderJobs(jobs) {
  $("jobs-tbody").innerHTML = jobs.map((j) => {
    const [badge, label] = STATE_BADGES[j.state] || ["text-bg-secondary", esc(j.state)];
    const pct = Math.round(j.progress?.percent || 0);
    let title = esc(j.title || j.url);
    if (j.playlist_title) {
      title += ` <small class="text-body-secondary">(${esc(j.playlist_title)})</small>`;
    }
    const errorHTML = j.error
      ? `<div class="small text-danger">${esc(j.error)}</div>` : "";
    const animated = j.state === "running" ? " progress-bar-striped progress-bar-animated" : "";
    return `<tr>
      <td>${title}${errorHTML}</td>
      <td class="small">${esc(j.format_label)}</td>
      <td><span class="badge ${badge}">${label}</span></td>
      <td>
        <div class="progress" role="progressbar" aria-valuenow="${pct}"
             aria-valuemin="0" aria-valuemax="100">
          <div class="progress-bar${animated}" style="width:${pct}%">${pct}%</div>
        </div>
      </td>
      <td class="small">${esc(j.progress?.speed || "")}</td>
      <td class="small">${esc(j.progress?.eta || "")}</td>
      <td class="text-nowrap">${actionButtons(j)}</td>
    </tr>`;
  }).join("");
}

function actionButtons(j) {
  const btn = (action, label, cls) =>
    `<button class="btn btn-sm ${cls}" data-action="${action}" data-id="${j.id}">${label}</button>`;
  if (j.state === "queued" || j.state === "running") {
    return btn("cancel", "Abbrechen", "btn-outline-warning");
  }
  const parts = [];
  if (j.state === "error" || j.state === "canceled") {
    parts.push(btn("retry", "Erneut", "btn-outline-primary"));
  }
  parts.push(btn("delete", "Entfernen", "btn-outline-danger"));
  return parts.join(" ");
}

$("jobs-tbody").addEventListener("click", async (e) => {
  const btn = e.target.closest("button[data-action]");
  if (!btn) return;
  const { action, id } = btn.dataset;
  try {
    if (action === "delete") {
      await api(`/api/jobs/${id}`, { method: "DELETE" });
    } else {
      await api(`/api/jobs/${id}/${action}`, { method: "POST" });
    }
    await refreshJobs();
  } catch (err) {
    alert(err.message);
  }
});

refreshJobs();
setInterval(refreshJobs, 1500);
