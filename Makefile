IMAGE=trusch/backbone-tools:latest
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

start-pod:
	podman pod create --name backbone-tools -p 3001:3001
	podman run --pod backbone-tools -d --name postgres -e POSTGRES_PASSWORD=postgres postgres
	podman run --pod backbone-tools -d --name backbone-tools-server ${IMAGE}

stop-pod:
	podman pod kill backbone-tools
	podman pod rm backbone-tools

restart-server:
	podman kill backbone-tools-server
	podman rm backbone-tools-server
	podman run --pod backbone-tools -d --name backbone-tools-server ${IMAGE}
