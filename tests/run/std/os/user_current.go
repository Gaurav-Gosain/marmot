package main

import (
	"fmt"
	"os"
	"os/user"
)

func main() {
	u, err := user.Current()
	fmt.Println(err, u.Uid != "", u.Gid != "", u.Username != "", u.HomeDir != "")
	u2, err := user.LookupId(u.Uid)
	fmt.Println(err, u2.Username == u.Username)
	_, err = user.Lookup("definitely-no-such-user-zz")
	_, isUnknown := err.(user.UnknownUserError)
	fmt.Println(err != nil, isUnknown)
	h, err := os.UserHomeDir()
	fmt.Println(h != "", err)
	c, err := os.UserCacheDir()
	fmt.Println(c != "", err)
	fmt.Println(os.Getuid() >= 0, os.Getgid() >= 0, os.Getppid() > 0)
	g, _ := u.GroupIds()
	fmt.Println(len(g) > 0)
}
