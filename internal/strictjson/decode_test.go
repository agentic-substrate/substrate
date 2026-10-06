package strictjson

import (
	"strings"
	"testing"
)

func TestDecodeRejectsAmbiguousAndUnsupportedJSON(t *testing.T) {
	var result struct {
		Name string `json:"name"`
	}
	for _, data := range []string{
		`{"name":"a","name":"b"}`, `{"name":"a","\u006eame":"b"}`,
		`{"name":"a","extra":true}`, `{"name":"a"} {}`, `{"name":"a"} garbage`,
		`{"Name":"a","name":"b"}`,
		`{"Name":"a"}`,
		`{"\u017fname":"a"}`,
		`{"name":"\ud800"}`, "{\"name\":\"\xff\"}",
		strings.Repeat("[", 34) + "0" + strings.Repeat("]", 34),
	} {
		if err := Decode([]byte(data), &result); err == nil {
			t.Fatalf("accepted %q", data)
		}
	}
	if err := Decode([]byte(`{"name":"valid \ud83d\ude00"}`), &result); err != nil {
		t.Fatal(err)
	}
}

func TestDecodeRejectsNestedDuplicateKeys(t *testing.T) {
	var result struct {
		Value struct {
			Name string `json:"name"`
		} `json:"value"`
	}
	if err := Decode([]byte(`{"value":{"name":"a","name":"b"}}`), &result); err == nil {
		t.Fatal("nested duplicate accepted")
	}
}

func TestDecodeRejectsUnicodeFieldAlias(t *testing.T) {
	var result struct {
		Sessions map[string]string `json:"sessions"`
	}
	if err := Decode([]byte(`{"\u017fessions":{}}`), &result); err == nil {
		t.Fatal("Unicode alias granted a known recovery field")
	}
}
