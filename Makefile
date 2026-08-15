BINARY := bin/app

.PHONY: build test check fmt-check vet run image up down clean

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
	docker build --platform linux/amd64 -t yt-dl-web-client .

up:
	docker compose up -d --build

down:
	docker compose down

clean:
	rm -rf bin tmp
