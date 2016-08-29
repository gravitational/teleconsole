# To bump the version, edit this variable and run `make version`
VERSION=0.0.5-beta

OUT=out/teleconsole
GOSRC=$(shell find -name "*.go" -print)
TELEPORT=$(shell find ../../gravitational/teleport/lib -name "*.go" -print)

# Default target: out/teleconsole
$(OUT): $(GOSRC) Makefile
	go build -i -ldflags -w -o $(OUT)

# Runs teleconsole against a server hosted in a local VM running
# as 'teleconsole.local' 
.PHONY:dev
dev: $(OUT)
	$(MAKE) -C ../telecast dev
	sleep 3
	out/teleconsole -s teleconsole.local:5000 -insecure -vv

# Make version bumps the version (commits it to Git automatically)
.PHONY:version
version:
	@VERSION=$(VERSION) $(MAKE) -C version
	git add .
	git commit -m "Version bump to $(VERSION)"
	$(MAKE) -C version gitref

.PHONY:clean
clean:
	rm -rf out

.PHONY:test
test:
	go test ./... -v

.PHONY:local
local: $(OUT)
	$(OUT) -s 10.0.51.22:1888 -d
