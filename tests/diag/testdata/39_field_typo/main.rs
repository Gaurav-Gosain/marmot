struct Config { port: i32, host: String }

fn main() {
    let mut c = Config { port: 0, host: String::new() };
    c.prot = 80;
    println!("{}", c.host);
}
