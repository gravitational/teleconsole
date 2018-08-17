# To create a new release of Teleconsole:
#   - make your changes
#   - bump VERSION variable in this Makefile
#   - run 'make'
#   - THEN commit & push to git
#   - run `make release` to create and push the new git tag
#
# NOTE: teleport repo must be checked out with tag v2.0.0-alpha.4

# To bump the version, edit this variable and run `make version`
export VERSION=0.4.0
OUT=out/teleconsole
GOSRC=$(shell find -name "*.go" -print)
TELEPORTVER=v2.0.0-alpha.4

# Default target: out/teleconsole
$(OUT): $(GOSRC) Makefile teleport
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
	go test -v ./... 

.PHONY:
teleport:
	cd ../teleport; git checkout $(TELEPORTVER)
	$(MAKE) -C ../teleport clean
