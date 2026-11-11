package main

import (
	"fmt"
	"sort"

	"github.com/Masterminds/semver/v3"
)

func main() {
	vs := []string{"1.2.3", "v1.2.3-beta.1", "1.0.0-alpha", "1.0.0-alpha.1", "1.0.0-rc.1+build5", "2.0.0", "0.9.10", "1.10.0", "1.2.3+meta", "1.0.0-alpha.beta"}
	var list semver.Collection
	for _, s := range vs {
		v, err := semver.NewVersion(s)
		if err != nil {
			fmt.Println("bad", s, err)
			continue
		}
		list = append(list, v)
	}
	sort.Sort(list)
	for _, v := range list {
		fmt.Println(v, v.Major(), v.Minor(), v.Patch(), v.Prerelease(), v.Metadata(), v.Original())
	}
	v := semver.MustParse("1.2.3-pre")
	fmt.Println(v.IncPatch(), v.IncMinor(), v.IncMajor())
	for _, c := range []string{">=1.2.0, <2.0.0", "~1.2", "^1.2.3", "1.x", "!=1.2.3", ">1.0.0-0", "1.2.3 || 2.0.0", "=1.2.3", "*"} {
		cons, err := semver.NewConstraint(c)
		if err != nil {
			fmt.Println(c, err)
			continue
		}
		var ok []string
		for _, s := range []string{"1.2.2", "1.2.3", "1.2.9", "1.3.0", "2.0.0", "1.0.1-rc.1"} {
			if cons.Check(semver.MustParse(s)) {
				ok = append(ok, s)
			}
		}
		fmt.Println(c, ok)
	}
	for _, bad := range []string{"", "1", "1.2.3.4", "a.b.c", "1.2.3-", "01.2.3"} {
		_, err := semver.NewVersion(bad)
		_, errS := semver.StrictNewVersion(bad)
		fmt.Printf("%q %v | %v\n", bad, err, errS)
	}
	fmt.Println(semver.MustParse("1.2.3").Compare(semver.MustParse("1.2.4")), semver.MustParse("1.0.0").Equal(semver.MustParse("1.0.0+x")), semver.MustParse("1.2.3").LessThan(semver.MustParse("1.2.3-a")))
}
