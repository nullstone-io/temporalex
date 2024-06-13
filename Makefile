.PHONY: init

.DEFAULT_GOAL: default

default: vendor test

vendor:
	go mod vendor

test:
	go fmt ./...
	gotestsum
