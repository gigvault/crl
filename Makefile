.PHONY: build test lint docker run-local clean

build:
	go build -o bin/crl ./cmd/crl

test:
	go test ./... -v

lint:
	golangci-lint run ./...

docker:
	docker build -t gigvault/crl:local .

run-local: docker
	../infra/scripts/deploy-local.sh crl

clean:
	rm -rf bin/
	go clean
