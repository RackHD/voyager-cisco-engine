ORGANIZATION = RackHD
PROJECT = voyager-cisco-engine
BINARYNAME = voyager-cisco-engine
GOOUT = ./bin
export RABBITMQ_URL = amqp://guest:guest@localhost:5672

default: deps build test

deps:
	go get github.com/onsi/ginkgo/ginkgo
	go get github.com/onsi/gomega
	go get github.com/satori/go.uuid
	go get ./...

integration-test:
	ginkgo -r -race -trace -cover -randomizeAllSpecs --slowSpecThreshold=30 --focus="\bINTEGRATION\b"

unit-test:
	ginkgo -r -race -trace -cover -randomizeAllSpecs --slowSpecThreshold=30 --focus="\bUNIT\b"

test:
	ginkgo -r -race -trace -cover -randomizeAllSpecs --slowSpecThreshold=30

cover-cmd: test
	go tool cover -html=cmd/cmd.coverprofile

build:
	go build -o $(GOOUT)/$(BINARYNAME)
