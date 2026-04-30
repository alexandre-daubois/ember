package model

import "testing"

func TestNormalizeURI(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"root", "/", "/"},
		{"static path", "/api/v1/health", "/api/v1/health"},
		{"uuid lowercase", "/users/550e8400-e29b-41d4-a716-446655440000", "/users/:uuid"},
		{"uuid uppercase", "/users/550E8400-E29B-41D4-A716-446655440000", "/users/:uuid"},
		{"uuid mixed case", "/users/550e8400-E29B-41d4-A716-446655440000/orders", "/users/:uuid/orders"},
		{"numeric id", "/orders/12345", "/orders/:id"},
		{"long hex hash", "/objects/0123456789abcdef0123456789abcdef", "/objects/:hash"},
		{"sha-like 64", "/blobs/0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", "/blobs/:hash"},
		{"short hex stays", "/colors/abc123", "/colors/abc123"},
		{"slug stays", "/posts/hello-world-2026", "/posts/hello-world-2026"},
		{"chained dynamic segments", "/users/42/orders/3fa85f64-5717-4562-b3fc-2c963f66afa6/items/7", "/users/:id/orders/:uuid/items/:id"},
		{"trailing slash", "/users/42/", "/users/:id/"},
		{"query string dropped", "/search?q=42&id=550e8400-e29b-41d4-a716-446655440000", "/search"},
		{"fragment dropped", "/page#section-42", "/page"},
		{"no leading slash", "items/42", "items/:id"},
		{"double slash kept", "/a//b/42", "/a//b/:id"},
		{"numeric host segment", "/v2/api", "/v2/api"},
		{"alpha digit mix kept", "/teams/abc42def", "/teams/abc42def"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeURI(tt.in)
			if got != tt.want {
				t.Errorf("NormalizeURI(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
