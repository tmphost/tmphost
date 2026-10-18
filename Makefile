.PHONY: build frontend backend test test-backend test-frontend coverage mocks docker dev-backend dev-frontend

PUBLIC := backend/cmd/server/public

build: frontend backend

# Build the SPA and copy it where the server embeds it.
frontend:
	cd frontend && npm ci && npm run build
	find $(PUBLIC) -mindepth 1 ! -name .gitkeep -delete
	cp -r frontend/dist/. $(PUBLIC)/

backend:
	cd backend && go build -o ../bin/tmphost ./cmd/server

test: test-backend test-frontend

test-backend:
	cd backend && go test -race -coverprofile=coverage.out $$(go list ./... | grep -v /mocks) && go tool cover -func=coverage.out | tail -1

test-frontend:
	cd frontend && npm run test:coverage

docker:
	docker build -t tmphost .

mocks:
	cd backend && go generate ./...

# Run both in separate terminals; open http://localhost:5173.
dev-backend:
	cd backend && TMPHOST_BASE_URL=http://localhost:5173 go run ./cmd/server

dev-frontend:
	cd frontend && npm run dev
