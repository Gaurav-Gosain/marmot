# Fix rate with go build output and with marmot JSON

`go run ./tests/diag/agentfix -model claude-haiku-5-5`, 65 cases, up to 3 attempts. A fix counts
when it builds with `go build` and the program's output and exit status match the reference fix
(`fixed.go.txt`). Details per case and attempt are in `results.json`.

| output the model saw | cases | fixed correctly | on the first try | attempts per case | compiles but wrong |
| --- | --- | --- | --- | --- | --- |
| go build | 65 | 60 (92%) | 60 (92%) | 1.15 | 5 |
| marmot JSON | 65 | 59 (91%) | 59 (91%) | 1.18 | 6 |

marmot does not win on this corpus. Every case is one small file with one mistake, and the model
sees the whole file with either output, so the location and the message are enough. Most of
the misses are cases with more than one reasonable fix (`area(3)` to `area(3, 3)` or `area(3, 4)`,
which `var x` to keep, what to do with an unused comparison), where the single reference output
rejects a sound fix in both conditions. The cases that differ between the conditions (an unused
`err`, a return value, an extra argument) went one way in one run and could go the other way in
the next.

What this corpus does not measure, and where the extra context should matter: errors whose
cause is in another file or package (interface and constraint declarations, called functions),
several errors at once, larger files where the model is not given the whole package, and token
cost per fix.
