// Package registry holds wisp's product credentials: each product's
// public key and the SHA-256 hashes of its ingest tokens (wisp's
// plan/designs/D002, "Product credentials"). wisp never stores a token
// itself — only its hash — so the registry file leaking doesn't let
// anyone ingest.
//
// The file lives only on the server (never in git, excluded from
// deploy.sh's rsync) and looks like:
//
//	{
//	  "products": [
//	    {"key": "cindernote", "token_sha256": ["<64 hex chars>", "<a second, during rotation>"]}
//	  ]
//	}
package registry

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
)

// MaxTokensPerProduct allows one rotation at a time: the new token is
// added, the product switches to it, the old one is removed.
const MaxTokensPerProduct = 2

// keyPattern keeps product keys safe as path segments (staging files are
// stored per product) and readable in the dashboard.
var keyPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

// ValidKey reports whether k is an acceptable product key.
func ValidKey(k string) bool { return keyPattern.MatchString(k) }

// Registry maps token hashes to product keys. It is immutable once
// loaded; reload by loading a new one.
type Registry struct {
	byHash map[[sha256.Size]byte]string
	keys   []string
}

type file struct {
	Products []struct {
		Key         string   `json:"key"`
		TokenSHA256 []string `json:"token_sha256"`
	} `json:"products"`
}

// Load reads and validates a registry file.
func Load(path string) (*Registry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("registry: %w", err)
	}
	return Parse(data)
}

// Parse validates registry JSON: well-formed keys, unique keys, 1–2
// hex-encoded SHA-256 hashes per product, and no hash shared between
// products (a token must identify exactly one product).
func Parse(data []byte) (*Registry, error) {
	var f file
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("registry: invalid JSON: %w", err)
	}
	r := &Registry{byHash: make(map[[sha256.Size]byte]string)}
	seen := make(map[string]bool)
	for i, p := range f.Products {
		if !ValidKey(p.Key) {
			return nil, fmt.Errorf("registry: product %d: invalid key %q (want %s)", i, p.Key, keyPattern)
		}
		if seen[p.Key] {
			return nil, fmt.Errorf("registry: duplicate product key %q", p.Key)
		}
		seen[p.Key] = true
		if n := len(p.TokenSHA256); n < 1 || n > MaxTokensPerProduct {
			return nil, fmt.Errorf("registry: product %q: want 1–%d token hashes, got %d", p.Key, MaxTokensPerProduct, n)
		}
		for _, hx := range p.TokenSHA256 {
			b, err := hex.DecodeString(hx)
			if err != nil || len(b) != sha256.Size {
				return nil, fmt.Errorf("registry: product %q: token hash must be 64 hex chars", p.Key)
			}
			var h [sha256.Size]byte
			copy(h[:], b)
			if other, dup := r.byHash[h]; dup {
				return nil, fmt.Errorf("registry: products %q and %q share a token hash", other, p.Key)
			}
			r.byHash[h] = p.Key
		}
		r.keys = append(r.keys, p.Key)
	}
	return r, nil
}

// Lookup returns the product a token belongs to. The token is hashed
// first and the hash looked up; since tokens are 256-bit random values,
// lookup timing reveals nothing usable about any real token.
func (r *Registry) Lookup(token string) (product string, ok bool) {
	if r == nil || token == "" {
		return "", false
	}
	product, ok = r.byHash[sha256.Sum256([]byte(token))]
	return product, ok
}

// Products returns the registered product keys in file order.
func (r *Registry) Products() []string { return append([]string(nil), r.keys...) }

// HashToken returns the hex SHA-256 of a token, the form stored in the
// registry file.
func HashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}
