.PHONY: server run dev prod build test fmt tidy seed db-up db-down check-prod gen-secret

server:
	nodemon --watch . --ext go --exec "go run ."

run:
	go run .

dev:
	DEV_OTP_LOG=true go run .

# Run with production-safe flags locally. Fails loudly if any critical secret
# is still at its placeholder value.
prod: check-prod
	DEV_OTP_LOG=false go run .

# Fail fast when secrets are still at example values. Blocks `make prod`.
check-prod:
	@test -f .env || (echo "error: .env is missing" && exit 1)
	@grep -q "^JWT_SECRET=replace-with" .env && (echo "error: JWT_SECRET is still the placeholder — run 'make gen-secret' and update .env" && exit 1) || true
	@grep -q "^OTP_HASH_SECRET=replace-with" .env && (echo "error: OTP_HASH_SECRET is still the placeholder — run 'make gen-secret' and update .env" && exit 1) || true
	@grep -qE "^TWO_FACTOR_API_KEY=.+" .env || (echo "error: TWO_FACTOR_API_KEY is empty — real SMS delivery is required for prod" && exit 1)
	@echo "prod env looks sane"

# Print a cryptographically strong 48-char base64 secret you can paste into .env.
gen-secret:
	@openssl rand -base64 36 | tr -d '\n' && echo

build:
	go build -o bin/quick-commerce .

test:
	go test ./...

fmt:
	gofmt -w $$(find . -name '*.go' -not -path './vendor/*')

tidy:
	go mod tidy

seed:
	go run ./cmd/seed

db-up:
	docker compose -f infra/docker-compose.yml up -d

db-down:
	docker compose -f infra/docker-compose.yml down
