PROTO_DIR=proto/artcraft/v1
KIT_PROTO=../CubaLinked-BusinessKitModule/proto
export GOFLAGS=-mod=mod

.PHONY: proto build run test vet tidy docker

proto:
	PATH=$$PATH:$$(go env GOPATH)/bin protoc -I . -I $(KIT_PROTO) --go_out=. --go_opt=paths=source_relative --go-grpc_out=. --go-grpc_opt=paths=source_relative $(PROTO_DIR)/artcraft.proto

tidy:
	go mod tidy

build:
	go build ./...

vet:
	go vet ./...

test:
	go test ./...

run:
	go run ./cmd/server

docker:
	cd .. && docker build -f CubaLinked-ArtCraftModule/Dockerfile -t cubalinked-artcraft .
