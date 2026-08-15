BINARY := bin/app
# Auf GitHub heißt das Projekt yt-dl-web-service — Image-Name folgt dem Repo.
IMAGE := yt-dl-web-service
GHCR_USER ?= shdev
TAG ?= latest

.PHONY: build test check fmt-check vet run image push check-ghcr-user up down clean

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

# Manueller Push zur GitHub Container Registry (kein CI):
#   make push [TAG=v1]            — Default-User: shdev
# Voraussetzung (einmalig): docker login ghcr.io mit PAT (Scope write:packages)
push: check-ghcr-user image
	docker tag $(IMAGE) ghcr.io/$(GHCR_USER)/$(IMAGE):$(TAG)
	docker push ghcr.io/$(GHCR_USER)/$(IMAGE):$(TAG)

check-ghcr-user:
	@test -n "$(GHCR_USER)" || { echo "GHCR_USER fehlt: make push GHCR_USER=<github-user>"; exit 1; }

up:
	docker compose up -d --build

down:
	docker compose down

clean:
	rm -rf bin tmp
