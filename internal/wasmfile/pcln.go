package wasmfile

import (
	"encoding/binary"
	"errors"
)

// SecCode is the code section's id.
const SecCode = 10

// CodePayloadStart returns the module byte offset of the code section's
// payload: past its id, its size and its function count, where the first
// function body starts. DWARF and the pc table give code addresses
// relative to it.
func CodePayloadStart(b []byte) (uint32, error) {
	if len(b) < len(magic) {
		return 0, errors.New("not a wasm module")
	}
	off := len(magic)
	ss, err := Parse(b)
	if err != nil {
		return 0, err
	}
	for _, s := range ss {
		_, k, err := uleb(b[off+1:])
		if err != nil {
			return 0, err
		}
		if s.ID == SecCode {
			// DWARF code addresses count from the start of the section's
			// contents (the function count included), as LLVM writes them.
			return uint32(off + 1 + k), nil
		}
		off += 1 + k + len(s.Data)
	}
	return 0, errors.New("no code section")
}

// AppendPcln appends the pc table as the custom section ".marmot_pcln":
// "MPCW", the code payload start (u32 little endian), then the table. It
// goes last, so no code offset moves.
func AppendPcln(b, table []byte) ([]byte, error) {
	start, err := CodePayloadStart(b)
	if err != nil {
		return nil, err
	}
	body := appendName(nil, ".marmot_pcln")
	body = append(body, 'M', 'P', 'C', 'W')
	body = binary.LittleEndian.AppendUint32(body, start)
	body = append(body, table...)
	out := append([]byte(nil), b...)
	out = append(out, SecCustom)
	out = appendUleb(out, uint64(len(body)))
	return append(out, body...), nil
}
