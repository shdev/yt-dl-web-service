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

function show(el) { el.hidden = false; }
function hide(el) { el.hidden = true; }

function esc(s) {
  // Escapt auch Quotes — der Wert landet teils in Attribut-Kontexten.
  return String(s ?? "").replace(/[&<>"'`]/g, (c) => ({
    "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;", "`": "&#96;",
  }[c]));
}

// Dezente Rückmeldung unten rechts statt alert(); verschwindet nach 4 s.
function toast(text, kind = "ok") {
  const cls = { ok: "toast toast-ok", warn: "toast toast-warn", error: "toast toast-err" };
  const el = document.createElement("div");
  el.className = cls[kind] || cls.ok;
  const dot = document.createElement("i");
  dot.className = "toast-dot";
  el.append(dot, document.createTextNode(text));
  $("toast-region").append(el);
  setTimeout(() => el.remove(), 4000);
}

function humanSize(bytes) {
  if (!bytes) return "";
  const units = ["B", "KiB", "MiB", "GiB"];
  let i = 0;
  let n = bytes;
  while (n >= 1024 && i < units.length - 1) { n /= 1024; i++; }
  return `${n.toFixed(n >= 10 ? 0 : 1)} ${units[i]}`;
}

// Relative Zeitangabe für Job-Zeitstempel; "" bei fehlendem/ungültigem Wert.
// Zukunfts-Timestamps (Uhrenversatz) werden auf 0 geklemmt statt negative
// Werte anzuzeigen.
function relTime(iso) {
  const d = new Date(iso);
  if (!iso || isNaN(d.getTime())) return "";
  const diffSec = Math.max(0, Math.floor((Date.now() - d.getTime()) / 1000));
  if (diffSec < 60) return "gerade eben";
  const diffMin = Math.floor(diffSec / 60);
  if (diffMin < 60) return `vor ${diffMin} min`;
  const diffH = Math.floor(diffMin / 60);
  if (diffH < 24) return `vor ${diffH} h`;
  const time = d.toLocaleTimeString("de-DE", { hour: "2-digit", minute: "2-digit" });
  return `${d.toLocaleDateString("de-DE")} ${time}`;
}

// Absolutwert für das title-Attribut neben relTime(); "" bei ungültigem Wert.
function absTime(iso) {
  const d = new Date(iso);
  return !iso || isNaN(d.getTime()) ? "" : d.toLocaleString("de-DE");
}

// Baut das Prefix+Zeit-Fragment für die Job-Meta-Zeile ("hinzugefügt vor 5
// min"); "" wenn der Zeitstempel fehlt/ungültig ist (dann bleibt das
// Fragment ganz weg statt eines leeren Rests).
function timeFragment(prefix, iso) {
  const rel = relTime(iso);
  if (!rel) return "";
  return `${esc(prefix)} <span title="${esc(absTime(iso))}">${esc(rel)}</span>`;
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
  const card = $("settings-card");
  card.hidden = !card.hidden;
  if (!card.hidden) loadYtdlpVersion();
});

// --- yt-dlp-Version & Update -----------------------------------------------

// Sequenznummer statt Cache: jedes Panel-Öffnen lädt frisch (yt-dlp kann
// anderweitig aktualisiert worden sein), und eine verspätete Antwort eines
// älteren Fetches darf eine neuere Anzeige nicht mehr überschreiben.
let ytdlpVersionSeq = 0;

async function loadYtdlpVersion() {
  const seq = ++ytdlpVersionSeq;
  try {
    const res = await api("/api/ytdlp");
    if (seq !== ytdlpVersionSeq) return;
    $("ytdlp-version").textContent = res.version;
  } catch (err) {
    if (seq !== ytdlpVersionSeq) return;
    $("ytdlp-version").textContent = "unbekannt";
    const status = $("ytdlp-update-status");
    status.className = "text-xs text-danger";
    status.textContent = `Versionsabfrage fehlgeschlagen: ${err.message}`;
    show(status);
    console.error(err);
  }
}

$("ytdlp-update-btn").addEventListener("click", async () => {
  const btn = $("ytdlp-update-btn");
  const status = $("ytdlp-update-status");
  ytdlpVersionSeq++; // laufende Versions-Fetches invalidieren
  btn.disabled = true;
  status.className = "text-xs text-muted";
  status.textContent = "Update läuft — das kann eine Minute dauern…";
  show(status);
  try {
    const res = await api("/api/ytdlp/update", { method: "POST" });
    ytdlpVersionSeq++;
    $("ytdlp-version").textContent = res.version;
    status.textContent = `Aktuell: ${res.version} ✓`;
  } catch (err) {
    status.className = "text-xs text-danger";
    status.textContent = err.message;
    console.error(err);
  } finally {
    btn.disabled = false;
  }
});

$("default-profile").addEventListener("change", async () => {
  const value = $("default-profile").value;
  try {
    await api("/api/settings", { method: "PUT", body: JSON.stringify({ default_profile: value }) });
    currentSettings = { default_profile: value };
    show($("settings-saved"));
    setTimeout(() => hide($("settings-saved")), 1500);
  } catch (err) {
    toast(err.message, "error");
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
    renderAudioLangChips(v.audio_languages || []);
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
  // Spiegelt internal/ytdlp/audiorank.go#isAudioOnly: ein ausdrückliches
  // vcodec "none" ist die primäre Audio-Kennung (ARTE liefert dafür
  // "acodec": null — unbekannter acodec zählt dann trotzdem als Ton).
  // Fehlt vcodec ganz, braucht es weiterhin einen positiven acodec.
  const isAudio = (f) =>
    (f.vcodec === "none" && f.acodec !== "none") ||
    (!f.vcodec && f.acodec && f.acodec !== "none");
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
    // Nur der Sprach-Kurzcode vor dem Bindestrich, analog zu den
    // Sprach-Chips (z. B. "de-DE" → "de").
    const prefixed = f.language ? `[${f.language.split("-")[0]}] ${label}` : label;
    return `<option value="${esc(f.format_id)}">${esc(prefixed)}</option>`;
  }).join("");
}

// --- Audiosprachen-Chips -----------------------------------------------------

// Chips für die Audiospuren-Vorauswahl im Profil-Modus; Sichtbarkeit steuert
// updateModeVisibility() (nur ab 2 Einträgen UND Modus "profile").
function renderAudioLangChips(list) {
  $("audio-langs-list").innerHTML = list.map((t, i) => `
    <input class="sr-only" type="checkbox" id="audio-lang-${i}" data-format-id="${esc(t.format_id)}"${t.selected ? " checked" : ""}>
    <label class="chip" for="audio-lang-${i}">${esc(t.label)}</label>`).join("");
}

function selectedAudioFormatIds() {
  return Array.from($("audio-langs-list").querySelectorAll("input:checked"))
    .map((c) => c.dataset.formatId);
}

// Ergänzt das Profil-Label um die gewählten Sprachlabels, z. B.
// "Beste Qualität · de + en (Original)".
function profileLabelWithLangs(ids) {
  const profileText = $("video-profile").selectedOptions[0]?.textContent.trim() || "";
  const langs = (probeResult.video.audio_languages || [])
    .filter((t) => ids.includes(t.format_id))
    .map((t) => t.label);
  return langs.length ? `${profileText} · ${langs.join(" + ")}` : profileText;
}

document.querySelectorAll('input[name="mode"]').forEach((el) =>
  el.addEventListener("change", updateModeVisibility)
);

function currentMode() {
  return document.querySelector('input[name="mode"]:checked').value;
}

function updateModeVisibility() {
  const mode = currentMode();
  const langs = probeResult?.video?.audio_languages || [];
  if (mode === "profile" && langs.length >= 2) {
    show($("audio-langs"));
  } else {
    hide($("audio-langs"));
  }
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
      toast("Download gestartet");
      if (res && res.skipped > 0) {
        toast(`${res.skipped} Eintrag/Einträge übersprungen (bereits in der Warteschlange oder ohne URL).`, "warn");
      }
    } else {
      const mode = currentMode();
      let payload;
      if (mode === "profile") {
        payload = {
          type: "video",
          url: $("url-input").value.trim(),
          title: probeResult.video.title,
          profile: $("video-profile").value,
        };
        // Chips nur einbeziehen, wenn sie sichtbar sind (Modus "profile" +
        // ≥2 Sprachen) und mindestens eine angehakt ist — sonst greift der
        // Server-Fallback (Feld weglassen).
        if (!$("audio-langs").hidden) {
          const ids = selectedAudioFormatIds();
          if (ids.length > 0) {
            payload.audio_format_ids = ids;
            payload.format_label = profileLabelWithLangs(ids);
          }
        }
      } else {
        payload = {
          type: "video",
          url: $("url-input").value.trim(),
          title: probeResult.video.title,
          audio_only: mode === "audio",
          format_video: mode === "manual" ? $("video-format").value : "",
          format_audio: $("audio-format").value,
          format_label: formatLabel(mode),
        };
      }
      await api("/api/jobs", { method: "POST", body: JSON.stringify(payload) });
      hide($("select-card"));
      $("url-input").value = "";
      await refreshJobs();
      toast("Download gestartet");
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

const STATE_PILLS = {
  queued:   ["pill pill-wait", "Wartet"],
  running:  ["pill pill-run", "Lädt"],
  done:     ["pill pill-ok", "Fertig"],
  error:    ["pill pill-err", "Fehler"],
  canceled: ["pill pill-warn", "Abgebrochen"],
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
  $("jobs-empty").hidden = jobs.length > 0;
  $("jobs-list").innerHTML = jobs.map((j) => {
    const [pill, label] = STATE_PILLS[j.state] || ["pill pill-wait", esc(j.state)];
    const pct = Math.round(j.progress?.percent || 0);
    let title = esc(j.title || j.url);
    if (j.playlist_title) {
      title += ` <span class="font-normal text-muted">(${esc(j.playlist_title)})</span>`;
    }
    const metaParts = [
      j.format_label,
      j.progress?.speed,
      j.progress?.eta ? `ETA ${j.progress.eta}` : "",
      j.state === "running" ? `${pct} %` : "",
    ].filter(Boolean).map(esc);
    metaParts.push(timeFragment("hinzugefügt", j.created_at));
    if (j.finished_at) {
      metaParts.push(timeFragment(j.state === "done" ? "fertig" : "beendet", j.finished_at));
    }
    const meta = metaParts.filter(Boolean).join(" · ");
    let extra = j.error
      ? `<div class="col-span-full text-xs text-danger">${esc(j.error)}</div>` : "";
    if (j.state === "done" && j.filename) {
      extra += `<div class="col-span-full truncate text-xs text-muted cursor-pointer font-mono"
        data-action="copy" data-filename="${esc(j.filename)}" title="${esc(j.filename)}">${esc(j.filename)}</div>`;
    }
    // Bekanntes Muster (z.B. yt-dlp#17456): 403 heißt fast immer, dass
    // yt-dlp veraltet ist — direkt zur Abhilfe verlinken.
    if (j.error && /HTTP Error 403|403: Forbidden/i.test(j.error)) {
      extra += `<div class="col-span-full text-xs text-muted">Tipp: yt-dlp über
        Einstellungen → „Jetzt aktualisieren“ auf den neuesten Stand bringen und den Job erneut starten.</div>`;
    }
    if (j.state === "running") {
      extra += `<div class="col-span-full bar" role="progressbar" aria-valuenow="${pct}"
        aria-valuemin="0" aria-valuemax="100"><span class="bar-fill" style="width:${pct}%"></span></div>`;
    }
    return `<div class="job-card grid grid-cols-[minmax(0,1fr)_auto] items-center gap-x-3.5 gap-y-1.5 sm:grid-cols-[minmax(0,1fr)_auto_auto]">
      <div class="truncate text-sm font-medium">${title}</div>
      <span class="${pill} justify-self-end"><i class="pill-dot"></i>${label}</span>
      <div class="col-span-2 flex gap-1.5 justify-self-start sm:col-span-1 sm:justify-self-end">${actionButtons(j)}</div>
      ${meta ? `<div class="col-span-full text-xs text-muted tabular-nums">${meta}</div>` : ""}
      ${extra}
    </div>`;
  }).join("");
}

function actionButtons(j) {
  const btn = (action, label) =>
    `<button class="btn btn-ghost btn-sm" data-action="${action}" data-id="${j.id}">${label}</button>`;
  if (j.state === "queued" || j.state === "running") {
    return btn("cancel", "Abbrechen");
  }
  const parts = [];
  if (j.state === "error" || j.state === "canceled") {
    parts.push(btn("retry", "Erneut"));
  }
  parts.push(btn("delete", "Entfernen"));
  return parts.join(" ");
}

$("jobs-list").addEventListener("click", async (e) => {
  const copyEl = e.target.closest('[data-action="copy"]');
  if (copyEl) {
    try {
      await navigator.clipboard.writeText(copyEl.dataset.filename);
      toast("Dateiname kopiert");
    } catch {
      toast("Kopieren nicht möglich", "error");
    }
    return;
  }
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
    toast(err.message, "error");
  }
});

refreshJobs();
setInterval(refreshJobs, 1500);
