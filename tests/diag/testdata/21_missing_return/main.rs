fn sign(x: i32) -> i32 {
    if x < 0 {
        return -1;
    } else if x > 0 {
        return 1;
    }
}

fn main() { println!("{}", sign(1)); }
