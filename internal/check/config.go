package check

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"go.yaml.in/yaml/v3"
)

// lintConfig is the part of a golangci-lint v2 configuration that marmot
// check reads, so that a project's existing .golangci.yml works unchanged.
type lintConfig struct {
	Version string `yaml:"version"`
	Run     struct {
		Tests     *bool    `yaml:"tests"`
		BuildTags []string `yaml:"build-tags"`
	} `yaml:"run"`
	Linters struct {
		Default    string       `yaml:"default"`
		Enable     []string     `yaml:"enable"`
		Disable    []string     `yaml:"disable"`
		Settings   lintSettings `yaml:"settings"`
		Exclusions struct {
			Generated   string        `yaml:"generated"`
			Presets     []string      `yaml:"presets"`
			Rules       []excludeRule `yaml:"rules"`
			Paths       []string      `yaml:"paths"`
			PathsExcept []string      `yaml:"paths-except"`
			WarnUnused  bool          `yaml:"warn-unused"`
		} `yaml:"exclusions"`
	} `yaml:"linters"`
	Formatters struct {
		Enable []string `yaml:"enable"`
	} `yaml:"formatters"`

	// path is the file the configuration came from, dir its directory.
	path, dir string
	// compiled exclusion rules
	rules []compiledRule
	paths []*regexp.Regexp
}

type lintSettings struct {
	Errcheck struct {
		ExcludeFunctions         []string `yaml:"exclude-functions"`
		CheckTypeAssertions      bool     `yaml:"check-type-assertions"`
		CheckBlank               bool     `yaml:"check-blank"`
		DisableDefaultExclusions bool     `yaml:"disable-default-exclusions"`
	} `yaml:"errcheck"`
	Govet struct {
		EnableAll  bool     `yaml:"enable-all"`
		DisableAll bool     `yaml:"disable-all"`
		Enable     []string `yaml:"enable"`
		Disable    []string `yaml:"disable"`
	} `yaml:"govet"`
	Staticcheck struct {
		Checks                  []string `yaml:"checks"`
		Initialisms             []string `yaml:"initialisms"`
		DotImportWhitelist      []string `yaml:"dot-import-whitelist"`
		HTTPStatusCodeWhitelist []string `yaml:"http-status-code-whitelist"`
	} `yaml:"staticcheck"`
	Gosec struct {
		Includes []string `yaml:"includes"`
		Excludes []string `yaml:"excludes"`
	} `yaml:"gosec"`
	Ineffassign struct {
		CheckEscapingErrors bool `yaml:"check-escaping-errors"`
	} `yaml:"ineffassign"`
	Unused struct {
		FieldWritesAreUses     *bool `yaml:"field-writes-are-uses"`
		PostStatementsAreReads bool  `yaml:"post-statements-are-reads"`
		ExportedFieldsAreUsed  *bool `yaml:"exported-fields-are-used"`
		ParametersAreUsed      *bool `yaml:"parameters-are-used"`
		LocalVariablesAreUsed  *bool `yaml:"local-variables-are-used"`
		GeneratedIsUsed        *bool `yaml:"generated-is-used"`
	} `yaml:"unused"`
}

type excludeRule struct {
	Path       string   `yaml:"path"`
	PathExcept string   `yaml:"path-except"`
	Linters    []string `yaml:"linters"`
	Text       string   `yaml:"text"`
	Source     string   `yaml:"source"`
}

type compiledRule struct {
	path, pathExcept, text, source *regexp.Regexp
	linters                        []string
}

// configNames are the file names golangci-lint looks for, in its order.
var configNames = []string{".golangci.yml", ".golangci.yaml", ".golangci.toml", ".golangci.json"}

// findLintConfig looks for a golangci-lint configuration in dir and its
// parents up to the module or repository root. It returns nil when there
// is none.
func findLintConfig(dir string) (*lintConfig, error) {
	for d := dir; ; d = filepath.Dir(d) {
		for _, n := range configNames {
			p := filepath.Join(d, n)
			if _, err := os.Stat(p); err == nil {
				return readLintConfig(p)
			}
		}
		if _, err := os.Stat(filepath.Join(d, ".git")); err == nil {
			return nil, nil
		}
		if filepath.Dir(d) == d {
			return nil, nil
		}
	}
}

func readLintConfig(path string) (*lintConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	c := &lintConfig{path: path, dir: filepath.Dir(path)}
	switch filepath.Ext(path) {
	case ".yml", ".yaml", ".json": // JSON is YAML
		if err := yaml.Unmarshal(data, c); err != nil {
			return nil, fmt.Errorf("%s: %v", path, err)
		}
	default:
		return nil, fmt.Errorf("%s: only YAML and JSON golangci-lint configurations are read", path)
	}
	if c.Version != "2" {
		return nil, fmt.Errorf("%s: version %q: marmot check reads golangci-lint v2 configurations (run golangci-lint migrate)", path, c.Version)
	}
	for _, r := range c.Linters.Exclusions.Rules {
		cr := compiledRule{linters: r.Linters}
		for _, x := range []struct {
			src string
			dst **regexp.Regexp
		}{{r.Path, &cr.path}, {r.PathExcept, &cr.pathExcept}, {r.Text, &cr.text}, {r.Source, &cr.source}} {
			if x.src == "" {
				continue
			}
			re, err := regexp.Compile(x.src)
			if err != nil {
				return nil, fmt.Errorf("%s: exclusion rule %q: %v", path, x.src, err)
			}
			*x.dst = re
		}
		c.rules = append(c.rules, cr)
	}
	for _, p := range c.Linters.Exclusions.Paths {
		re, err := regexp.Compile(p)
		if err != nil {
			return nil, fmt.Errorf("%s: exclusion path %q: %v", path, p, err)
		}
		c.paths = append(c.paths, re)
	}
	for _, name := range c.Linters.Exclusions.Presets {
		for _, r := range exclusionPresets[name] {
			c.rules = append(c.rules, compiledRule{text: regexp.MustCompile(r.Text), linters: r.Linters})
		}
	}
	return c, nil
}

// exclusionPresets are golangci-lint's named exclusion rule sets.
var exclusionPresets = map[string][]excludeRule{
	"comments": {
		{Text: "(ST1000|ST1020|ST1021|ST1022)", Linters: []string{"staticcheck"}},
		{Text: `exported (.+) should have comment( \(or a comment on this block\))? or be unexported`, Linters: []string{"revive"}},
		{Text: `package comment should be of the form "(.+)..."`, Linters: []string{"revive"}},
		{Text: `comment on exported (.+) should be of the form "(.+)..."`, Linters: []string{"revive"}},
		{Text: `should have a package comment`, Linters: []string{"revive"}},
	},
	"std-error-handling": {
		{Text: "(?i)Error return value of .((os\\.)?std(out|err)\\..*|.*Close|.*Flush|os\\.Remove(All)?|.*print(f|ln)?|os\\.(Un)?Setenv). is not checked", Linters: []string{"errcheck"}},
	},
	"common-false-positives": {
		{Text: "G103: Use of unsafe calls should be audited", Linters: []string{"gosec"}},
		{Text: "G204: Subprocess launched with variable", Linters: []string{"gosec"}},
		{Text: "G304: Potential file inclusion via variable", Linters: []string{"gosec"}},
	},
	"legacy": {
		{Text: "(possible misuse of unsafe.Pointer|should have signature)", Linters: []string{"govet"}},
		{Text: "SA4011", Linters: []string{"staticcheck"}},
		{Text: "G104", Linters: []string{"gosec"}},
		{Text: "(G301|G302|G307): Expect (directory permissions to be 0750|file permissions to be 0600) or less", Linters: []string{"gosec"}},
	},
}

// excluded reports whether an issue of the linter with the text at the
// file (relative to the configuration directory, slash separated) is
// excluded by a rule. src is the source line, for rules with a source
// pattern.
func (c *lintConfig) excluded(linter, rel, text, src string) bool {
	if c == nil {
		return false
	}
	for _, p := range c.paths {
		if p.MatchString(rel) {
			return true
		}
	}
	for _, r := range c.rules {
		if len(r.linters) > 0 && !contains(r.linters, linter) {
			continue
		}
		if r.path != nil && !r.path.MatchString(rel) {
			continue
		}
		if r.pathExcept != nil && r.pathExcept.MatchString(rel) {
			continue
		}
		if r.text != nil && !r.text.MatchString(text) {
			continue
		}
		if r.source != nil && !r.source.MatchString(src) {
			continue
		}
		return true
	}
	return false
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if strings.EqualFold(x, s) {
			return true
		}
	}
	return false
}
