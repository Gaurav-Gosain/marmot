package main

import (
	"fmt"
	"io"

	"github.com/pkg/errors"
)

type myErr struct{ code int }

func (e *myErr) Error() string { return fmt.Sprint("code ", e.code) }

func inner() error { return errors.New("inner failed") }

func mid() error { return errors.Wrap(inner(), "mid") }

func main() {
	err := errors.Wrapf(mid(), "outer %d", 42)
	fmt.Println(err)
	fmt.Printf("%v|%s|%q\n", err, err, err)
	fmt.Println(errors.Cause(err))
	fmt.Println(errors.Unwrap(err))
	fmt.Println(errors.Is(errors.Wrap(io.EOF, "x"), io.EOF), errors.Cause(errors.Wrap(io.EOF, "x")) == io.EOF)
	var me *myErr
	fmt.Println(errors.As(errors.Wrap(&myErr{7}, "ctx"), &me), me.code)
	fmt.Println(errors.Wrap(nil, "x") == nil, errors.WithMessage(nil, "x") == nil)
	e2 := errors.WithMessagef(io.EOF, "read %s", "file")
	fmt.Println(e2, errors.Cause(e2) == io.EOF)
	e3 := errors.WithStack(io.ErrUnexpectedEOF)
	fmt.Println(e3)
	st, ok := e3.(interface{ StackTrace() errors.StackTrace })
	fmt.Println(ok, len(st.StackTrace()) > 0)
	f := fmt.Sprintf("%+v", errors.New("with trace"))
	fmt.Println(len(f) > len("with trace"), f[:10])
}
