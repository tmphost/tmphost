.PHONY: build frontend backend cli test test-backend test-frontend test-cli coverage mocks lint lint-backend lint-frontend copyright copyright-fix docker dev-backend dev-frontend

PUBLIC := backend/cmd/server/public

build: frontend backend

# Build the SPA and copy it where the server embeds it.
frontend:
	cd frontend && npm ci && npm run build
	find $(PUBLIC) -mindepth 1 ! -name .gitkeep -delete
	cp -r frontend/dist/. $(PUBLIC)/

backend:
	cd backend && go build -o ../bin/tmphost ./cmd/server

# bin/tmphost is the server, so the CLI gets its own name; rename it on install.
cli:
	cd cli && go build -o ../bin/tmphost-cli .

test: test-backend test-frontend test-cli

test-backend:
	cd backend && go test -race -coverprofile=coverage.out $$(go list ./... | grep -v /mocks) && go tool cover -func=coverage.out | tail -1

test-cli:
	cd cli && go test -race -coverprofile=coverage.out ./... && go tool cover -func=coverage.out | tail -1

test-frontend:
	cd frontend && npm run test:coverage

docker:
	docker build -t tmphost .

mocks:
	cd backend && go generate ./...

lint: lint-backend lint-frontend copyright

# Needs golangci-lint v2 on PATH; CI pins the version in .github/workflows/lint.yml.
lint-backend:
	cd backend && golangci-lint run ./...

lint-frontend:
	cd frontend && npm run lint

# Every source file carries the copyright notice; `make copyright-fix` adds it.
copyright:
	node frontend/scripts/copyright.mjs

copyright-fix:
	node frontend/scripts/copyright.mjs --fix

# Run both in separate terminals; open http://localhost:5173.
dev-backend:
	cd backend && TMPHOST_BASE_URL=http://localhost:5173 go run ./cmd/server

dev-frontend:
	cd frontend && npm run dev
