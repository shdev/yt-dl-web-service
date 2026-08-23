BINARY := bin/app
# Auf GitHub heißt das Projekt yt-dl-web-service — Image-Name folgt dem Repo.
IMAGE := yt-dl-web-service
GHCR_USER ?= shdev
TAG ?= latest
# Muss mit ARG TAILWIND_VERSION im Dockerfile übereinstimmen.
TAILWIND_VERSION := 4.3.3

.PHONY: build test check fmt-check vet run image image-native push check-ghcr-user up down start stop css css-watch clean

build:
	CGO_ENABLED=0 go build -o $(BINARY) ./cmd/server

test:
	go test ./...

check: fmt-check vet test

fmt-check:
	test -z "$$(gofmt -l .)"

vet:
	go vet ./...

run: build
	mkdir -p tmp/downloads tmp/config
	PORT=8080 DOWNLOAD_DIR=tmp/downloads CONFIG_DIR=tmp/config \
		YTDLP_UPDATE_ON_START=false $(BINARY)

image:
	docker build --platform linux/amd64 -t $(IMAGE) .

# Image für die aktuelle Plattform des Hosts (z.B. arm64 auf Apple Silicon,
# amd64 auf Linux-PCs) — schneller lokaler Build ohne Emulation.
# Das Base-Image ist multi-arch (amd64, arm64, arm/v7).
image-native:
	docker build -t $(IMAGE) .

# Manueller Push zur GitHub Container Registry (kein CI):
#   make push [TAG=v1]            — Default-User: shdev
# Voraussetzung (einmalig): docker login ghcr.io mit PAT (Scope write:packages)
# Baut amd64+arm64 via buildx und pusht EIN Multi-Arch-Manifest.
# (image/image-native taggen beide $(IMAGE) — ein tag+push eines einzelnen
# Builds würde :latest sonst auf eine einzige Architektur reduzieren.)
push: check-ghcr-user
	docker buildx build --platform linux/amd64,linux/arm64 \
		-t ghcr.io/$(GHCR_USER)/$(IMAGE):$(TAG) --push .

check-ghcr-user:
	@test -n "$(GHCR_USER)" || { echo "GHCR_USER fehlt: make push GHCR_USER=<github-user>"; exit 1; }

up:
	docker compose up -d --build

down:
	docker compose down

# Alias zu down — symmetrisch zu "make start".
stop: down

# Muss zum ports-Mapping in docker-compose.yml passen (Default 8080:8080);
# überschreibbar: make start HOST_PORT=9090
HOST_PORT ?= 8080

# Image bauen, Container starten und die UI im Browser öffnen, sobald der
# Dienst antwortet (max. 60 s).
start: up
	@ok=0; for i in $$(seq 1 60); do \
	  curl -fsS -o /dev/null http://localhost:$(HOST_PORT)/ && { ok=1; break; }; \
	  sleep 1; \
	done; \
	test $$ok -eq 1 || { echo "Dienst antwortet nicht auf http://localhost:$(HOST_PORT)"; exit 1; }
	@command -v open >/dev/null 2>&1 \
	  && open "http://localhost:$(HOST_PORT)/" \
	  || xdg-open "http://localhost:$(HOST_PORT)/"

clean:
	rm -rf bin tmp

# Tailwind-CSS via Docker kompilieren — kein lokales npm nötig.
# Das npm-Cache-Volume erspart den CLI-Download bei jedem Aufruf.
# Der Symlink nach /node_modules ist nötig, weil der v4-Resolver das Paket
# "tailwindcss" vom Verzeichnis der Input-Datei aufwärts sucht.
TAILWIND_RUN = npm install -g @tailwindcss/cli@$(TAILWIND_VERSION) >/dev/null \
	&& ln -s /usr/local/lib/node_modules/@tailwindcss/cli/node_modules /node_modules \
	&& cd /work && tailwindcss -i web/src/input.css -o web/static/app.css

css:
	docker run --rm -v $(CURDIR):/work -v ytdlweb-npm-cache:/root/.npm \
		node:22-alpine sh -c "$(TAILWIND_RUN) --minify"

css-watch:
	docker run --rm -it -v $(CURDIR):/work -v ytdlweb-npm-cache:/root/.npm \
		node:22-alpine sh -c "$(TAILWIND_RUN) --watch"
