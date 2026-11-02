package live

import (
	"os"
	"testing"
)

// TestMain marks this process as a test environment. Production AI settings
// only come from the super-admin provider pool; the environment fallback in
// aiCandidates is intentionally limited to tests so this suite can point at
// local httptest servers without writing encrypted rows for every case.
func TestMain(m *testing.M) {
	_ = os.Setenv("APP_ENV", "test")
	os.Exit(m.Run())
}
