.PHONY: build test check clean

build:
	go build -o gh-csd .

test:
	go test ./...

# Run before finishing any change; CI runs the same.
check:
	test -z "$$(gofmt -l .)" || (gofmt -l . && exit 1)
	go vet ./...
	go test ./...
	go build -o gh-csd .

clean:
	rm -f gh-csd
