package config

import "testing"

func TestPublicBaseURLOriginValidation(t *testing.T) {
	for _, tc := range []struct{ input, expected string }{
		{"", ""}, {" https://Admin.Example.com/ ", "https://admin.example.com"},
		{"https://admin.example.com:8443", "https://admin.example.com:8443"},
	} {
		actual, err := NormalizePublicBaseURL(tc.input)
		if err != nil || actual != tc.expected {
			t.Fatalf("public origin normalization failed: %v", err)
		}
	}
	for _, input := range []string{
		"http://admin.example.com", "https://u:secret@example.com", "https://example.com/path", "https://example.com?token=secret",
		"https://example.com#x", "https://127.0.0.1", "https://[::1]", "https://localhost", "https://-bad.example.com", "https://example..com", "https://example.com:0", "https://example.com:65536", "https://example.com\\evil", "https://example.com\n",
	} {
		// Whitespace around a valid origin is accepted; embedded controls are not.
		if input == "https://example.com\n" {
			input = "https://exam\nple.com"
		}
		if _, err := NormalizePublicBaseURL(input); err == nil {
			t.Fatalf("invalid public origin accepted")
		}
	}
}
