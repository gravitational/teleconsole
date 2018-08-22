## Building Teleconsole

Teleconsole is a thin layer on top of [Gravitational Teleport](https://github.com/gravitational/teleconsole)
but unfortunately it does not vendor it yet.

To build Teleconsole from source for your OS/Arch, do the following:

* Make sure you have Golang 1.9.7
* Make sure your `$GOPATH` is properly set, usually to `~/go`

Type the following:

```bash
$ mkdir -p ${GOPATH}/src/github.com/gravitational
$ cd ${GOPATH}/src/github.com/gravitational
$ git clone git@github.com:gravitational/teleport.git
$ git clone git@github.com:gravitational/teleconsole.git
$ cd teleconsole
$ make
```

This should produce a new `teleconsole` binary in the `out` directory

## Future Plans

We are migrating Teleconsole to the latest version of Teleport (soon to be
released 3.0). Once the migration is complete, Teleport will be vendored into
Teleconsole repository and it will be possible to simply 
`go get github.com/gravitational/teleconsole` on your platform of choice.
