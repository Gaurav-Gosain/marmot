package main

import "os"

func main() {
	os.Stdout.WriteString("to stdout\n")
	os.Stderr.WriteString("to stderr\n")
	println(len(os.Args) >= 1)
	os.Setenv("MARMOT_TEST_VAR", "xyz")
	println(os.Getenv("MARMOT_TEST_VAR"), os.Getenv("MARMOT_UNSET_VAR_Q") == "")
	v, ok := os.LookupEnv("MARMOT_TEST_VAR")
	println(v, ok)
	f, err := os.CreateTemp("", "marmot")
	if err != nil {
		println("tmp fail")
		return
	}
	name := f.Name()
	f.WriteString("file content")
	f.Close()
	data, err := os.ReadFile(name)
	println(string(data), err == nil)
	st, _ := os.Stat(name)
	println(st.Size(), st.IsDir())
	os.Remove(name)
	_, err = os.Stat(name)
	println(os.IsNotExist(err))
	_, err = os.Open("/definitely/not/here")
	println(err != nil)
}
