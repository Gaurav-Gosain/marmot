fn index<T: PartialEq>(xs: &[T], v: &T) -> i32 {
    for (i, x) in xs.iter().enumerate() {
        if x == v { return i as i32; }
    }
    -1
}

struct F;

fn main() {
    println!("{}", index(&[F], &F));
}
