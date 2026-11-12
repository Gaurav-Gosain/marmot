trait Namer { fn name(&self) -> String; }

struct Dog;

impl Namer for Dog {
    fn name(&self) -> Vec<u8> { b"dog".to_vec() }
}

fn main() {
    let n: &dyn Namer = &Dog;
    println!("{}", n.name());
}
