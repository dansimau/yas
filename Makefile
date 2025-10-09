.PHONY: lint
lint:
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.5.0 fmt
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.5.0 run --fix

.PHONY: test
test:
	go test ./... -coverprofile=./cover.out -covermode=atomic -coverpkg=./...
	go run github.com/vladopajic/go-test-coverage/v2@latest --config=./.testcoverage.yaml
