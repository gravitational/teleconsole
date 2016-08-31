# To create a new release of Teleconsole:
#   - make your changes
#   - bump VERSION variable in this Makefile
#   - run `make release` to create git tags
#   - run `make publish` to create tarball and push it to S3

# To bump the version, edit this variable and run `make version`
VERSION=0.0.7-beta

OUT=out/teleconsole
GOSRC=$(shell find -name "*.go" -print)
TELEPORT=$(shell find ../../gravitational/teleport/lib -name "*.go" -print)
TARBALL="teleconsole-v$(VERSION)-`go env GOOS`-`go env GOARCH`.tar.bz2"

# Default target: out/teleconsole
$(OUT): $(GOSRC) Makefile
	$(MAKE) -C version gitref
	go build -i -ldflags -w -o $(OUT)

# Runs teleconsole against a server hosted in a local VM running
# as 'teleconsole.local' 
.PHONY:dev
dev: $(OUT)
	$(MAKE) -C ../telecast dev
	sleep 3
	out/teleconsole -s teleconsole.local:5000 -insecure -vv


# Makes a new release (pushes tags to Github)
.PHONY:release 
release: version
	git add .
	git commit -m "Version bump to $(VERSION)"
	git tag $(VERSION)
	git push --tags
	$(MAKE)

# Make version bumps the version 
.PHONY:version
version:
	@VERSION=$(VERSION) $(MAKE) -C version

# publish: creates a tarball of the current release and pushes it to S3
.PHONY:publish
publish: $(OUT)
	cd out && tar -cjf $(TARBALL) teleconsole 
	aws s3 cp out/$(TARBALL) s3://s3.gravitational.io/teleconsole/

.PHONY:clean
clean:
	rm -rf out

.PHONY:test
test:
	go test ./... -v

.PHONY:local
local: $(OUT)
	$(OUT) -s 10.0.51.22:1888 -d
