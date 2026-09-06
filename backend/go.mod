module github.com/duncan-ireri/omarchy-apimock/backend

go 1.24

// Pin the exact toolchain so `go build` is byte-for-byte reproducible: the
// release checksum in checksums/ is verified against a build made with this
// version. Bump this and the checksum together.
toolchain go1.24.7
