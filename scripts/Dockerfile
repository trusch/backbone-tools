FROM golang:1.14 AS builder
RUN apt update && apt install unzip -y
RUN wget -O protoc.zip https://github.com/protocolbuffers/protobuf/releases/download/v3.11.4/protoc-3.11.4-linux-x86_64.zip
RUN unzip protoc.zip
RUN mv bin/* /bin/
RUN mv include/* /usr/include/
RUN go get -v -u google.golang.org/grpc github.com/golang/protobuf/protoc-gen-go

WORKDIR /go/src/github.com/trusch/backbone-tools/
COPY go.mod go.sum ./
RUN go mod download
COPY ./cmd ./cmd
COPY ./pkg ./pkg
RUN cd pkg/api && protoc -I . -I /usr/include --go_out=plugins=grpc:. *.proto
RUN go install -v ./cmd/...

ARG BASE_IMAGE=gcr.io/distroless/base:debug
FROM $BASE_IMAGE
COPY --from=builder /go/bin/backbone-tools /go/bin/backbonectl /go/bin/echo-worker /bin/

ENV PLATFORMCTL_DISABLE_TLS=true
ENTRYPOINT ["/bin/backbone-tools"]