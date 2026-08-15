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
- Parallel downloads (configurable), with live progress, speed and ETA
- Persistent job queue: survives container restarts, interrupted downloads resume (`--continue`)
- Retry, cancel and remove jobs from the UI (removing a job never deletes files)
- Single container: one Go binary with an embedded Bootstrap UI, no CDN, UI works offline
- Optional yt-dlp self-update on container start

## Quick start

Requires Docker with Compose v2. Clone the repository, then:

```bash
mkdir -p data/downloads data/config   # create these first — they must be writable by the user configured below
make up        # builds the image and starts the container
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
| `OUTPUT_TEMPLATE` | `%(title)s [%(id)s].%(ext)s` | yt-dlp output filename template |
| `YTDLP_UPDATE_ON_START` | `true` | Update yt-dlp when the container starts |

| Volume mount | Purpose |
|---|---|
| `/downloads` | Target directory for finished downloads |
| `/config` | Persistent state: job queue (`jobs.json`), in-app settings (`settings.json`) and the updated yt-dlp binary |

In-app settings (gear icon) are stored in `/config/settings.json` — currently
the default format profile preselected after each probe.

## How it works

- A small Go server (standard library only) shells out to yt-dlp: `-J` to
  probe formats, a worker pool with a machine-readable progress template for
  downloads.
- Jobs are persisted to `/config/jobs.json` on every state change (atomic
  temp-file + rename). After a restart, interrupted jobs are re-queued and
  resume from their `.part` files.
- The UI (Bootstrap 5, vanilla JS, German) polls `/api/jobs` every 1.5 s.
- The Docker image is based on `mikenye/youtube-dl` (ships yt-dlp + ffmpeg),
  built for amd64.

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

## License

[PolyForm Noncommercial 1.0.0](LICENSE.md) — you may use, modify and share
this software for any noncommercial purpose; commercial use is not permitted.

Bundled components keep their own licenses: Bootstrap (MIT, vendored),
yt-dlp (Unlicense, pulled at image build/start time).
