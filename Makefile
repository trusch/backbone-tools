IMAGE=docker.io/trusch/backbone-tools:latest
BASE_IMAGE=gcr.io/distroless/base:debug

default: image install

image: go.mod go.sum $(shell find ./ -name "*.go")
	mkdir -p /tmp/{go-build,go-modules}
	podman build \
		-t ${IMAGE} \
		-v /tmp/go-build:/root/.cache/go-build \
		-v /tmp/go-modules:/go/pkg/mod \
		-f ./scripts/Dockerfile \
		--build-arg BASE_IMAGE=${BASE_IMAGE} .

install:
	go install -v ./cmd/...

start: start-pod
stop: stop-pod

# podman dev pod:
# external availabe services:
# - backbone-tools gRPC API at localhost:3001
# - backbone-tools health and metrics API at localhost:8080
# - jaeger-tracing web UI at localhost:16686
start-pod:
	podman pod create --name backbone-tools -p 3001:3001 -p 8080:8080 -p 16686:16686
	podman run --pod backbone-tools -d --name postgres -e POSTGRES_PASSWORD=postgres postgres
	podman run --pod backbone-tools -d --name jaeger jaegertracing/all-in-one:1.17
	podman run --pod backbone-tools -d --name backbone-tools-server ${IMAGE} --tracing localhost:6831

stop-pod:
	podman pod kill backbone-tools
	podman pod rm backbone-tools

restart-server:
	podman kill backbone-tools-server
	podman rm backbone-tools-server
	podman run --pod backbone-tools -d --name backbone-tools-server ${IMAGE}

# docker dev workflow
start-docker:
	docker run -d --rm --name postgres -e POSTGRES_PASSWORD=postgres postgres
	docker run -d --rm  --name jaeger -p 16686:16686  jaegertracing/all-in-one:1.17
	docker run -d --rm --name backbone-tools-server --link postgres --link jaeger -p3001:3001 -p8080:8080 ${IMAGE} "--db=postgres://postgres@postgres:5432?sslmode=disable&password=postgres" "--tracing=jaeger:6831"

stop-docker:
	docker kill postgres backbone-tools-server jaeger
