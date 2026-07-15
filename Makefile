.PHONY: build-orchestrator test-orchestrator vet-orchestrator

build-orchestrator:
	CGO_ENABLED=0 go build -ldflags="-s -w" -o bin/rtg-orchestrator ./cmd/rtg-orchestrator/

test-orchestrator:
	go test ./cmd/rtg-orchestrator/... -count=1
	go test ./internal/registry/... -count=1

vet-orchestrator:
	go vet ./cmd/rtg-orchestrator/...
	go vet ./internal/registry/...
