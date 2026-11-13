// A small raylib program against bindings made by marmot bind: a window,
// a ball that bounces (arrow keys push it), and text.
package main

import (
	"fmt"

	"marmotworld/bind_raylib/rl"
)

func main() {
	const w, h = 800, 450
	rl.InitWindow(w, h, "marmot bind: raylib")
	defer rl.CloseWindow()
	rl.SetTargetFPS(60)

	pos := rl.Vector2{X: w / 2, Y: h / 2}
	vel := rl.Vector2{X: 220, Y: 160}
	const radius = 30
	for !rl.WindowShouldClose() {
		dt := rl.GetFrameTime()
		if rl.IsKeyDown(int(rl.KeyRight)) {
			vel.X += 600 * dt
		}
		if rl.IsKeyDown(int(rl.KeyLeft)) {
			vel.X -= 600 * dt
		}
		if rl.IsKeyDown(int(rl.KeyUp)) {
			vel.Y -= 600 * dt
		}
		if rl.IsKeyDown(int(rl.KeyDown)) {
			vel.Y += 600 * dt
		}
		pos.X += vel.X * dt
		pos.Y += vel.Y * dt
		if pos.X < radius || pos.X > w-radius {
			vel.X = -vel.X
		}
		if pos.Y < radius || pos.Y > h-radius {
			vel.Y = -vel.Y
		}
		hue := float32(rl.GetTime()*60) - float32(int(rl.GetTime()*60/360))*360

		rl.BeginDrawing()
		rl.ClearBackground(rl.RayWhite)
		rl.DrawRectangleRec(rl.Rectangle{X: 20, Y: 20, Width: w - 40, Height: h - 40}, rl.Fade(rl.LightGray, 0.3))
		rl.DrawCircleV(pos, radius, rl.ColorFromHSV(hue, 0.8, 0.9))
		rl.DrawText("Hello from marmot bind", 40, 40, 30, rl.DarkGray)
		rl.DrawText(fmt.Sprintf("ball at %.0f, %.0f (arrow keys push it)", pos.X, pos.Y), 40, 80, 20, rl.Maroon)
		rl.DrawFPS(w-100, 30)
		rl.EndDrawing()
	}
}
