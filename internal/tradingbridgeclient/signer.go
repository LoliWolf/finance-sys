package tradingbridgeclient

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"sort"
	"strings"
)

func CanonicalString(method, path string, query url.Values, body []byte, timestampMS, nonce string) string {
	hash := sha256.Sum256(body)
	return strings.Join([]string{
		strings.ToUpper(method),
		path,
		normalizeQuery(query),
		hex.EncodeToString(hash[:]),
		timestampMS,
		nonce,
	}, "\n")
}

func Sign(secret, canonical string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(canonical))
	return hex.EncodeToString(mac.Sum(nil))
}

func normalizeQuery(values url.Values) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0)
	for _, key := range keys {
		items := append([]string(nil), values[key]...)
		sort.Strings(items)
		for _, value := range items {
			parts = append(parts, url.QueryEscape(key)+"="+url.QueryEscape(value))
		}
	}
	return strings.Join(parts, "&")
}
