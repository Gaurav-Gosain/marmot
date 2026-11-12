fn zero<T: Default>() -> T { T::default() }

fn main() {
    let x = zero();
}
