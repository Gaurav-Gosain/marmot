use std::collections::HashMap;

fn main() {
    let m: HashMap<f64, String> = HashMap::new();
    let mut m2 = m;
    m2.insert(1.0, String::new());
}
