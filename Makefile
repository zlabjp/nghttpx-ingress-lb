all: push

# 0.0 shouldn't clobber any release builds
PREFIX="zlabjp/nghttpx-ingress-controller"
TAG=latest

REPO_INFO=$(shell git config --get remote.origin.url)

ifndef VERSION
  VERSION := git-$(shell git rev-parse --short HEAD)
endif

.PHONY: controller container push clean vet fmt check

controller: clean
	CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -ldflags \
		"-w -X main.version=${VERSION} -X main.gitRepo=${REPO_INFO}" \
		-o nghttpx-ingress-controller \
		github.com/zlabjp/nghttpx-ingress-lb/pkg/cmd

container: controller
	docker build -t "${PREFIX}:${TAG}" .

push: container
	docker push "${PREFIX}:${TAG}"

clean:
	rm -f nghttpx-ingress-controller

vet:
	go tool vet -printfuncs Infof,Warningf,Errorf,Fatalf,Exitf pkg

fmt:
	go fmt github.com/zlabjp/nghttpx-ingress-lb/pkg/...

check:
	go test github.com/zlabjp/nghttpx-ingress-lb/pkg/...
