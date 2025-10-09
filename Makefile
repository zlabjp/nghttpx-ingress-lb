PREFIX="zlabjp/nghttpx-ingress-controller"
TAG=latest

REPO_INFO=$(shell git config --get remote.origin.url)

ifndef VERSION
  VERSION := git-$(shell git rev-parse --short HEAD)
endif

.PHONY: all
all: container

.PHONY: controller
controller:
	CGO_ENABLED=0 GOOS=linux go build -installsuffix cgo -ldflags \
		"-w -X main.version=${VERSION} -X main.gitRepo=${REPO_INFO}" \
		github.com/zlabjp/nghttpx-ingress-lb/cmd/nghttpx-ingress-controller/...

.PHONY: container
container: export GOARCH = amd64
container: export PLATFORM = --platform=linux/${GOARCH}
container: controller container-platform

.PHONY: container-platform
container-platform:
	docker buildx build ${PLATFORM} -t "${PREFIX}:${TAG}" .

.PHONY: push
push: container
	docker push "${PREFIX}:${TAG}"

.PHONY: clean
clean:
	rm -f nghttpx-ingress-controller

.PHONY: vet
vet:
	go vet -printfuncs Eventf github.com/zlabjp/nghttpx-ingress-lb/pkg/... github.com/zlabjp/nghttpx-ingress-lb/cmd/...

.PHONY: fmt
fmt:
	go fmt github.com/zlabjp/nghttpx-ingress-lb/pkg/... github.com/zlabjp/nghttpx-ingress-lb/cmd/...

.PHONY: check
check:
	go test github.com/zlabjp/nghttpx-ingress-lb/pkg/... github.com/zlabjp/nghttpx-ingress-lb/cmd/...
