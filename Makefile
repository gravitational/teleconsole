# To create a new release of Teleconsole:
#   - make your changes
#   - bump VERSION variable in this Makefile
#   - run 'make'
#   - THEN commit & push to git
#   - run `make release` to create and push the new git tag

# To bump the version, edit this variable and run `make version`
export VERSION=0.3.2-alpha
OUT=build/teleconsole
GOSRC=$(shell find -name "*.go" -print)
BUILDBOX_TAG:=teleconsole-buildbox:0.0.1

$(OUT): $(GOSRC) Makefile buildbox
	docker run \
		-v $(shell pwd):/go/src/github.com/gravitational/teleconsole \
		$(BUILDBOX_TAG) \
		$(MAKE) -C /go/src/github.com/gravitational/teleconsole build-container

.PHONY: build-container
build-container:
	mkdir -p build
	$(MAKE) -C version
	CGO_ENABLED=1 go build -i -ldflags -w -o $(OUT)

# Makes a new release (pushes tags to Github)
.PHONY:release 
release: version
	git tag -f $(VERSION)
	git push --tags --force

.PHONY:clean
clean:
	rm -rf out

.PHONY:test
test:
	go test ./... -v

# buildbox builds a docker image used to compile the binaries
.PHONY: buildbox
buildbox:
	docker build \
		--build-arg UID=$$(id -u) --build-arg GID=$$(id -g) \
		-t $(BUILDBOX_TAG) .
