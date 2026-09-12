package config

import (
	"bytes"
	"testing"
)

func TestAuditConfigNeverPersistsCredentials(t *testing.T) {
	raw := []byte(`{"meta":{"config_version":45},"database":{"dsn":"private-dsn"},"trading":{"eastmoney":{"token":"private-token"},"bridge":{"hmac":{"secret":"private-hmac"}}},"security":{"static_tokens":["private-auth"]}}`)
	audit := AuditJSON(raw)
	if bytes.Contains(audit, []byte("private-")) || !bytes.Contains(audit, []byte("config_version")) {
		t.Fatalf("unsafe audit JSON: %s", audit)
	}
	if !bytes.Contains(raw, []byte("private-token")) {
		t.Fatal("mutated runtime config")
	}
}
