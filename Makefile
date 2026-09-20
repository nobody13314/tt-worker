.PHONY: test run mock docker-up docker-down
test:
	go test ./...
run:
	go run ./cmd/tt-worker
mock:
	go run ./cmd/mock-server
docker-up:
	docker compose up -d --build
docker-down:
	docker compose down
