MAKEFLAGS += -j4
COVERPKG ?= github.com/dansimau/yas/...
SRC = $(shell find cmd pkg \
				\( -type f -name '*.go' -o -type d \) \
				! -path '*_test.go' \
				! -path '*/.*' \
)

.PHONY: out
out: yas lint test

yas: $(SRC)
	go build -o yas cmd/yas/main.go

.PHONY: install
install: yas
	sudo install -m 755 yas /usr/local/bin/yas

.PHONY: lint
lint:
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.5.0 fmt
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.5.0 run --fix

.PHONY: test
test:
	mkdir -p coverage
	GOCMDTESTER_COVERPKG=$(COVERPKG) \
		time go test \
			./... \
			-count 1 \
			-json \
			-coverprofile=./coverage/main.cov \
			-covermode=atomic \
			-coverpkg=$(COVERPKG) | \
				tee test-results.json | \
				python3 bin/colourise-go-test-output.py >/dev/null

	go run github.com/wadey/gocovmerge@latest coverage/main.cov coverage/integration-tests.cov > coverage/combined.out
	go tool cover -html=coverage/combined.out -o=coverage/cover.html
	go run github.com/vladopajic/go-test-coverage/v2@latest --config=./.testcoverage.yaml
