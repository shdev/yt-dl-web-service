# Stage 1: Go-Build
FROM golang:1.24 AS build
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go build -o /app ./cmd/server

# Stage 2: Runtime — mikenye/youtube-dl als Base (Spec §2).
# yt-dlp + ffmpeg sind enthalten; das s6-Init (/init) wird bewusst
# durch unseren Webservice ersetzt.
FROM mikenye/youtube-dl
COPY --from=build /app /usr/local/bin/app
ENTRYPOINT ["/usr/local/bin/app"]
HEALTHCHECK --interval=30s --timeout=5s --start-period=15s \
  CMD ["/usr/local/bin/app", "-healthcheck"]
