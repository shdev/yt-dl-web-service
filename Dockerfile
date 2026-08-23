# Stage 0: Tailwind-CSS-Build — erzeugt app.css frisch im Image, damit ein
# veralteter committeter Stand nie ins Binary gelangt.
# Gepinnt (wie deno weiter unten): reproduzierbar, Bump invalidiert den Cache.
# Muss mit TAILWIND_VERSION im Makefile übereinstimmen.
FROM node:22-alpine AS css
ARG TAILWIND_VERSION=4.3.3
# Symlink: der v4-Resolver sucht das Paket "tailwindcss" vom Verzeichnis der
# Input-Datei aufwärts — /node_modules liegt auf diesem Pfad.
RUN npm install -g @tailwindcss/cli@${TAILWIND_VERSION} \
 && ln -s /usr/local/lib/node_modules/@tailwindcss/cli/node_modules /node_modules
WORKDIR /src
COPY web/ web/
RUN tailwindcss -i web/src/input.css -o /out/app.css --minify

# Stage 1: Go-Build
FROM golang:1.24 AS build
WORKDIR /src
COPY . .
COPY --from=css /out/app.css web/static/app.css
RUN CGO_ENABLED=0 go build -o /app ./cmd/server

# Stage 2: deno — JS-Runtime, die yt-dlp für YouTube braucht (JS-Challenges;
# ohne Runtime fehlen Formate bzw. schlagen Downloads fehl). Das Base-Image
# bringt keine mit. Kein deno-Build für arm/v7 — dort wird der Schritt
# übersprungen statt den Build zu brechen.
FROM debian:stable-slim AS deno
# TARGETARCH füllt nur BuildKit — mit dem Legacy-Builder wäre die Variable
# leer und deno fiele still weg; dpkg liefert dann die Host-Architektur.
ARG TARGETARCH
# Gepinnt statt releases/latest: reproduzierbare Builds, und ein Versions-
# bump invalidiert den Docker-Layer-Cache automatisch.
ARG DENO_VERSION=v2.9.5
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates curl unzip \
 && mkdir -p /out \
 && target="${TARGETARCH:-$(dpkg --print-architecture)}" \
 && case "$target" in \
      amd64) arch=x86_64-unknown-linux-gnu ;; \
      arm64) arch=aarch64-unknown-linux-gnu ;; \
      *) echo "WARNUNG: kein deno-Build für $target — YouTube ggf. eingeschränkt" >&2; arch= ;; \
    esac \
 && if [ -n "$arch" ]; then \
      curl -fsSL "https://github.com/denoland/deno/releases/download/${DENO_VERSION}/deno-${arch}.zip" \
        -o /tmp/deno.zip \
      && unzip -q /tmp/deno.zip -d /out \
      && chmod +x /out/deno; \
    fi

# Stage 3: Runtime — mikenye/youtube-dl als Base (Spec §2).
# yt-dlp + ffmpeg sind enthalten; das s6-Init (/init) wird bewusst
# durch unseren Webservice ersetzt.
FROM mikenye/youtube-dl
COPY --from=build /app /usr/local/bin/app
COPY --from=deno /out/ /usr/local/bin/
ENTRYPOINT ["/usr/local/bin/app"]
HEALTHCHECK --interval=30s --timeout=5s --start-period=15s \
  CMD ["/usr/local/bin/app", "-healthcheck"]
