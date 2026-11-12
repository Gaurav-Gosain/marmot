use std::ops::Add;

fn sum<T: Add<Output = T> + Default + Copy>(xs: &[T]) -> T {
    let mut t = T::default();
    for x in xs { t = t + *x; }
    t
}

fn main() {
    println!("{}", sum(&["a"]));
}
