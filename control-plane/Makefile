.PHONY: test test-coverage coverage

test:
	go test ./... -parallel=2

test-coverage:
	./scripts/coverage.sh

coverage: test-coverage
