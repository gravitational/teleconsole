VERSION=0.0.3-beta
OUT=out/teleconsole
GOSRC=$(shell find -name "*.go" -print)
TELEPORT=$(shell find ../../gravitational/teleport/lib -name "*.go" -print)

$(OUT): $(GOSRC) Makefile
	go build -i -ldflags -w -o $(OUT)

# Runs teleconsole against a server hosted in a local VM running
# as 'teleconsole.local' 
.PHONY:dev
dev: $(OUT)
	$(MAKE) -C ../telecast dev
	sleep 3
	out/teleconsole -s teleconsole.local:5000 -insecure -vv

.PHONY:version
version:
	@VERSION=$(VERSION) $(MAKE) -C version


.PHONY:clean
clean:
	rm -rf out

.PHONY:test
test:
	go test ./... -v

.PHONY:local
local: $(OUT)
	$(OUT) -s 10.0.51.22:1888 -d
