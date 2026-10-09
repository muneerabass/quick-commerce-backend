.PHONY: server run build test fmt tidy

server:
	nodemon --watch . --ext go --exec "go run ."

run:
	go run .

build:
	go build -o bin/quick-commerce .

test:
	go test ./...

fmt:
	gofmt -w $$(find . -name '*.go' -not -path './vendor/*')

tidy:
	go mod tidy
