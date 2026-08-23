# yt-dl-web-service

A self-hosted web UI for [yt-dlp](https://github.com/yt-dlp/yt-dlp), built to
run on a NAS. Paste a video or playlist URL, pick the exact video/audio format
(or a quality profile), and let the server download in the background — with a
persistent job queue, live progress, and parallel downloads. Finished files
land directly in a mounted folder (e.g. your media library).

## Features

- Paste a URL, inspect title, thumbnail and every available format (via `yt-dlp -J`)
- Pick exact video + audio formats, best quality, or audio-only
- Playlist support: one job per video, with quality profiles (best / ≤1080p / ≤720p / audio only)
- Multi-language audio: for videos with several audio tracks, downloads the
  preferred language (German, then English) plus the original track whenever
  it differs — automatic by default, overridable via language chips
- Downloads are sorted into `<source>/<channel>/…` folders by default (e.g.
  `youtube/SomeChannel/…`), configurable via `OUTPUT_TEMPLATE`
- Job cards show relative timestamps (added / finished) and the final
  filename once a download is done — click to copy it
- Parallel downloads (configurable), with live progress, speed and ETA
- Persistent job queue: survives container restarts, interrupted downloads resume (`--continue`)
- Retry, cancel and remove jobs from the UI (removing a job never deletes files)
- Single container: one Go binary with an embedded UI (custom Tailwind-built CSS), no CDN, UI works offline
- Optional yt-dlp self-update on container start — plus a one-click update
  button in the UI (gear icon), no container restart needed
- Ships deno as JavaScript runtime — required by current yt-dlp for YouTube
  (JS challenges; without it, formats go missing or downloads fail)

## Quick start

Requires Docker with Compose v2. Clone the repository, then:

```bash
mkdir -p data/downloads data/config   # create these first — they must be writable by the user configured below
make up        # builds the image and starts the container
make start     # like make up, but also opens the UI in your browser once it responds
```

Open `http://<your-host>:8080`, paste a URL, choose a format, hit
"Download starten". Finished files appear in `./data/downloads/`.

```bash
make down      # stops the container
```

Before the first start, adjust `docker-compose.yml`:

- `user:` — UID:GID that should own the downloaded files
- `volumes:` — where downloads and the queue state are stored
- `ports:` — host port (default 8080)

> **Note:** The web UI has no authentication. Run it on a trusted network
> (LAN), or put a reverse proxy with auth in front of it.

## Configuration

| Environment variable | Default | Purpose |
|---|---|---|
| `PORT` | `8080` | HTTP port of the web service |
| `MAX_CONCURRENT` | `3` | Number of parallel downloads |
| `OUTPUT_TEMPLATE` | `%(extractor)s/%(channel,uploader\|Unbekannt)s/%(title)s [%(id)s].%(ext)s` | yt-dlp output filename template |
| `YTDLP_UPDATE_ON_START` | `true` | Update yt-dlp when the container starts |

| Volume mount | Purpose |
|---|---|
| `/downloads` | Target directory for finished downloads |
| `/config` | Persistent state: job queue (`jobs.json`), in-app settings (`settings.json`) and the updated yt-dlp binary |

In-app settings (gear icon) are stored in `/config/settings.json` — currently
the default format profile preselected after each probe. The gear panel also
shows the installed yt-dlp version and offers a one-click update
(`POST /api/ytdlp/update` runs `yt-dlp -U` on the writable copy in `/config/bin`).

### Download folder structure

By default, files land under `<downloads>/<source>/<channel>/<title> [<id>].<ext>`:
`source` is yt-dlp's normalized extractor name (e.g. `youtube`, `ArteTV`; short
or alias domains like `youtu.be` map to the same source), `channel` is
`channel` or `uploader` from the video's metadata, falling back to
`Unbekannt` when neither is reported. Override the whole layout via
`OUTPUT_TEMPLATE` (any yt-dlp output template works).

Interrupted downloads resume from a `.part` file that yt-dlp keeps in that
same nested folder (`--continue`). If you change `OUTPUT_TEMPLATE` while a
download is queued or in progress, its `.part` file stays under the old
path and won't be picked up by `--continue` anymore — remove it manually or
let the retry start over. Duplicate detection for playlist jobs works the
same way: it compares the internal yt-dlp format expression, so after an
app update, playlist jobs still queued from an older version are no longer
recognized as duplicates of the same URL (the expression it was built with
may have changed) — if in doubt, let the queue drain before updating.

## Multi-language audio

For videos with several audio tracks (real dubs, e.g. ARTE's German /
French / English original, or YouTube's auto-dubs), the app downloads more
than one track by default: the preferred language — German before English —
plus the original track whenever it differs from the preferred one. A
German-dubbed ARTE video, for example, downloads with the German dub and
the English original in one file. Audio-description tracks are recognized
and never picked automatically. On videos with 2+ tracks, language chips on
the format card let you override the selection before starting the
download; playlist downloads (profile-based, no per-video probe) apply the
same de-before-en, original-included rule as a best-effort fallback chain.

Combining two audio tracks into one file needs multi-stream muxing
(`--audio-multistreams`); the container format is then `mp4/mkv` — mp4 when
the codecs allow it, mkv as the automatic fallback otherwise.

Playlist downloads always mux with `--merge-output-format mp4/mkv`, even for
entries that only end up with a single audio track — so a playlist download
may land as `.mkv` where a single-video download of the same source would
have stayed `.webm`. Single-video jobs without a multi-track selection are
unaffected and keep their usual container.

## Troubleshooting

**`ERROR: unable to download video data: HTTP Error 403: Forbidden` (YouTube)**
— almost always an outdated yt-dlp; YouTube changes frequently and yt-dlp
follows with new releases (e.g. [yt-dlp#17456](https://github.com/yt-dlp/yt-dlp/issues/17456),
fixed in 2026.08.19 by removing the `android_vr` client from the defaults).
Fix: gear icon → "Jetzt aktualisieren", then retry the failed job. A container
restart with `YTDLP_UPDATE_ON_START=true` does the same on startup.

**`No supported JavaScript runtime could be found` (warning in logs)** —
current yt-dlp needs a JS runtime (deno) for YouTube. Images built from this
repository since the deno stage was added include it on amd64 and arm64;
rebuild/pull the image if you still see the warning. On arm/v7 there is no
deno release — the build skips it and the warning remains.

## How it works

- A small Go server (standard library only) shells out to yt-dlp: `-J` to
  probe formats, a worker pool with a machine-readable progress template for
  downloads.
- Jobs are persisted to `/config/jobs.json` on every state change (atomic
  temp-file + rename). After a restart, interrupted jobs are re-queued and
  resume from their `.part` files.
- The UI (vanilla JS, Tailwind CSS, German) polls `/api/jobs` every 1.5 s.
  Each job card shows a relative "added" timestamp, plus a "done"/"finished"
  one once the job reaches a final state (hover either for the absolute
  time), and — once a download completes — the final filename with a
  click-to-copy button.
- The Docker image is based on `mikenye/youtube-dl` (ships yt-dlp + ffmpeg),
  built for amd64, with deno added on top (amd64/arm64) as the JS runtime
  yt-dlp needs for YouTube.

## Development

Requires Go ≥ 1.23; there are no external Go dependencies.

```bash
make check     # gofmt + go vet + tests
make run       # run locally on :8080 (uses ./tmp as volume substitute;
               # real downloads need a yt-dlp binary at /usr/local/bin/yt-dlp)
make image         # build the Docker image for amd64 (NAS/publishing target)
make image-native  # build for the host's own platform (no emulation — fast local builds)
```

The design spec and implementation plan live in `docs/superpowers/`.

## Using the prebuilt image (GHCR)

Instead of building locally, you can pull the published image — it is public,
so no `docker login` is needed. Either way, create the two data directories
first; they must be writable by the user you run the container as:

```bash
mkdir -p data/downloads data/config
```

### Plain `docker run` (no Compose)

```bash
docker run -d --name yt-dl-web \
  --user 1000:1000 \
  -p 8080:8080 \
  -e MAX_CONCURRENT=3 \
  -e YTDLP_UPDATE_ON_START=true \
  -v "$PWD/data/downloads:/downloads" \
  -v "$PWD/data/config:/config" \
  --restart unless-stopped \
  ghcr.io/shdev/yt-dl-web-service:latest
```

Then open `http://<your-host>:8080`. Update later with:

```bash
docker pull ghcr.io/shdev/yt-dl-web-service:latest
docker stop yt-dl-web && docker rm yt-dl-web
# ... then run the docker run command above again
```

### Docker Compose

Standalone `docker-compose.yml` (no repository checkout needed):

```yaml
services:
  yt-dl-web:
    image: ghcr.io/shdev/yt-dl-web-service:latest
    container_name: yt-dl-web
    # UID:GID that should own the downloaded files
    user: "1000:1000"
    ports:
      - "8080:8080"
    environment:
      MAX_CONCURRENT: "3"
      YTDLP_UPDATE_ON_START: "true"
    volumes:
      - ./data/downloads:/downloads
      - ./data/config:/config
    restart: unless-stopped
```

```bash
docker compose up -d                      # start
docker compose pull && docker compose up -d   # update to the latest image
```

(The `docker-compose.yml` in this repository builds the image locally instead —
that variant is meant for development; see Quick start.)

## Publishing the image (maintainers)

The image is pushed manually from a workstation — no CI involved:

```bash
# one-time: create a classic personal access token with the write:packages
# scope, then log in (the token lands in your credential helper):
echo $GHCR_TOKEN | docker login ghcr.io -u shdev --password-stdin

make push          # builds, tags and pushes ghcr.io/shdev/yt-dl-web-service:latest
make push TAG=v1   # same, with a specific tag
```

Note: the first push creates a *private* package. Switch it to *public* once
in the package settings on GitHub (Packages → yt-dl-web-service → Package
settings → Change visibility) so it can be pulled without authentication.

## Development

`web/static/app.css` is generated from `web/src/input.css` with Tailwind CSS —
do not edit it by hand. After changing HTML, JS or `input.css`, rebuild it:

    make css        # one-shot build (runs in Docker, no local npm needed)
    make css-watch  # rebuild on every change while styling

The generated file is committed so `go test` and `make run` work without
Docker. The Docker image builds its own fresh CSS in a dedicated build stage,
so a stale committed `app.css` never ends up in the image.

## License

[PolyForm Noncommercial 1.0.0](LICENSE.md) — you may use, modify and share
this software for any noncommercial purpose; commercial use is not permitted.

Bundled components keep their own licenses: Tailwind CSS (MIT, generated
stylesheet), yt-dlp (Unlicense, pulled at image build/start time).
