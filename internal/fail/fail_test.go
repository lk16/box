package fail_test

import (
	"errors"
	"testing"

	"github.com/lk16/box/internal/fail"
)

func TestARefusalReadsAsTheOneLineItHolds(t *testing.T) {
	err := fail.Errorf("kit is not set for %s", "demo")
	if err.Error() != "kit is not set for demo" {
		t.Fatalf("the refusal reads %q", err)
	}
}

func TestARefusalIsTellableFromAnyOtherError(t *testing.T) {
	var refusal *fail.Error
	if !errors.As(fail.Errorf("no"), &refusal) {
		t.Fatal("a refusal was not recognised as one")
	}
	if errors.As(errors.New("no"), &refusal) {
		t.Fatal("a plain error was taken for a refusal")
	}
}
