// Package wasmfile reads and rewrites a WebAssembly module at the section
// level: the last step of a wasip1/wasm link keeps only the exports the
// program needs, adds a producers section, and strips the names section
// for -s.
package wasmfile

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"strings"
)

// Section ids (the binary format, section 5.5).
const (
	SecCustom = 0
	SecExport = 7
)

// Section is one section: its id and its contents (after the size).
type Section struct {
	ID   byte
	Data []byte
}

// Name is a custom section's name ("" for others).
func (s Section) Name() string {
	if s.ID != SecCustom {
		return ""
	}
	n, k, err := uleb(s.Data)
	if err != nil || k+int(n) > len(s.Data) {
		return ""
	}
	return string(s.Data[k : k+int(n)])
}

var magic = []byte{0, 'a', 's', 'm', 1, 0, 0, 0}

// Parse splits a module into its sections.
func Parse(b []byte) ([]Section, error) {
	if !bytes.HasPrefix(b, magic) {
		return nil, errors.New("not a wasm module (version 1)")
	}
	b = b[len(magic):]
	var ss []Section
	for len(b) > 0 {
		id := b[0]
		n, k, err := uleb(b[1:])
		if err != nil {
			return nil, err
		}
		start := 1 + k
		if uint64(len(b)-start) < n {
			return nil, fmt.Errorf("section %d cut short", id)
		}
		ss = append(ss, Section{id, b[start : start+int(n)]})
		b = b[start+int(n):]
	}
	return ss, nil
}

// Encode writes the sections back as a module.
func Encode(ss []Section) []byte {
	out := slices.Clone(magic)
	for _, s := range ss {
		out = append(out, s.ID)
		out = appendUleb(out, uint64(len(s.Data)))
		out = append(out, s.Data...)
	}
	return out
}

// Export is one export entry.
type Export struct {
	Name  string
	Kind  byte // 0 func, 1 table, 2 memory, 3 global, 4 tag
	Index uint64
}

// Exports decodes an export section.
func Exports(data []byte) ([]Export, error) {
	n, k, err := uleb(data)
	if err != nil {
		return nil, err
	}
	data = data[k:]
	es := make([]Export, 0, n)
	for i := uint64(0); i < n; i++ {
		l, k, err := uleb(data)
		if err != nil || k+int(l)+1 > len(data) {
			return nil, errors.New("export section cut short")
		}
		name := string(data[k : k+int(l)])
		kind := data[k+int(l)]
		idx, k2, err := uleb(data[k+int(l)+1:])
		if err != nil {
			return nil, err
		}
		es = append(es, Export{name, kind, idx})
		data = data[k+int(l)+1+k2:]
	}
	return es, nil
}

func encodeExports(es []Export) []byte {
	out := appendUleb(nil, uint64(len(es)))
	for _, e := range es {
		out = appendName(out, e.Name)
		out = append(out, e.Kind)
		out = appendUleb(out, e.Index)
	}
	return out
}

// Options says what Clean keeps and adds.
type Options struct {
	// Keep is the export names to keep; every other export is dropped.
	Keep []string
	// StripNames drops the "name" custom section and the DWARF sections
	// (.debug_*), as -s does.
	StripNames bool
	// Producers, when set, replaces the producers section with one
	// processed-by entry per pair (tool name, version).
	Producers [][2]string
}

// Clean rewrites a module as Options say.
func Clean(b []byte, o Options) ([]byte, error) {
	ss, err := Parse(b)
	if err != nil {
		return nil, err
	}
	var out []Section
	for _, s := range ss {
		switch {
		case s.ID == SecExport:
			es, err := Exports(s.Data)
			if err != nil {
				return nil, err
			}
			es = slices.DeleteFunc(es, func(e Export) bool { return !slices.Contains(o.Keep, e.Name) })
			s.Data = encodeExports(es)
		case s.ID == SecCustom && o.StripNames && (s.Name() == "name" || strings.HasPrefix(s.Name(), ".debug_")):
			continue
		case s.ID == SecCustom && s.Name() == "producers" && o.Producers != nil:
			continue
		}
		out = append(out, s)
	}
	if o.Producers != nil {
		out = append(out, producers(o.Producers))
	}
	return Encode(out), nil
}

// producers is the tool-conventions producers section with one field,
// processed-by.
func producers(tools [][2]string) Section {
	d := appendName(nil, "producers")
	d = appendUleb(d, 1)
	d = appendName(d, "processed-by")
	d = appendUleb(d, uint64(len(tools)))
	for _, t := range tools {
		d = appendName(d, t[0])
		d = appendName(d, t[1])
	}
	return Section{SecCustom, d}
}

func appendName(b []byte, s string) []byte {
	b = appendUleb(b, uint64(len(s)))
	return append(b, s...)
}

func uleb(b []byte) (uint64, int, error) {
	var v uint64
	for i := 0; i < len(b) && i < 10; i++ {
		v |= uint64(b[i]&0x7f) << (7 * i)
		if b[i]&0x80 == 0 {
			return v, i + 1, nil
		}
	}
	return 0, 0, errors.New("bad LEB128")
}

func appendUleb(b []byte, v uint64) []byte {
	for {
		c := byte(v & 0x7f)
		v >>= 7
		if v != 0 {
			c |= 0x80
		}
		b = append(b, c)
		if v == 0 {
			return b
		}
	}
}
