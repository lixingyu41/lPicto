.PHONY: backend-test backend-vet frontend-build build docker-build check

backend-test:
	cd backend && go test ./...

backend-vet:
	cd backend && go vet ./...

frontend-build:
	cd frontend && npm install && npm run build

build:
	cd backend && go build ./cmd/server

docker-build:
	docker build -t lpicto:local .

check: backend-test backend-vet frontend-build
