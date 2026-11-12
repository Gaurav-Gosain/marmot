# Fix rate with go build output and with marmot JSON

`go run ./tests/diag/agentfix [-hard] -model claude-haiku-5-5`. The model gets the build output
and the failing file and answers with the fixed file. A fix counts when the module builds with
`go build` and the program's output and exit status match the reference fix (`fixed.go.txt`);
cases marked `ambiguous` (several sound fixes) count when they build. The model is asked again,
with the new build output, only while the program does not build (at most 3 attempts). Per case
and attempt details, including the model's answer, are in `results.json` and
`results-hard.json`.

## Corpus: 65 single-file programs, one mistake each (tests/diag/testdata)

| output the model saw | cases | fixed correctly | compiles but wrong | attempts per case |
| --- | --- | --- | --- | --- |
| go build | 65 | 61 (94%) | 4 | 1.00 |
| marmot JSON | 65 | 64 (98%) | 1 | 1.00 |

## Hard: 15 modules, cause in another file or package, model sees only the failing file (tests/diag/hard)

| output the model saw | cases | fixed correctly | on the first try | attempts per case |
| --- | --- | --- | --- | --- |
| go build | 15 | 15 (100%) | 13 (87%) | 1.13 |
| marmot JSON | 15 | 15 (100%) | 14 (93%) | 1.07 |

## Reading the numbers

These are single runs; a case can go either way between runs, so differences of one or two
cases are noise. Earlier runs of the corpus with a stricter protocol gave 97% for both, and
92% against 91%.

Where marmot helped: a byte constant that overflows (marmot's help names a wider type; with the
go output the model wrapped the value instead), the string assigned to an int, an unused `err`,
and on the hard set fewer second attempts for an interface with two missing methods and for an
unexported constructor (marmot lists what the package exports). The reference fixes and
marmot's hints were written by the same author, so where a hint and the reference agree (the
overflow case) the comparison favors marmot.

Where it did not matter: a small model fixes almost every one of these mistakes from go's
output alone, because go's messages already name the type, the method or the have and want
signatures, and the failing file is small. The gap this benchmark can show is small by
construction. Larger gains should show where the model cannot see the declarations at all
(no file given, only the output), with several errors per build, and in token cost per fix.
