package codegen

import "testing"

// rt_wb_kind tells the runtime which barrier the program has (rt.h): 0
// none, 1 flag barrier, 2 cards. wasm has no cards: every mode is 1 there.
func TestWBKind(t *testing.T) {
	saveMode, saveOpt, saveFlag, saveStub := wbMode, optWB, cardFlag, cardStub
	defer func() { wbMode, optWB, cardFlag, cardStub = saveMode, saveOpt, saveFlag, saveStub }()
	cases := []struct {
		mode, arch string
		want       int
	}{
		{"", "arm64", 0},
		{"1", "arm64", 1},
		{"cards", "amd64", 2},
		{"cardflag", "arm64", 2},
		{"cardstub", "amd64", 2},
		{"cards", "wasm", 1},
	}
	for _, c := range cases {
		wbMode = c.mode
		optWB = c.mode == "1" || c.mode == "cards" || c.mode == "cardflag" || c.mode == "cardstub"
		cardFlag, cardStub = c.mode == "cardflag", c.mode == "cardstub"
		m := (&Program{opts: Options{GOARCH: c.arch}}).newModule(nil)
		if got := m.wbKind(); got != c.want {
			t.Errorf("MARMOT_WB=%q %s: rt_wb_kind %d, want %d", c.mode, c.arch, got, c.want)
		}
	}
}
