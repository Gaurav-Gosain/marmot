// Package pprof decodes pprof profiles (profile.proto, optionally gzipped)
// and aggregates them by function and by source line. It has no
// dependencies beyond the standard library.
package pprof

import (
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"slices"
	"sort"
)

// Profile is a decoded profile.
type Profile struct {
	SampleTypes []ValueType
	Samples     []Sample
	Locations   map[uint64]*Location
	Functions   map[uint64]*Function
	PeriodType  ValueType
	Period      int64
	DurationNs  int64
	DefaultType string
}

type ValueType struct{ Type, Unit string }

type Sample struct {
	Locs   []uint64 // innermost first
	Values []int64
}

type Location struct {
	ID      uint64
	Address uint64
	Lines   []Line // inlined frames, innermost first
}

type Line struct {
	Function uint64
	Line     int64
}

type Function struct {
	ID       uint64
	Name     string
	File     string
	StartLin int64
}

// Parse decodes a profile.
func Parse(data []byte) (*Profile, error) {
	if len(data) > 2 && data[0] == 0x1f && data[1] == 0x8b {
		zr, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		if data, err = io.ReadAll(zr); err != nil {
			return nil, err
		}
	}
	p := &Profile{Locations: map[uint64]*Location{}, Functions: map[uint64]*Function{}}
	var strs []string
	type vt struct{ t, u int64 }
	var sampleTypes []vt
	var period vt
	var defaultType int64
	type rawFn struct {
		id         uint64
		name, file int64
		start      int64
	}
	var fns []rawFn
	err := fields(data, func(num int, wire int, v uint64, b []byte) error {
		switch num {
		case 1: // sample_type
			var x vt
			if err := fields(b, func(n, w int, v uint64, _ []byte) error {
				switch n {
				case 1:
					x.t = int64(v)
				case 2:
					x.u = int64(v)
				}
				return nil
			}); err != nil {
				return err
			}
			sampleTypes = append(sampleTypes, x)
		case 2: // sample
			var s Sample
			if err := fields(b, func(n, w int, v uint64, bb []byte) error {
				switch n {
				case 1:
					if w == 2 {
						s.Locs = append(s.Locs, packed(bb)...)
					} else {
						s.Locs = append(s.Locs, v)
					}
				case 2:
					if w == 2 {
						for _, x := range packed(bb) {
							s.Values = append(s.Values, int64(x))
						}
					} else {
						s.Values = append(s.Values, int64(v))
					}
				}
				return nil
			}); err != nil {
				return err
			}
			p.Samples = append(p.Samples, s)
		case 4: // location
			l := &Location{}
			if err := fields(b, func(n, w int, v uint64, bb []byte) error {
				switch n {
				case 1:
					l.ID = v
				case 3:
					l.Address = v
				case 4:
					var ln Line
					if err := fields(bb, func(n, w int, v uint64, _ []byte) error {
						switch n {
						case 1:
							ln.Function = v
						case 2:
							ln.Line = int64(v)
						}
						return nil
					}); err != nil {
						return err
					}
					l.Lines = append(l.Lines, ln)
				}
				return nil
			}); err != nil {
				return err
			}
			p.Locations[l.ID] = l
		case 5: // function
			var f rawFn
			if err := fields(b, func(n, w int, v uint64, _ []byte) error {
				switch n {
				case 1:
					f.id = v
				case 2:
					f.name = int64(v)
				case 4:
					f.file = int64(v)
				case 5:
					f.start = int64(v)
				}
				return nil
			}); err != nil {
				return err
			}
			fns = append(fns, f)
		case 6:
			strs = append(strs, string(b))
		case 10:
			p.DurationNs = int64(v)
		case 11:
			if err := fields(b, func(n, w int, v uint64, _ []byte) error {
				switch n {
				case 1:
					period.t = int64(v)
				case 2:
					period.u = int64(v)
				}
				return nil
			}); err != nil {
				return err
			}
		case 12:
			p.Period = int64(v)
		case 14:
			defaultType = int64(v)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	str := func(i int64) string {
		if i >= 0 && int(i) < len(strs) {
			return strs[i]
		}
		return ""
	}
	for _, t := range sampleTypes {
		p.SampleTypes = append(p.SampleTypes, ValueType{str(t.t), str(t.u)})
	}
	p.PeriodType = ValueType{str(period.t), str(period.u)}
	p.DefaultType = str(defaultType)
	for _, f := range fns {
		p.Functions[f.id] = &Function{ID: f.id, Name: str(f.name), File: str(f.file), StartLin: f.start}
	}
	if len(p.SampleTypes) == 0 {
		return nil, errors.New("not a pprof profile (no sample types)")
	}
	return p, nil
}

// fields calls fn for each field of a protobuf message.
func fields(b []byte, fn func(num, wire int, v uint64, data []byte) error) error {
	for len(b) > 0 {
		key, n := uvarint(b)
		if n <= 0 {
			return errors.New("bad protobuf")
		}
		b = b[n:]
		num, wire := int(key>>3), int(key&7)
		switch wire {
		case 0:
			v, n := uvarint(b)
			if n <= 0 {
				return errors.New("bad varint")
			}
			b = b[n:]
			if err := fn(num, wire, v, nil); err != nil {
				return err
			}
		case 1:
			if len(b) < 8 {
				return errors.New("short fixed64")
			}
			b = b[8:]
		case 2:
			l, n := uvarint(b)
			if n <= 0 || uint64(len(b)-n) < l {
				return errors.New("bad length")
			}
			data := b[n : n+int(l)]
			b = b[n+int(l):]
			if err := fn(num, wire, 0, data); err != nil {
				return err
			}
		case 5:
			if len(b) < 4 {
				return errors.New("short fixed32")
			}
			b = b[4:]
		default:
			return fmt.Errorf("protobuf wire type %d", wire)
		}
	}
	return nil
}

func uvarint(b []byte) (uint64, int) {
	var x uint64
	var s uint
	for i, c := range b {
		if i == 10 {
			return 0, -1
		}
		if c < 0x80 {
			return x | uint64(c)<<s, i + 1
		}
		x |= uint64(c&0x7f) << s
		s += 7
	}
	return 0, 0
}

func packed(b []byte) []uint64 {
	var out []uint64
	for len(b) > 0 {
		v, n := uvarint(b)
		if n <= 0 {
			break
		}
		out = append(out, v)
		b = b[n:]
	}
	return out
}

// Index returns the sample value index of a type name (cpu, alloc_space,
// ...), the default type when name is empty, or the last type.
func (p *Profile) Index(name string) (int, error) {
	if name == "" {
		name = p.DefaultType
	}
	for i, t := range p.SampleTypes {
		if t.Type == name {
			return i, nil
		}
	}
	if name == p.DefaultType || name == "" {
		return len(p.SampleTypes) - 1, nil
	}
	var names []string
	for _, t := range p.SampleTypes {
		names = append(names, t.Type)
	}
	return 0, fmt.Errorf("no sample type %q (have %v)", name, names)
}

// Frame is one function in a stack, with its line.
type Frame struct {
	Func string
	File string
	Line int64
}

// Stack expands a sample into frames, innermost first (inlined frames
// included).
func (p *Profile) Stack(s Sample) []Frame {
	var out []Frame
	for _, id := range s.Locs {
		l := p.Locations[id]
		if l == nil {
			continue
		}
		if len(l.Lines) == 0 {
			out = append(out, Frame{Func: fmt.Sprintf("0x%x", l.Address)})
			continue
		}
		for _, ln := range l.Lines {
			f := p.Functions[ln.Function]
			if f == nil {
				continue
			}
			out = append(out, Frame{Func: f.Name, File: f.File, Line: ln.Line})
		}
	}
	return out
}

// Stat is a flat and cumulative total.
type Stat struct {
	Name string `json:"name"`
	File string `json:"file,omitempty"`
	Line int64  `json:"line,omitempty"`
	Flat int64  `json:"flat"`
	Cum  int64  `json:"cum"`
}

// ByFunction totals the values of type idx by function, largest flat
// first.
func (p *Profile) ByFunction(idx int) ([]Stat, int64) {
	return p.aggregate(idx, func(f Frame) (string, string, int64) { return f.Func, f.File, 0 })
}

// ByLine totals the values by source line.
func (p *Profile) ByLine(idx int) ([]Stat, int64) {
	return p.aggregate(idx, func(f Frame) (string, string, int64) { return f.Func, f.File, f.Line })
}

func (p *Profile) aggregate(idx int, key func(Frame) (string, string, int64)) ([]Stat, int64) {
	type k struct {
		n, f string
		l    int64
	}
	m := map[k]*Stat{}
	var total int64
	for _, s := range p.Samples {
		if idx >= len(s.Values) {
			continue
		}
		v := s.Values[idx]
		total += v
		seen := map[k]bool{}
		for i, fr := range p.Stack(s) {
			n, f, l := key(fr)
			kk := k{n, f, l}
			st := m[kk]
			if st == nil {
				st = &Stat{Name: n, File: f, Line: l}
				m[kk] = st
			}
			if i == 0 {
				st.Flat += v
			}
			if !seen[kk] {
				seen[kk] = true
				st.Cum += v
			}
		}
	}
	out := make([]Stat, 0, len(m))
	for _, s := range m {
		out = append(out, *s)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Flat != out[j].Flat {
			return out[i].Flat > out[j].Flat
		}
		if out[i].Cum != out[j].Cum {
			return out[i].Cum > out[j].Cum
		}
		return out[i].Name < out[j].Name
	})
	return out, total
}

// Node is a node of the call tree (callers above callees).
type Node struct {
	Name     string  `json:"name"`
	Value    int64   `json:"value"`
	Children []*Node `json:"children,omitempty"`
}

// Tree builds the top-down call tree of type idx.
func (p *Profile) Tree(idx int) *Node {
	root := &Node{Name: "total"}
	for _, s := range p.Samples {
		if idx >= len(s.Values) {
			continue
		}
		v := s.Values[idx]
		root.Value += v
		st := p.Stack(s)
		n := root
		for _, s := range slices.Backward(st) {
			var c *Node
			for _, x := range n.Children {
				if x.Name == s.Func {
					c = x
				}
			}
			if c == nil {
				c = &Node{Name: s.Func}
				n.Children = append(n.Children, c)
			}
			c.Value += v
			n = c
		}
	}
	var sortTree func(*Node)
	sortTree = func(n *Node) {
		sort.Slice(n.Children, func(i, j int) bool { return n.Children[i].Value > n.Children[j].Value })
		for _, c := range n.Children {
			sortTree(c)
		}
	}
	sortTree(root)
	return root
}
