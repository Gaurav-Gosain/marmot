struct Stack { items: Vec<i32> }

impl Stack {
    fn push(&mut self, v: i32) { self.items.push(v); }
}

fn main() {
    let mut s = Stack { items: vec![] };
    s.puhs(1);
}
