package yamlx

import "testing"

func TestDecode(t *testing.T) {
	var v struct {
		A int    `json:"a"`
		B string `json:"b"`
	}
	if _, err := Decode([]byte("a: 1\nb: x\n"), &v); err != nil || v.A != 1 || v.B != "x" {
		t.Fatalf("got %+v %v", v, err)
	}
	if _, err := Decode([]byte("a: 1\nc: 2\n"), &v); err == nil {
		t.Fatal("unknown field accepted")
	}
	if _, err := Decode([]byte("a: [1"), &v); err == nil {
		t.Fatal("bad yaml accepted")
	}
	if _, err := ToJSON([]byte(": :")); err == nil {
		t.Fatal("bad yaml accepted")
	}
}
