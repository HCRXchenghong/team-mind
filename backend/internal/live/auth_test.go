package live

import "testing"

func TestValidEmail(t *testing.T) {
	tests := []struct {
		value string
		valid bool
	}{
		{"person@example.com", true},
		{"person+team@example.co.uk", true},
		{" Person@example.com", false},
		{"person name@example.com", false},
		{"Name <person@example.com>", false},
		{"person@@example.com", false},
		{"person@", false},
		{"@example.com", false},
		{"person.example.com", false},
	}
	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			if got := validEmail(tt.value); got != tt.valid {
				t.Fatalf("validEmail(%q) = %v, want %v", tt.value, got, tt.valid)
			}
		})
	}
}
