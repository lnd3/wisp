package registry

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	tokA  = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	tokA2 = "a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2a2"
	tokB  = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func TestLookup(t *testing.T) {
	r, err := Parse([]byte(`{"products": [
		{"key": "cindernote", "token_sha256": ["` + HashToken(tokA) + `", "` + HashToken(tokA2) + `"]},
		{"key": "persona", "token_sha256": ["` + HashToken(tokB) + `"]}
	]}`))
	if err != nil {
		t.Fatal(err)
	}
	for tok, want := range map[string]string{tokA: "cindernote", tokA2: "cindernote", tokB: "persona"} {
		if got, ok := r.Lookup(tok); !ok || got != want {
			t.Errorf("Lookup(%.6s…) = %q, %v; want %q", tok, got, ok, want)
		}
	}
	for _, tok := range []string{"", "nope", HashToken(tokA)} { // the hash itself is not a token
		if _, ok := r.Lookup(tok); ok {
			t.Errorf("Lookup(%q) succeeded", tok)
		}
	}
	if got := strings.Join(r.Products(), ","); got != "cindernote,persona" {
		t.Errorf("Products() = %s", got)
	}
	var nilReg *Registry
	if _, ok := nilReg.Lookup(tokA); ok {
		t.Error("nil registry must reject everything")
	}
}

func TestParseRejects(t *testing.T) {
	h := HashToken(tokA)
	for name, js := range map[string]string{
		"bad json":        `{`,
		"bad key":         `{"products":[{"key":"Cinder Note","token_sha256":["` + h + `"]}]}`,
		"path key":        `{"products":[{"key":"../x","token_sha256":["` + h + `"]}]}`,
		"duplicate key":   `{"products":[{"key":"a","token_sha256":["` + h + `"]},{"key":"a","token_sha256":["` + HashToken(tokB) + `"]}]}`,
		"no hashes":       `{"products":[{"key":"a","token_sha256":[]}]}`,
		"three hashes":    `{"products":[{"key":"a","token_sha256":["` + h + `","` + HashToken(tokA2) + `","` + HashToken(tokB) + `"]}]}`,
		"short hash":      `{"products":[{"key":"a","token_sha256":["abcd"]}]}`,
		"plaintext token": `{"products":[{"key":"a","token_sha256":["` + tokA + `x"]}]}`,
		"shared hash":     `{"products":[{"key":"a","token_sha256":["` + h + `"]},{"key":"b","token_sha256":["` + h + `"]}]}`,
	} {
		if _, err := Parse([]byte(js)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestLoad(t *testing.T) {
	p := filepath.Join(t.TempDir(), "products.json")
	os.WriteFile(p, []byte(`{"products":[{"key":"a","token_sha256":["`+HashToken(tokA)+`"]}]}`), 0o600)
	if r, err := Load(p); err != nil || len(r.Products()) != 1 {
		t.Fatalf("Load = %v, %v", r, err)
	}
	if _, err := Load(p + ".missing"); err == nil {
		t.Error("missing file must fail")
	}
}
