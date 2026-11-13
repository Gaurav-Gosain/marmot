package bind

import (
	"os"
	"path"

	"github.com/BurntSushi/toml"
)

// Config is bind.toml. Every field is optional.
type Config struct {
	Package   string   `toml:"package"`    // Go package name (default: the header's base name)
	Header    string   `toml:"header"`     // the header to bind
	PkgConfig string   `toml:"pkg-config"` // pkg-config name for the flags
	CFlags    []string `toml:"cflags"`
	LDFlags   []string `toml:"ldflags"`
	Files     []string `toml:"files"`    // more headers whose declarations are bound
	Prefixes  []string `toml:"prefixes"` // stripped from C names (sqlite3_, SQLITE_)
	Include   []string `toml:"include"`  // C names (globs) to bind; default all
	Exclude   []string `toml:"exclude"`  // C names (globs) to leave out

	// Types renames C types: "sqlite3" = "DB".
	Types map[string]string `toml:"types"`

	// Status describes int results that report errors.
	Status struct {
		Functions []string `toml:"functions"` // globs of functions whose int result is a status
		OK        []string `toml:"ok"`        // success values (default: 0)
		Message   string   `toml:"message"`   // C function turning a status into text
	} `toml:"status"`

	// Close names the C function that frees a handle type: "sqlite3" =
	// "sqlite3_close_v2". It becomes the handle's Close method.
	Close map[string]string `toml:"close"`

	// Owned lists functions whose pointer result the caller must free, with
	// the function that frees it: "sqlite3_expanded_sql" = "sqlite3_free".
	Owned map[string]string `toml:"owned"`

	// Borrowed lists functions whose char * or pointer result the caller
	// must not free (copied or wrapped, never freed).
	Borrowed []string `toml:"borrowed"`

	// Out lists out parameters: "sqlite3_open" = ["ppDb"].
	Out map[string][]string `toml:"out"`
}

// LoadConfig reads a bind.toml; a missing file gives the zero Config.
func LoadConfig(file string) (*Config, error) {
	c := &Config{}
	if file == "" {
		return c, nil
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	if _, err := toml.Decode(string(data), c); err != nil {
		return nil, err
	}
	return c, nil
}

func matchAny(globs []string, name string) bool {
	for _, g := range globs {
		if ok, _ := path.Match(g, name); ok {
			return true
		}
	}
	return false
}

func (c *Config) included(name string) bool {
	if len(c.Include) > 0 && !matchAny(c.Include, name) {
		return false
	}
	return !matchAny(c.Exclude, name)
}
