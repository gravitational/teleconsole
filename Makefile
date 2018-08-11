# To create a new release of Teleconsole:
#   - make your changes
#   - bump VERSION variable in this Makefile
#   - run 'make'
#   - THEN commit & push to git
#   - run `make release` to create and push the new git tag

# To bump the version, edit this variable and run `make version`
export VERSION=0.3.2-alpha
OUT=out/teleconsole
GOSRC=$(shell find -name "*.go" -print)

# Default target: out/teleconsole
$(OUT): $(GOSRC) Makefile
	$(MAKE) -C ../teleport clean
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


.PHONY:deps
deps:
	go get github.com/fatih/color
	go get github.com/Sirupsen/logrus
	go get github.com/julienschmidt/httprouter
	go get golang.org/x/text/encoding
	go get golang.org/x/text/encoding/unicode
	go get rsc.io/letsencrypt
	# NOTE: teleport repo must be checked out with tag v2.0.0-alpha.3

