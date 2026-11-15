package wasmfile

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestAppendPcln(t *testing.T) {
	// type section (1 byte of contents), code section (count 1, a body
	// of 2 bytes: size 2, locals 0, end).
	mod := append(append([]byte(nil), magic...), 1, 1, 0, 10, 4, 1, 2, 0, 0x0b)
	out, err := AppendPcln(mod, []byte("TBL"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(out, mod) {
		t.Fatal("the module before the section changed")
	}
	ss, err := Parse(out)
	if err != nil {
		t.Fatal(err)
	}
	last := ss[len(ss)-1]
	if last.Name() != ".marmot_pcln" {
		t.Fatalf("last section %q", last.Name())
	}
	body := last.Data[1+len(".marmot_pcln"):]
	if string(body[:4]) != "MPCW" || string(body[8:]) != "TBL" {
		t.Fatalf("section body %q", body)
	}
	// magic 8, type section 3 bytes, code id and size: contents at 13.
	if got := binary.LittleEndian.Uint32(body[4:8]); got != 13 {
		t.Errorf("code payload start %d, want 13", got)
	}
}
