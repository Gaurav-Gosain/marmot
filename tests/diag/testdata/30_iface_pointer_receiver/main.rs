trait Counter { fn inc(&mut self); }

struct C { n: i32 }

impl Counter for &mut C {
    fn inc(&mut self) { self.n += 1; }
}

fn use_it<T: Counter>(mut x: T) { x.inc(); }

fn main() {
    let c = C { n: 0 };
    use_it(c);
}
