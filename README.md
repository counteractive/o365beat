# O365beat

Welcome to O365beat.

Ensure that this folder is at the following location:
`${GOPATH}/src/github.com/counteractive/o365beat`

## Getting Started with O365beat

### Requirements

* [Golang](https://golang.org/dl/) 1.7

### Build

To build the binary for O365beat run the command below. This will grab vendor dependencies if you don't have them already, and generate a binary in the same directory with the name o365beat.

```bash
make
```

### Run

To run O365beat with debugging output enabled, run:

```bash
./o365beat -c o365beat.yml -e -d "*" # add --strict.perms=false under WSL
```

### Test (none so far!)

To test O365beat, run the following command:

```bash
make testsuite
```

alternatively:

```bash
make unit-tests
make system-tests
make integration-tests
make coverage-report
```

The test coverage is reported in the folder `./build/coverage/`

### Update

Each beat has a template for the mapping in elasticsearch and a documentation for the fields
which is automatically generated based on `fields.yml` by running the following command.

```bash
make update
```

### Cleanup

To clean  O365beat source code, run the following command:

```bash
make fmt
```

To clean up the build directory and generated artifacts, run:

```bash
make clean
```

### Clone

To clone O365beat from the git repository, run the following commands:

```bash
mkdir -p ${GOPATH}/src/github.com/counteractive/o365beat
git clone https://github.com/counteractive/o365beat ${GOPATH}/src/github.com/counteractive/o365beat
```

For further development, check out the [beat developer guide](https://www.elastic.co/guide/en/beats/libbeat/current/new-beat.html).

## Packaging

The beat frameworks provides tools to cross-compile and package your beat for different platforms. This requires [docker](https://www.docker.com/) and vendor-ing as described above. To build packages of your beat, run the following command:

```bash
make release
```

This will fetch and create all images required for the build process. The whole process to finish can take several minutes.
