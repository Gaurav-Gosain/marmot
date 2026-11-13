// Checks raylib calls that pass and return structs by value, without a window.
package main

import (
	"fmt"

	"marmotworld/bind_raylib/rl"
)

func main() {
	c := rl.ColorFromHSV(120, 1, 1)
	fmt.Println(c, rl.Fade(rl.Red, 0.5), rl.ColorToInt(rl.Red), rl.GetColor(0x336699ff))
	fmt.Println(rl.ColorBrightness(rl.Blue, 0.5), rl.ColorLerp(rl.Black, rl.White, 0.25))
	a := rl.Rectangle{X: 0, Y: 0, Width: 10, Height: 10}
	b := rl.Rectangle{X: 5, Y: 5, Width: 10, Height: 10}
	fmt.Println(rl.CheckCollisionRecs(a, b), rl.GetCollisionRec(a, b))
	fmt.Println(rl.CheckCollisionCircles(rl.Vector2{X: 0, Y: 0}, 1, rl.Vector2{X: 1.5, Y: 0}, 1),
		rl.CheckCollisionPointRec(rl.Vector2{X: 3, Y: 3}, a), rl.CheckCollisionPointCircle(rl.Vector2{X: 9, Y: 9}, rl.Vector2{}, 2))
	fmt.Println(rl.ColorNormalize(rl.Gold), rl.ColorToHSV(rl.Lime))
}
