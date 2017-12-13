FROM golang:1.9

ARG UID
ARG GID

RUN groupadd builder --gid=$GID -o; \
    useradd builder --uid=$UID --gid=$GID --create-home --shell=/bin/bash;

RUN (mkdir -p /go/src/github.com/gravitational/teleconsole && chown -R builder /go)
RUN (mkdir -p /go/bin)

ENV LANGUAGE="en_US.UTF-8" \
    LANG="en_US.UTF-8" \
    LC_ALL="en_US.UTF-8" \
    LC_CTYPE="en_US.UTF-8" \
    GOPATH="/go" \
    PATH="$PATH:/opt/go/bin:/go/bin"

VOLUME ["/go/src/github.com/gravitational/teleconsole"]
