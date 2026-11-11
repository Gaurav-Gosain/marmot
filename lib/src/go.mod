// This go.mod only keeps the overlay out of the root module, so that
// `go test ./...` and `go vet ./...` at the repo root skip lib/src (its
// packages import GOROOT internal packages and are compiled by marmot).
// The marmot loader must not merge this file into the GOROOT tree.
module marmot.invalid/lib/src

go 1.27
