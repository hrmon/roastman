package jsonorder

import (
	"bytes"
	"testing"
)

func TestParsePreservesOrderAndFormat(t *testing.T) {
	inputs := []string{
		"{\n  \"b\": 1,\n  \"a\": [\n    1,\n    2\n  ]\n}",
		"{\"b\":1,\"a\":[1,2]}",
		"{\r\n\t\"b\": 1,\r\n\t\"a\": {\"x\": true}\r\n}\r\n",
		"{\n  \"a\": {\"x\": 1, \"y\": [1, 2]},\n  \"b\": []\n}",
	}
	for _, input := range inputs {
		v, err := Parse([]byte(input))
		if err != nil {
			t.Fatalf("Parse(%q): %v", input, err)
		}
		got := v.MarshalFormatted(DetectFormat([]byte(input)))
		if !bytes.Equal(got, []byte(input)) {
			t.Errorf("round trip mismatch\ninput: %q\ngot:   %q", input, got)
		}
	}
}

func TestApplyKeyOrders(t *testing.T) {
	v, err := Parse([]byte(`{"a":1,"b":2}`))
	if err != nil {
		t.Fatal(err)
	}
	ApplyKeyOrders(v, map[string][]string{"": {"b", "a"}})
	if got := string(v.MarshalCompact()); got != `{"b":2,"a":1}` {
		t.Fatalf("got %s", got)
	}
}

func TestDetectFormatMixedInline(t *testing.T) {
	input := "{\n  \"a\": {\"x\": 1, \"y\": 2},\n  \"b\": [1, 2]\n}"
	f := DetectFormat([]byte(input))
	if len(f.InlinePaths) != 2 {
		t.Fatalf("inline paths = %v", f.InlinePaths)
	}
	if !f.Pretty || !f.InlineCommaSpace || !f.InlineColonSpace {
		t.Fatalf("format = %#v", f)
	}
}
