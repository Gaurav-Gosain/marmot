struct User {
    name: String,
    age: i32,
}

fn main() {
    let u = User { name: String::from("a"), age: "ten" };
    println!("{}", u.name);
}
