package keyring

import (
	"fmt"
	"testing"

	kr "github.com/zalando/go-keyring"
)

func TestKeyringOperations(t *testing.T) {
	kr.MockInit()

	t.Run("store and get", func(t *testing.T) {
		err := Store("l4_test_abc123")
		if err != nil {
			t.Fatalf("Store() error: %v", err)
		}

		got, err := Get()
		if err != nil {
			t.Fatalf("Get() error: %v", err)
		}
		if got != "l4_test_abc123" {
			t.Errorf("Get() = %q, want %q", got, "l4_test_abc123")
		}
	})

	t.Run("delete", func(t *testing.T) {
		Store("l4_test_abc123")

		err := Delete()
		if err != nil {
			t.Fatalf("Delete() error: %v", err)
		}

		_, err = Get()
		if err == nil {
			t.Error("expected error after delete")
		}
	})

	t.Run("get not found", func(t *testing.T) {
		kr.MockInit()

		_, err := Get()
		if err == nil {
			t.Error("expected error for missing key")
		}
	})
}

func TestStoreFuncOverride(t *testing.T) {
	orig := StoreFunc
	defer func() { StoreFunc = orig }()

	StoreFunc = func(_ string) error {
		return fmt.Errorf("mock store error")
	}

	err := Store("test")
	if err == nil {
		t.Error("expected error from overridden StoreFunc")
	}
}

func TestDefaultStore(t *testing.T) {
	kr.MockInit()
	err := defaultStore("l4_test_default")
	if err != nil {
		t.Fatalf("defaultStore error: %v", err)
	}
	got, _ := Get()
	if got != "l4_test_default" {
		t.Errorf("Get() = %q, want %q", got, "l4_test_default")
	}
}
