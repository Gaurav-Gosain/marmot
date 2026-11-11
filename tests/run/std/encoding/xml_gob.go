package main

import (
	"bytes"
	"encoding/gob"
	"encoding/xml"
	"fmt"
)

type Addr struct {
	City string `xml:"city,attr"`
	Zip  string `xml:",chardata"`
}

type Person struct {
	XMLName xml.Name `xml:"person"`
	ID      int      `xml:"id,attr"`
	Name    string   `xml:"name"`
	Emails  []string `xml:"contact>email"`
	Addr    *Addr    `xml:"addr"`
	Note    string   `xml:",comment"`
	Skip    string   `xml:"-"`
	Opt     string   `xml:"opt,omitempty"`
}

type G struct {
	N   int
	S   string
	F   []float64
	M   map[string]int
	P   *G
	Any any
}

func init() { gob.Register(map[string]string{}) }

func main() {
	p := Person{ID: 7, Name: "Ann <&>", Emails: []string{"a@x", "b@y"}, Addr: &Addr{"Paris", "75001"}, Note: " hi "}
	b, err := xml.MarshalIndent(p, "", " ")
	fmt.Println(string(b), err)
	var back Person
	fmt.Println(xml.Unmarshal(b, &back), back.ID, back.Name, back.Emails, *back.Addr, back.XMLName.Local)
	for _, bad := range []string{"<a><b></a>", "<a", "", "<person id='x'/>"} {
		var q Person
		fmt.Printf("%q %v\n", bad, xml.Unmarshal([]byte(bad), &q))
	}
	d := xml.NewDecoder(bytes.NewReader([]byte(`<r a="1"><c>text</c><!-- k --><d/></r>`)))
	for {
		t, err := d.Token()
		if err != nil {
			break
		}
		switch v := t.(type) {
		case xml.StartElement:
			fmt.Print("start:", v.Name.Local, len(v.Attr), " ")
		case xml.EndElement:
			fmt.Print("end:", v.Name.Local, " ")
		case xml.CharData:
			fmt.Printf("chars:%q ", string(v))
		case xml.Comment:
			fmt.Printf("comment:%q ", string(v))
		}
	}
	fmt.Println()
	var buf bytes.Buffer
	g := G{3, "gob", []float64{1.5, 2.5}, map[string]int{"a": 1}, &G{N: 9}, map[string]string{"k": "v"}}
	fmt.Println(gob.NewEncoder(&buf).Encode(g), buf.Len() > 0)
	var out G
	fmt.Println(gob.NewDecoder(&buf).Decode(&out), out.N, out.S, out.F, out.M, out.P.N, out.Any)
	var wrong struct{ N string }
	buf.Reset()
	gob.NewEncoder(&buf).Encode(g)
	fmt.Println(gob.NewDecoder(&buf).Decode(&wrong) != nil)
}
