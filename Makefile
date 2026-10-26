export PATH := $(CURDIR)/bin:$(PATH)

generate: ## templ + sqlc
	templ generate
	@if [ -f sqlc.yaml ]; then sqlc generate; fi

css:
	./bin/tailwindcss -i web/styles/input.css -o web/static/css/app.css --minify

check: generate css
	go vet ./...
	go test ./...

dev: ## live reload; loads .env if present (the binary itself reads env only)
	@if [ -f .env ]; then set -a; . ./.env; set +a; fi; air

build: generate css
	CGO_ENABLED=0 go build -o bin/patrolhub ./cmd/patrolhub

docker: css ## image from the committed CSS, refreshed first
	docker build -t patrolhub:latest .

deploy: css ## Fly.io (fly.toml); the image uses the committed CSS, refreshed first
	fly deploy
