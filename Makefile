# To create a new release of Teleconsole:
#   - make your changes
#   - bump VERSION variable in this Makefile
#   - commit & push to git
#   - run `make release` to create git tags

# To bump the version, edit this variable and run `make version`
export VERSION=0.1.0-beta

OUT=out/teleconsole
GOSRC=$(shell find -name "*.go" -print)
TELEPORT=$(shell find ../../gravitational/teleport/lib -name "*.go" -print)
TARBALL="teleconsole-v$(VERSION)-`go env GOOS`-`go env GOARCH`.tar.bz2"

# Default target: out/teleconsole
$(OUT): $(GOSRC) Makefile
	$(MAKE) -C version
	go build -i -ldflags -w -o $(OUT)

# Runs teleconsole against a server hosted in a local VM running
# as 'teleconsole.local' 
.PHONY:dev
dev: clean
	$(MAKE) $(OUT)
	$(MAKE) -C ../telecast clean
	$(MAKE) -C ../telecast dev
	sleep 3
	out/teleconsole -s teleconsole.local:5000 -insecure


# Makes a new release (pushes tags to Github)
.PHONY:release 
release: version
	git tag -f $(VERSION)
	git push --tags --force

# Make version bumps the version 
.PHONY:version
version:
	@VERSION=$(VERSION) $(MAKE) -C version

.PHONY:clean
clean:
	rm -rf out

.PHONY:test
test:
	go test ./... -v
