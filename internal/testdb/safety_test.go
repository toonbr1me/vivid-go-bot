package testdb

import "testing"

func TestNeverUsesApplicationDatabase(t *testing.T) {
	t.Setenv("TEST_DATABASE_URL", "")
	t.Setenv("DATABASE_URL", "postgres://user:secret@prod.example/production")
	if URL() != "" {
		t.Fatal("application database must never be used by tests")
	}
	t.Setenv("TEST_DATABASE_URL", " postgres://localhost/vivid_test ")
	if URL() != "postgres://localhost/vivid_test" {
		t.Fatal("explicit test database was not selected")
	}
}

func TestRejectsNonTestDatabaseBeforeAnyConnection(t *testing.T) {
	for _, raw := range []string{"postgres://localhost/production", "postgres://localhost/vivid", "postgres://localhost/postgres", "https://localhost/vivid_test", "postgres://localhost/test/nested", "postgres://localhost/vivid_test#fragment"} {
		t.Run(raw, func(t *testing.T) {
			if validateTestURL(raw) == nil {
				t.Fatal("unsafe test database accepted")
			}
		})
	}
	for _, raw := range []string{"postgres://localhost/vivid_test", "postgresql://localhost/test_vivid", "postgres://localhost/vivid_test_local"} {
		if err := validateTestURL(raw); err != nil {
			t.Fatal(err)
		}
	}
}
