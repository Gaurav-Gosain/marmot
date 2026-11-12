# Fix rate with go build output and with marmot JSON

`go run ./tests/diag/agentfix -model claude-haiku-5-5`, 65 cases, up to 3 attempts. A fix counts
when it builds with `go build` and the program's output and exit status match the reference fix
(`fixed.go.txt`). Details per case and attempt are in `results.json`.

| output the model saw | cases | fixed correctly | on the first try | attempts per case | compiles but wrong |
| --- | --- | --- | --- | --- | --- |
| go build | 65 | 63 (97%) | 63 (97%) | 1.06 | 2 |
| marmot JSON | 65 | 63 (97%) | 63 (97%) | 1.06 | 2 |

Five cases with several sound fixes (marked by an `ambiguous` file) are judged by compiling
only. A first run judged them by output too and gave 92% and 91%; it also used `-o /dev/null`,
which marmot then mishandled after a successful fix (fixed since), so that run is not used.

marmot does not win on this corpus. Every case is one small file with one mistake, and the model
sees the whole file with either output, so the location and the message are enough. The two
misses in each condition are not the same cases (an unused `err` and a string plus int on one
side, a pointer to an interface on both), which is within run to run noise.

What this corpus does not measure, and where the extra context should matter: errors whose
cause is in another file or package (interface and constraint declarations, called functions),
several errors at once, larger files where the model is not given the whole package, and token
cost per fix.
