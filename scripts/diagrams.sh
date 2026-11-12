#!/bin/sh
# Render docs/diagrams/*.mmd to light and dark SVG with sketch
# (go install tuios.dev/sketch/cmd/sketch@latest; tuios main at b063d337b or later). Chart bars use the theme's
# accent colour instead of its first series colour, which is red.
set -e
cd "$(dirname "$0")/.."
SKETCH=${SKETCH:-sketch}
LIGHT=${LIGHT:-catppuccin-latte}
DARK=${DARK:-catppuccin-mocha}
for f in docs/diagrams/*.mmd; do
	n=$(basename "$f" .mmd)
	for mode in light dark; do
		theme=$LIGHT; [ $mode = dark ] && theme=$DARK
		out=docs/img/$n-$mode.svg
		"$SKETCH" "$f" -theme "$theme" -o "$out"
		accent=$(sed -n 's/.*--pele-accent:\(#[0-9a-fA-F]*\).*/\1/p' "$out" | head -1)
		[ -n "$accent" ] && sed -i.bak "s/--pele-series-1:#[0-9a-fA-F]*/--pele-series-1:$accent/" "$out" && rm -f "$out.bak"
	done
done
