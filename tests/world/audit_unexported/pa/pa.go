package pa

type I interface{ m() string }

type A struct{}

func (A) m() string { return "pa.T.m" }

func Call(i I) string { return i.m() }

type K interface{ Name() string }

func Names(ks []K) string {
	s := ""
	for _, k := range ks {
		s += k.Name() + ","
	}
	return s
}

func CallAny(v any) string {
	if i, ok := v.(I); ok {
		return i.m()
	}
	return "not I"
}
