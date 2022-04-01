NAME=prom_multi_proc
VERSION=$(shell cat version)
BUILD_TIME=$(shell date -u +"%Y-%m-%d %T")
BUILD_HASH=$(shell git rev-parse HEAD | cut -c 1-7 2>/dev/null || echo "")
GO_VERSION=$(shell go version | awk '{print $$3}')

LDFLAGS=-ldflags "-X 'main.Version=$(VERSION)' \
									-X 'main.BuildTime=$(BUILD_TIME)' \
									-X 'main.BuildHash=$(BUILD_HASH)' \
									-X 'main.GoVersion=$(GO_VERSION)'"

all: clean test build

clean:
	go clean
	@rm -f `which ${NAME}`

test:
	go test -cover

build: test
	go install ${LDFLAGS}

distclean:
	@mkdir -p dist
	rm -rf dist/*

dist: test distclean
	env GOOS=linux GOARCH=amd64 go build -v ${LDFLAGS} -o dist/${NAME}-${VERSION}-linux-amd64; \
	env GOOS=darwin GOARCH=amd64 go build -v ${LDFLAGS} -o dist/${NAME}-${VERSION}-darwin-amd64; \
	env GOOS=darwin GOARCH=arm64 go build -v ${LDFLAGS} -o dist/${NAME}-${VERSION}-darwin-arm64

sign: dist
	$(eval key := $(shell git config --get user.signingkey))
	for file in dist/*; do \
		gpg2 --armor --local-user ${key} --detach-sign $${file}; \
	done

package: sign
	tar czf dist/${NAME}-${VERSION}-linux-amd64.tar.gz -C dist ${NAME}-${VERSION}-linux-amd64 ${NAME}-${VERSION}-linux-amd64.asc; \
	tar czf dist/${NAME}-${VERSION}-darwin-amd64.tar.gz -C dist ${NAME}-${VERSION}-darwin-amd64 ${NAME}-${VERSION}-darwin-amd64.asc; \
	tar czf dist/${NAME}-${VERSION}-darwin-arm64.tar.gz -C dist ${NAME}-${VERSION}-darwin-arm64 ${NAME}-${VERSION}-darwin-arm64.asc; \
	find dist/ -type f  ! -name "*.tar.gz" -delete

tag:
	scripts/tag.sh

upload:
	if [ -n "$${GITHUB_TOKEN}" ]; then \
		ghr -t "$${GITHUB_TOKEN}" -u $$(whoami) -r ${NAME} -replace ${VERSION} dist/; \
	else; \
		echo "GITHUB_TOKEN not available"; \
	fi

release: package tag upload

.PHONY: all clean test build distclean dist sign package tag upload release
