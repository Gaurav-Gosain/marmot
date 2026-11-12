package diag

import (
	"strings"
)

// Codes are stable: a code never changes meaning once released. New codes
// are added at the end.
const (
	CodeOther         = "M0000" // an error without a more specific code
	CodeSyntax        = "M0001"
	CodeUndefined     = "M0002"
	CodeNoFieldMethod = "M0003"
	CodeUnusedVar     = "M0004"
	CodeUnusedImport  = "M0005"
	CodeCannotUse     = "M0006"
	CodeMismatched    = "M0007"
	CodeReturnCount   = "M0008"
	CodeArgCount      = "M0009"
	CodeMissingReturn = "M0010"
	CodePkgNotFound   = "M0011"
	CodeAssignCount   = "M0012"
	CodeNoNewVars     = "M0013"
	CodeNotImplement  = "M0014"
	CodeRedeclared    = "M0015"
	CodeInvalidOp     = "M0016"
	CodeUsage         = "M0017"
	CodeLink          = "M0018"
	CodeInternal      = "M0019"
	CodePanic         = "M0020"
	CodeFatal         = "M0021"
	CodeTestFail      = "M0022"
	CodeImportCycle   = "M0023"
	CodeNotExported   = "M0024"
	CodeUnsupported   = "M0025"
)

// CodeInfo describes one code for marmot explain.
type CodeInfo struct {
	Code    string `json:"code"`
	Title   string `json:"title"`
	Text    string `json:"text"`
	Example string `json:"example,omitempty"`
	Fix     string `json:"fix,omitempty"` // the usual fix pattern
	match   *lazyRE
}

// codeTable is in match order: the first pattern that matches a message
// gives its code.
var codeTable = []CodeInfo{
	{Code: CodeSyntax, Title: "syntax error",
		Text:    "The file does not parse. The position is where the parser gave up, which is often just after the real mistake: look at the end of the previous line for a missing bracket, comma or operand.",
		Example: "x := 1 +\n}",
		Fix:     "Complete the expression or statement, or close the open bracket.",
		match:   lazy(`^(expected |missing '|illegal |syntax error|unexpected |string literal not terminated|raw string literal not terminated|rune literal |comment not terminated|invalid character|exponent has no digits|invalid digit|'_' must separate)`)},
	{Code: CodeImportCycle, Title: "import cycle",
		Text:  "Two or more packages import each other. Go does not allow import cycles. Move the shared code into a third package that both import.",
		match: lazy(`import cycle not allowed`)},
	{Code: CodePkgNotFound, Title: "package not found",
		Text:    "An imported package is not in the standard library, the main module, or any required module.",
		Example: "import \"example.com/lib\"",
		Fix:     "Run go get for the module, fix the import path, or remove the import.",
		match:   lazy(`(no required module provides package|cannot find package|is not in std|could not import|package .* is not in GOROOT|no non-test Go files|matched no packages|directory not found|cannot find module providing)`)},
	{Code: CodeNotExported, Title: "name not exported",
		Text:    "The name exists in the package but starts with a lower case letter, so only that package can use it.",
		Example: "fmt.println(\"hi\")",
		Fix:     "Use the exported name (upper case first letter).",
		match:   lazy(`(not exported by package|cannot refer to unexported)`)},
	{Code: CodeUndefined, Title: "undefined name",
		Text:    "The name is not declared in this scope, the package, or the universe scope. It may be misspelled, declared in a file excluded by build tags, or belong to a package that is not imported.",
		Example: "fmt.Printn(\"hi\")\nx := strings.ToUpper(s) // strings not imported",
		Fix:     "Fix the spelling, or import the package that declares it.",
		match:   lazy(`^undefined: `)},
	{Code: CodeNoFieldMethod, Title: "no such field or method",
		Text:    "The selector names a field or method the type does not have. For methods, check the receiver: a method with a pointer receiver is not in the method set of the value type when the value is not addressable.",
		Example: "var b strings.Builder\nb.Write String(\"x\")",
		Fix:     "Use one of the fields or methods of the type.",
		match:   lazy(`undefined \(type .* has no (field or )?method|has no field or method`)},
	{Code: CodeUnusedVar, Title: "declared and not used",
		Text:    "Go rejects local variables that are never read. Assigning to a variable does not count as a use.",
		Example: "count := 3 // never read",
		Fix:     "Use the variable, remove it, or assign the value to _.",
		match:   lazy(`(declared and not used|declared but not used)`)},
	{Code: CodeUnusedImport, Title: "imported and not used",
		Text:    "Go rejects imports that the file does not use.",
		Example: "import \"os\" // nothing in the file uses os",
		Fix:     "Remove the import, or use _ as its name if it is imported only for its init side effects.",
		match:   lazy(`imported( as \w+)? and not used`)},
	{Code: CodeNotImplement, Title: "does not implement interface",
		Text:  "A value is used as an interface but its type lacks a method of the interface, or has it with a different signature or with a pointer receiver only.",
		Fix:   "Add the missing method, fix its signature, or pass a pointer when the method has a pointer receiver.",
		match: lazy(`does not implement`)},
	{Code: CodeCannotUse, Title: "type mismatch",
		Text:    "A value of one type is used where another type is required: as a function argument, in an assignment, in a return statement, or in a composite literal.",
		Example: "func add(a, b int) int\nadd(1, \"two\")",
		Fix:     "Convert the value, or change the declaration of the destination.",
		match:   lazy(`^cannot use `)},
	{Code: CodeMismatched, Title: "mismatched types",
		Text:    "Both operands of a binary operator must have identical types (untyped constants convert implicitly).",
		Example: "var n int; var f float64\n_ = n + f",
		Fix:     "Convert one operand to the type of the other.",
		match:   lazy(`mismatched types`)},
	{Code: CodeReturnCount, Title: "wrong number of return values",
		Text:  "The return statement gives a different number of values than the function signature declares.",
		Fix:   "Return exactly the values in the signature, or change the signature.",
		match: lazy(`^(too many|not enough) return values`)},
	{Code: CodeArgCount, Title: "wrong number of arguments",
		Text:  "The call passes a different number of arguments than the function takes.",
		Fix:   "Pass the arguments the signature lists.",
		match: lazy(`^(too many|not enough) arguments in call`)},
	{Code: CodeMissingReturn, Title: "missing return",
		Text:  "A function with results can reach its closing brace without a return statement.",
		Fix:   "Add a return at the end, or a panic for a case that cannot happen.",
		match: lazy(`^missing return`)},
	{Code: CodeAssignCount, Title: "assignment count mismatch",
		Text:    "The number of variables on the left does not match the number of values on the right.",
		Example: "a, b := strconv.Atoi(s) // ok\nn := strconv.Atoi(s)    // M0012",
		Fix:     "Receive every result, using _ for the ones you do not need.",
		match:   lazy(`^assignment mismatch|in single-value context`)},
	{Code: CodeNoNewVars, Title: "no new variables",
		Text:  "A short variable declaration (:=) must declare at least one new variable.",
		Fix:   "Use = instead of :=.",
		match: lazy(`no new variables on left side of :=`)},
	{Code: CodeRedeclared, Title: "redeclared",
		Text:  "The same name is declared twice in one scope.",
		Fix:   "Rename one of the declarations or remove the duplicate.",
		match: lazy(`(redeclared|already declared|method .* already declared)`)},
	{Code: CodeInvalidOp, Title: "invalid operation",
		Text:  "The operator or conversion is not defined for the operand types.",
		match: lazy(`^(invalid operation|cannot convert|invalid argument|cannot call|cannot assign|cannot range|cannot index|cannot slice|invalid composite literal|non-boolean)`)},
	{Code: CodeUsage, Title: "bad command line",
		Text:  "The command line names no buildable main package, or a flag value is not valid.",
		match: lazy(`(is not a main package|is not supported; name one main package|^bad -|^unsupported -target|no packages matched|^flag provided but not defined)`)},
	{Code: CodeLink, Title: "link error",
		Text:  "The program compiled but did not link: a function has no body (no assembly or runtime overlay provides it), or a symbol is missing from the runtime library.",
		Fix:   "If the function comes from the standard library, marmot does not support it yet; report it. For your own code, give the function a body.",
		match: lazy(`(Undefined symbols|undefined reference|undefined symbol|missing function body|ld: |linker command failed)`)},
	{Code: CodeUnsupported, Title: "not supported by marmot",
		Text:  "The program uses a feature marmot does not support yet (for example cgo, plugins, or a body-less function without an overlay).",
		match: lazy(`(not supported|unsupported|cgo)`)},
	{Code: CodeInternal, Title: "internal compiler error",
		Text:  "marmot failed on a valid program. This is a bug in marmot, not in your code. The report bundle has what is needed to reproduce it.",
		Fix:   "Report the bug with the bundle. As a workaround try -O 0, or MARMOT_TIER0=0.",
		match: lazy(`(marmot-backend failed|internal error|marmot-backend timed out|LLVM ERROR|Assertion .* failed|^panic: |marmot-pcln|dsymutil failed|clang failed|failed: signal)`)},
	{Code: CodePanic, Title: "runtime panic",
		Text: "The program panicked and no deferred recover stopped it. The trace lists the goroutine's frames, innermost first."},
	{Code: CodeFatal, Title: "fatal runtime error",
		Text: "The runtime stopped the program: a deadlock, out of memory, a concurrent map write, or a broken invariant. Fatal errors cannot be recovered."},
	{Code: CodeTestFail, Title: "test failed",
		Text: "A test called t.Error, t.Fatal or panicked."},
	{Code: CodeOther, Title: "error",
		Text: "An error without a more specific code. The message says what is wrong."},
}

var codeIndex = func() map[string]*CodeInfo {
	m := map[string]*CodeInfo{}
	for i := range codeTable {
		m[codeTable[i].Code] = &codeTable[i]
	}
	return m
}()

// Classify returns the code for a message.
func Classify(msg string) string {
	first, _, _ := strings.Cut(msg, "\n")
	for i := range codeTable {
		if m := codeTable[i].match; m != nil && m.MatchString(first) {
			return codeTable[i].Code
		}
	}
	return CodeOther
}

// Lookup returns the description of a code, or nil.
func Lookup(code string) *CodeInfo {
	return codeIndex[strings.ToUpper(code)]
}

// AllCodes lists every code in numeric order.
func AllCodes() []CodeInfo {
	out := make([]CodeInfo, 0, len(codeTable))
	for i := 0; i <= 99; i++ {
		if c := codeIndex[codeName(i)]; c != nil {
			out = append(out, *c)
		}
	}
	return out
}

func codeName(i int) string {
	const digits = "0123456789"
	return "M00" + string(digits[i/10]) + string(digits[i%10])
}
