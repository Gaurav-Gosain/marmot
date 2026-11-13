# Stock standard library tests under `marmot test`

Input of tests/support for docs/support-matrix.md. Each row is the outcome of
`marmot test -short` on one stock package: result (ok, FAIL, build), tests
passed and failed, pass rate. Rerun the packages and replace the table and
the Measured line together.

Measured: darwin/arm64 laptop, 2026-11-12, before the fixes

| package | result | pass | fail | rate |
| --- | --- | --- | --- | --- |
| fmt | ok | 79 | 0 | 100% |
| strings | FAIL | 146 | 5 | 97% |
| bytes | FAIL | 175 | 3 | 98% |
| strconv | FAIL | 69 | 11 | 86% |
| sort | ok | 81 | 0 | 100% |
| slices | FAIL | 162 | 3 | 98% |
| maps | ok | 24 | 0 | 100% |
| math | ok | 111 | 0 | 100% |
| unicode | ok | 42 | 0 | 100% |
| unicode/utf8 | FAIL | 30 | 1 | 97% |
| time | FAIL | 46 | 2 | 96% |
| sync | build | 0 | 0 | - |
| sync/atomic | ok | 107 | 0 | 100% |
| context | FAIL | 68 | 1 | 99% |
| errors | ok | 92 | 0 | 100% |
| io | FAIL | 77 | 2 | 97% |
| bufio | FAIL | 88 | 1 | 99% |
| os | FAIL | 3525 | 11 | 100% |
| os/exec | FAIL | 23 | 57 | 29% |
| os/signal | FAIL | 5 | 1 | 83% |
| path | ok | 16 | 0 | 100% |
| path/filepath | FAIL | 59 | 1 | 98% |
| encoding/json | FAIL | 0 | 1 | 0% |
| encoding/binary | build | 0 | 0 | - |
| encoding/base64 | ok | 23 | 0 | 100% |
| encoding/hex | ok | 18 | 0 | 100% |
| encoding/csv | ok | 77 | 0 | 100% |
| encoding/xml | FAIL | 332 | 3 | 99% |
| encoding/pem | ok | 21 | 0 | 100% |
| regexp | ok | 75 | 0 | 100% |
| text/template | FAIL | 58 | 1 | 98% |
| html/template | FAIL | 0 | 0 | - |
| text/tabwriter | ok | 6 | 0 | 100% |
| text/scanner | ok | 22 | 0 | 100% |
| net/url | FAIL | 89 | 5 | 95% |
| net/http | build | 0 | 0 | - |
| net/http/httptest | FAIL | 51 | 3 | 94% |
| net | FAIL | 9 | 0 | 100% |
| mime | FAIL | 24 | 1 | 96% |
| mime/multipart | FAIL | 49 | 4 | 92% |
| crypto/sha256 | FAIL | 33 | 2 | 94% |
| crypto/md5 | FAIL | 17 | 1 | 94% |
| crypto/sha1 | FAIL | 17 | 1 | 94% |
| crypto/hmac | ok | 233 | 0 | 100% |
| crypto/rand | FAIL | 14 | 2 | 88% |
| crypto/aes | ok | 58 | 0 | 100% |
| crypto/tls | FAIL | 0 | 0 | - |
| crypto/x509 | FAIL | 0 | 0 | - |
| hash/crc32 | ok | 18 | 0 | 100% |
| hash/fnv | ok | 62 | 0 | 100% |
| container/heap | ok | 9 | 0 | 100% |
| container/list | ok | 11 | 0 | 100% |
| container/ring | ok | 15 | 0 | 100% |
| math/rand | FAIL | 25 | 2 | 93% |
| math/rand/v2 | ok | 25 | 0 | 100% |
| math/big | FAIL | 266 | 1 | 100% |
| math/bits | build | 0 | 0 | - |
| reflect | build | 0 | 0 | - |
| runtime | build | 0 | 0 | - |
| log | FAIL | 9 | 2 | 82% |
| log/slog | FAIL | 225 | 29 | 89% |
| flag | FAIL | 28 | 1 | 97% |
| embed | build | 0 | 0 | - |
| io/fs | FAIL | 5 | 0 | 100% |
| compress/gzip | ok | 23 | 0 | 100% |
| compress/flate | ok | 128 | 0 | 100% |
| archive/tar | FAIL | 1 | 2 | 33% |
| archive/zip | FAIL | 0 | 0 | - |
| html | ok | 6 | 0 | 100% |
| go/token | FAIL | 25 | 1 | 96% |
| go/scanner | ok | 25 | 0 | 100% |
| go/parser | FAIL | 42 | 0 | 100% |
| go/ast | FAIL | 23 | 1 | 96% |
| go/format | ok | 5 | 0 | 100% |
| go/types | FAIL | 70 | 1 | 99% |
| iter | FAIL | 10 | 10 | 50% |
| unique | build | 0 | 0 | - |
