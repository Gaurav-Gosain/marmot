# World tier

Real open source Go modules, used to measure how much existing Go code compiles and behaves the
same under marmot. Each directory is its own Go module (`marmotworld/NAME`) with the dependency
pinned in `go.mod`, vendored under `vendor/` (license files included), a small deterministic
`main.go` that exercises it, and goldens `golden.out`, `golden.err`, `golden.exit` recorded with
stock Go (`go build -mod=vendor`). The conform runner treats a directory here as one test named
`world/NAME`.

Add a module (the only step that uses the network):

    tests/world/add.sh NAME MODULE VERSION      # creates the module, pins the dependency
    # write tests/world/NAME/main.go
    tests/world/add.sh NAME --vendor            # go mod tidy and go mod vendor
    go run ./tests/conform -update -dir world/NAME

Run: `go run ./tests/conform -dir world -j 2` (add `-modes dev,release`).

| Test | Module | Version |
| --- | --- | --- |
| base58 | `github.com/mr-tron/base58` | v1.2.0 |
| difflib | `github.com/pmezard/go-difflib` | v1.0.0 |
| doublestar | `github.com/bmatcuk/doublestar/v4` | v4.6.1 |
| godiff | `github.com/sergi/go-diff` | v1.3.1 |
| goldmark | `github.com/yuin/goldmark` | v1.7.4 |
| humanize | `github.com/dustin/go-humanize` | v1.0.1 |
| ini | `gopkg.in/ini.v1` | v1.67.0 |
| levenshtein | `github.com/agnivade/levenshtein` | v1.1.1 |
| lru | `github.com/hashicorp/golang-lru/v2` | v2.0.7 |
| pflag | `github.com/spf13/pflag` | v1.0.5 |
| rate | `golang.org/x/time` | v0.5.0 |
| semver | `github.com/Masterminds/semver/v3` | v3.2.1 |
| tablewriter | `github.com/olekukonko/tablewriter` | v0.0.5 |
| toml | `github.com/BurntSushi/toml` | v1.4.0 |
| uuid | `github.com/google/uuid` | v1.6.0 |
| yaml | `gopkg.in/yaml.v3` | v3.0.1 |

Licenses: each vendored module keeps its own LICENSE file in `vendor/<module path>/`.
