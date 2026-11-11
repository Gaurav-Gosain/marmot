package pb

type I interface{ m() string }

type B struct{}

func (B) m() string { return "pb.T.m" }

func Call(i I) string { return i.m() }

func CallAny(v any) string {
	if i, ok := v.(I); ok {
		return i.m()
	}
	return "not I"
}
