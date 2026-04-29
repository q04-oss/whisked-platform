.PHONY: run build test test-race test-security generate migrate lint vet vuln

# ── Development ───────────────────────────────────────────────────────────────

run:
	go run ./cmd/server

build:
	CGO_ENABLED=0 go build -trimpath -o bin/server ./cmd/server

# ── Tests ─────────────────────────────────────────────────────────────────────

test:
	go test -race -timeout 120s ./...

test-security:
	go test -race -run "TestRequireHMAC|TestSecret|TestRateLimit" -v ./internal/...

# ── Code generation ───────────────────────────────────────────────────────────

generate:
	sqlc generate

# ── Database migrations ───────────────────────────────────────────────────────

migrate:
	migrate -path db/migrations -database "$(DATABASE_URL)" up

migrate-down:
	migrate -path db/migrations -database "$(DATABASE_URL)" down 1

# ── Seed ──────────────────────────────────────────────────────────────────────
# Creates the first admin staff account. Run once after migrations.
# Example: make seed EMAIL=admin@whisked.ca NAME="Belle" PASSWORD=yourpassword

seed:
	go run ./cmd/seed \
	  -email="$(EMAIL)" \
	  -name="$(NAME)" \
	  -password="$(PASSWORD)" \
	  -role=admin

# ── Quality gates (mirrors CI) ────────────────────────────────────────────────

vet:
	go vet ./...

lint:
	golangci-lint run ./...

vuln:
	govulncheck ./...

# Run all quality gates locally before pushing.
check: vet lint vuln test
