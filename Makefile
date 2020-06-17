all: push

# 0.0 shouldn't clobber any release builds
PREFIX="zlabjp/nghttpx-ingress-controller"
TAG=latest

REPO_INFO=$(shell git config --get remote.origin.url)

ifndef VERSION
  VERSION := git-$(shell git rev-parse --short HEAD)
endif

export GO111MODULE=on

.PHONY: controller container push clean vet fmt check

controller: clean
	CGO_ENABLED=0 GOOS=linux go build -installsuffix cgo -ldflags \
		"-w -X main.version=${VERSION} -X main.gitRepo=${REPO_INFO}" \
		github.com/zlabjp/nghttpx-ingress-lb/cmd/nghttpx-ingress-controller/...
	CGO_ENABLED=0 GOOS=linux go build -installsuffix cgo \
		github.com/zlabjp/nghttpx-ingress-lb/cmd/fetch-ocsp-response/...
	CGO_ENABLED=0 GOOS=linux go build -installsuffix cgo \
		github.com/zlabjp/nghttpx-ingress-lb/cmd/cat-ocsp-resp/...

container: controller
	docker build -t "${PREFIX}:${TAG}" .

push: container
	docker push "${PREFIX}:${TAG}"

clean:
	rm -f nghttpx-ingress-controller

vet:
	go vet -printfuncs Infof,Warningf,Errorf,Fatalf,Exitf github.com/zlabjp/nghttpx-ingress-lb/pkg/... github.com/zlabjp/nghttpx-ingress-lb/cmd/...

fmt:
	go fmt github.com/zlabjp/nghttpx-ingress-lb/pkg/... github.com/zlabjp/nghttpx-ingress-lb/cmd/...

check:
	go test github.com/zlabjp/nghttpx-ingress-lb/pkg/... github.com/zlabjp/nghttpx-ingress-lb/cmd/...
