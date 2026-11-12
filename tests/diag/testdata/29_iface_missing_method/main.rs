trait Shape {
    fn area(&self) -> f64;
    fn perimeter(&self) -> f64;
}

struct Rect { w: f64, h: f64 }

impl Shape for Rect {
    fn area(&self) -> f64 { self.w * self.h }
}

fn show(s: &dyn Shape) { println!("{}", s.area()); }

fn main() {
    show(&Rect { w: 1.0, h: 2.0 });
}
