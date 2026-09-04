package tradingbridgeclient

import (
	"net/url"
	"testing"
)

func TestCanonicalStringAndSign(t *testing.T) {
	query := url.Values{"b": {"2"}, "a": {"z", "1"}}
	canonical := CanonicalString("post", "/v1/orders", query, []byte(`{"x":1}`), "123", "nonce")
	expected := "POST\n/v1/orders\na=1&a=z&b=2\n5041bf1f713df204784353e82f6a4a535931cb64f1f4b4a5aeaffcb720918b22\n123\nnonce"
	if canonical != expected {
		t.Fatalf("canonical mismatch:\n%s", canonical)
	}
	if got := Sign("secret", canonical); got != "36c3acd3f6166a4bd30d17ce997cd2a63d0ac9ed041d9f15bb9e26dc3e5ace03" {
		t.Fatalf("signature mismatch: %s", got)
	}
}
