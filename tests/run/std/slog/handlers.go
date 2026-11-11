package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
)

func noTime(groups []string, a slog.Attr) slog.Attr {
	if a.Key == slog.TimeKey && len(groups) == 0 {
		return slog.Attr{}
	}
	return a
}

func main() {
	var buf bytes.Buffer
	j := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{ReplaceAttr: noTime, Level: slog.LevelDebug}))
	j.Debug("debug msg", "k", 1)
	j.Info("hi", "n", 3, "f", 1.5, "b", true, "err", errors.New("boom"), slog.Group("g", "k", "v", slog.Int("i", 2)))
	j.With("req", "r1").WithGroup("grp").Warn("w", "a", []int{1, 2}, "m", map[string]int{"x": 1})
	j.Error("e", "s", "quote\"d\n", "nil", nil)
	fmt.Print(buf.String())
	buf.Reset()
	t := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{ReplaceAttr: noTime, AddSource: false}))
	t.Info("text msg", "k", "v with space", "n", 3, "e", errors.New("x y"), "q", "a=b")
	t.Debug("hidden")
	t.With("a", 1).WithGroup("g").Info("grouped", "b", 2)
	fmt.Print(buf.String())
	buf.Reset()
	lv := new(slog.LevelVar)
	lv.Set(slog.LevelWarn)
	l3 := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: lv, ReplaceAttr: noTime}))
	l3.Info("dropped")
	l3.Warn("kept")
	lv.Set(slog.LevelDebug - 4)
	l3.Log(context.Background(), slog.LevelDebug-2, "custom level")
	fmt.Print(buf.String())
	fmt.Println(slog.LevelInfo, slog.LevelError+1, slog.Level(-2), l3.Enabled(context.Background(), slog.LevelDebug-8))
	var lvl slog.Level
	fmt.Println(lvl.UnmarshalText([]byte("WARN+2")), lvl)
	a := slog.Any("k", []string{"x"})
	fmt.Println(a.Key, a.Value.Kind(), slog.String("s", "v"), slog.Duration("d", 1500000000))
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{ReplaceAttr: noTime})))
	slog.Info("default logger", "x", strings.Repeat("y", 3))
}
