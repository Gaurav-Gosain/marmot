package main

type S struct{ n int }
type Stringer interface{ String() string }

func (s S) String() string { return "S" }

func kind(v interface{}) string {
	switch x := v.(type) {
	case nil:
		return "nil"
	case int:
		return "int"
	case int8, int16:
		return "small"
	case string:
		if x == "" {
			return "empty"
		}
		return "string"
	case []int:
		return "slice"
	case map[string]int:
		return "map"
	case func():
		return "func"
	case *S:
		return "ptr S"
	case Stringer:
		return "stringer"
	case error:
		return "error"
	case bool:
		return "bool"
	case float32, float64:
		return "float"
	default:
		return "other"
	}
}

func main() {
	vals := []interface{}{nil, 1, int8(1), int16(1), "", "a", []int{}, map[string]int{}, func() {}, &S{}, S{}, true, 1.5, float32(1), 'x', uint(1), [1]int{}}
	for _, v := range vals {
		println(kind(v))
	}
	var i interface{} = 7
	switch i.(type) {
	case string:
		println("s")
	case int:
		println("i")
	}
	switch v := i.(type) {
	case int, string:
		_ = v
		println("multi")
	}
}
