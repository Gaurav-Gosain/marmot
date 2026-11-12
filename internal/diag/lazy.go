package diag

import (
	"regexp"
	"sync"
)

// lazyRE compiles its pattern on first use, so a build without errors does
// not pay for the patterns at startup.
type lazyRE struct {
	once sync.Once
	src  string
	re   *regexp.Regexp
}

func lazy(src string) *lazyRE { return &lazyRE{src: src} }

func (l *lazyRE) get() *regexp.Regexp {
	l.once.Do(func() { l.re = regexp.MustCompile(l.src) })
	return l.re
}

func (l *lazyRE) MatchString(s string) bool            { return l.get().MatchString(s) }
func (l *lazyRE) FindStringSubmatch(s string) []string { return l.get().FindStringSubmatch(s) }
func (l *lazyRE) FindAllStringSubmatch(s string, n int) [][]string {
	return l.get().FindAllStringSubmatch(s, n)
}
