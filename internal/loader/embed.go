package loader

import (
	"crypto/sha256"
	"fmt"
	"go/ast"
	"go/types"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/tools/go/packages"
)

// //go:embed support. The loader rewrites every package variable that has
// a //go:embed directive so that it has an initializer holding the
// embedded data, through go/packages overlays, and loads again:
//
//	string   -> var v string = "<contents>"
//	[]byte   -> var v []byte = []byte("<contents>")
//	embed.FS -> var v embed.FS = zz_marmotEmbedFS(name, data, hash, ...)
//
// zz_marmotEmbedFS lives in an extra overlay file of the package and
// builds the FS value with the layout of embed.FS (a pointer to a sorted
// []file{name, data string; hash [16]byte}), as cmd/compile does.

const embedHelperFile = "zz_marmot_embed.go"

// embedOverlays returns the overlay files for the embed variables of pkgs
// (nil if there are none) and the helper files that must be added to file
// pattern lists.
//
// With inline, the helper goes into an edited file of the package (imports
// on its package clause line, declarations at its end) instead of a file
// of its own: the full load passes the edited files to the parser
// (packages.Config.ParseFile), not to the go command, which may not
// overlay or add files beneath GOMODCACHE.
func embedOverlays(pkgs []*packages.Package, inline bool) (map[string][]byte, []string, error) {
	var over map[string][]byte
	var helpers []string
	for _, p := range pkgs {
		jumps := asmJumps(p)
		if len(p.EmbedFiles) == 0 && len(p.EmbedPatterns) == 0 && len(jumps) == 0 {
			continue
		}
		needFS := false
		fsFile, fsName := "", 0
		for _, f := range p.Syntax {
			name := p.Fset.File(f.Pos()).Name()
			var edits []embedEditAt
			fs := false
			if len(p.EmbedFiles) > 0 || len(p.EmbedPatterns) > 0 {
				var err error
				if edits, fs, err = embedEdits(p, f); err != nil {
					return nil, nil, err
				}
			}
			edits = append(edits, asmEdits(p, f, jumps)...)
			if len(edits) == 0 {
				continue
			}
			needFS = needFS || fs
			src, err := os.ReadFile(name)
			if err != nil {
				return nil, nil, err
			}
			if fs && fsFile == "" {
				fsFile, fsName = name, p.Fset.Position(f.Name.End()).Offset
			}
			// Apply from the end so offsets stay valid.
			sort.SliceStable(edits, func(a, b int) bool { return edits[a].pos > edits[b].pos })
			for _, e := range edits {
				off := e.pos
				src = append(src[:off], append([]byte(e.text), src[off:]...)...)
			}
			if over == nil {
				over = map[string][]byte{}
			}
			over[name] = src
		}
		if needFS && inline {
			// The edits come after the package clause, so its offset holds.
			src := over[fsFile]
			imp := []byte("; import (zz_marmot_embed \"embed\"; zz_marmot_unsafe \"unsafe\")")
			src = append(src[:fsName], append(imp, src[fsName:]...)...)
			body := fmt.Sprintf(embedHelperSrc, p.Name)
			body = body[strings.Index(body, "type zz_marmotEmbedFile"):]
			body = strings.NewReplacer("embed.FS", "zz_marmot_embed.FS", "unsafe.Pointer", "zz_marmot_unsafe.Pointer").Replace(body)
			over[fsFile] = append(append(src, '\n'), body...)
		} else if needFS && len(p.GoFiles) > 0 {
			h := filepath.Join(filepath.Dir(p.GoFiles[0]), embedHelperFile)
			over[h] = []byte(fmt.Sprintf(embedHelperSrc, p.Name))
			helpers = append(helpers, h)
		}
	}
	return over, helpers, nil
}

const embedHelperSrc = `package %s

import (
	"embed"
	"unsafe"
)

type zz_marmotEmbedFile struct {
	name string
	data string
	hash [16]byte
}

func zz_marmotEmbedFS(kv ...string) embed.FS {
	files := make([]zz_marmotEmbedFile, len(kv)/3)
	for i := range files {
		files[i].name, files[i].data = kv[3*i], kv[3*i+1]
		copy(files[i].hash[:], kv[3*i+2])
	}
	var fs embed.FS
	*(**[]zz_marmotEmbedFile)(unsafe.Pointer(&fs)) = &files
	return fs
}
`

func embedEdits(p *packages.Package, f *ast.File) ([]embedEditAt, bool, error) {
	var out []embedEditAt
	needFS := false
	dir := ""
	if len(p.GoFiles) > 0 {
		dir = filepath.Dir(p.GoFiles[0])
	}
	for _, d := range f.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok.String() != "var" {
			continue
		}
		for _, s := range gd.Specs {
			vs := s.(*ast.ValueSpec)
			doc := vs.Doc
			if doc == nil && len(gd.Specs) == 1 {
				doc = gd.Doc
			}
			pats, err := embedPatterns(doc)
			if err != nil {
				return nil, false, fmt.Errorf("%s: %v", p.Fset.Position(vs.Pos()), err)
			}
			if len(pats) == 0 {
				continue
			}
			if len(vs.Names) != 1 || len(vs.Values) != 0 || vs.Type == nil {
				return nil, false, fmt.Errorf("%s: go:embed cannot apply to var with initializer or multiple names", p.Fset.Position(vs.Pos()))
			}
			obj := p.TypesInfo.Defs[vs.Names[0]]
			if obj == nil {
				continue
			}
			files, err := matchEmbed(dir, pats, p.EmbedFiles)
			if err != nil {
				return nil, false, fmt.Errorf("%s: %v", p.Fset.Position(vs.Pos()), err)
			}
			var text string
			switch kind := embedKind(obj.Type()); kind {
			case "string", "bytes":
				if len(files) != 1 || strings.HasSuffix(files[0], "/") {
					return nil, false, fmt.Errorf("%s: invalid go:embed: multiple files for type %s", p.Fset.Position(vs.Pos()), obj.Type())
				}
				data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(files[0])))
				if err != nil {
					return nil, false, err
				}
				text = " = " + strconv.Quote(string(data))
				if kind == "bytes" {
					text = " = []byte(" + strconv.Quote(string(data)) + ")"
				}
			case "fs":
				needFS = true
				var b strings.Builder
				b.WriteString(" = zz_marmotEmbedFS(")
				for i, name := range embedFSList(files) {
					if i > 0 {
						b.WriteString(", ")
					}
					data, sum := "", ""
					if !strings.HasSuffix(name, "/") {
						raw, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
						if err != nil {
							return nil, false, err
						}
						h := sha256.Sum256(raw)
						data, sum = string(raw), string(h[:16])
					}
					fmt.Fprintf(&b, "%s, %s, %s", strconv.Quote(name), strconv.Quote(data), strconv.Quote(sum))
				}
				b.WriteString(")")
				text = b.String()
			default:
				return nil, false, fmt.Errorf("%s: go:embed cannot apply to var of type %s", p.Fset.Position(vs.Pos()), obj.Type())
			}
			out = append(out, embedEditAt{pos: p.Fset.File(vs.Pos()).Offset(vs.Type.End()), text: text})
		}
	}
	return out, needFS, nil
}

type embedEditAt struct {
	pos  int
	text string
}

func embedKind(t types.Type) string {
	if n, ok := types.Unalias(t).(*types.Named); ok {
		if o := n.Obj(); o.Pkg() != nil && o.Pkg().Path() == "embed" && o.Name() == "FS" {
			return "fs"
		}
	}
	switch u := t.Underlying().(type) {
	case *types.Basic:
		if u.Kind() == types.String {
			return "string"
		}
	case *types.Slice:
		if b, ok := u.Elem().Underlying().(*types.Basic); ok && b.Kind() == types.Byte {
			return "bytes"
		}
	}
	return ""
}

// embedPatterns parses the //go:embed lines of a doc comment.
func embedPatterns(doc *ast.CommentGroup) ([]string, error) {
	if doc == nil {
		return nil, nil
	}
	var pats []string
	for _, c := range doc.List {
		rest, ok := strings.CutPrefix(c.Text, "//go:embed")
		if !ok || rest != "" && rest[0] != ' ' && rest[0] != '\t' {
			continue
		}
		rest = strings.TrimSpace(rest)
		for rest != "" {
			var pat string
			switch rest[0] {
			case '"', '`':
				q := rest[0]
				i := strings.IndexByte(rest[1:], q)
				if q == '"' {
					// Find the closing quote, skipping escapes.
					i = -1
					for k := 1; k < len(rest); k++ {
						if rest[k] == '\\' {
							k++
							continue
						}
						if rest[k] == '"' {
							i = k - 1
							break
						}
					}
				}
				if i < 0 {
					return nil, fmt.Errorf("invalid quoted string in //go:embed: %s", rest)
				}
				s, err := strconv.Unquote(rest[:i+2])
				if err != nil {
					return nil, fmt.Errorf("invalid quoted string in //go:embed: %s", rest)
				}
				pat, rest = s, rest[i+2:]
			default:
				i := strings.IndexAny(rest, " \t")
				if i < 0 {
					i = len(rest)
				}
				pat, rest = rest[:i], rest[i:]
			}
			pats = append(pats, pat)
			rest = strings.TrimSpace(rest)
		}
	}
	return pats, nil
}

// matchEmbed returns the files (slash separated, relative to dir) that
// patterns select, out of the package's embed files from go list.
func matchEmbed(dir string, pats []string, embedFiles []string) ([]string, error) {
	var rel []string
	for _, f := range embedFiles {
		r, err := filepath.Rel(dir, f)
		if err != nil {
			return nil, err
		}
		rel = append(rel, filepath.ToSlash(r))
	}
	seen := map[string]bool{}
	var out []string
	for _, pat := range pats {
		all := false
		if p, ok := strings.CutPrefix(pat, "all:"); ok {
			pat, all = p, true
		}
		n := 0
		for _, f := range rel {
			if ok, _ := path.Match(pat, f); ok {
				n++
				if !seen[f] {
					seen[f] = true
					out = append(out, f)
				}
				continue
			}
			// A pattern that names a directory selects the files under it,
			// except names starting with . or _ unless all: is given.
			parts := strings.Split(f, "/")
			for k := 1; k < len(parts); k++ {
				d := strings.Join(parts[:k], "/")
				if ok, _ := path.Match(pat, d); !ok {
					continue
				}
				hidden := false
				for _, e := range parts[k:] {
					if strings.HasPrefix(e, ".") || strings.HasPrefix(e, "_") {
						hidden = true
					}
				}
				if hidden && !all {
					break
				}
				n++
				if !seen[f] {
					seen[f] = true
					out = append(out, f)
				}
				break
			}
		}
		if n == 0 {
			return nil, fmt.Errorf("pattern %s: no matching files found", pat)
		}
	}
	sort.Strings(out)
	return out, nil
}

// embedFSList adds the parent directories ("dir/") of files and sorts the
// list the way package embed searches it (by directory, then element).
func embedFSList(files []string) []string {
	have := map[string]bool{}
	var list []string
	for _, f := range files {
		if have[f] {
			continue
		}
		have[f] = true
		list = append(list, f)
		for d := path.Dir(f); d != "." && !have[d+"/"]; d = path.Dir(d) {
			have[d+"/"] = true
			list = append(list, d+"/")
		}
	}
	sort.Slice(list, func(i, j int) bool {
		di, ei := embedSplit(list[i])
		dj, ej := embedSplit(list[j])
		return di < dj || di == dj && ei < ej
	})
	return list
}

// embedSplit is embed.split: "a/b/c/" -> "a/b", "c".
func embedSplit(name string) (dir, elem string) {
	name = strings.TrimSuffix(name, "/")
	i := strings.LastIndexByte(name, '/')
	if i < 0 {
		return ".", name
	}
	return name[:i], name[i+1:]
}
