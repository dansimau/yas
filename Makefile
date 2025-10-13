.PHONY: lint
lint:
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.5.0 fmt
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.5.0 run --fix

.PHONY: test
test:
	go test -json ./... -coverprofile=./cover.out -covermode=atomic -coverpkg=./... | tee test-results.json | python3 bin/colourise-go-test-output.py
	go tool cover -html=cover.out -o=cover.html
	go run github.com/vladopajic/go-test-coverage/v2@latest --config=./.testcoverage.yaml
