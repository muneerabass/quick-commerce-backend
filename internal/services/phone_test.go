package services

import (
	"errors"
	"testing"
)

func TestNormalizePhone(t *testing.T) {
	got, err := NormalizePhone(" +91 98765-43210 ")
	if err != nil || got != "+919876543210" {
		t.Fatalf("got phone=%q err=%v", got, err)
	}
	if _, err := NormalizePhone("9876543210"); !errors.Is(err, ErrInvalidPhone) {
		t.Fatalf("expected E.164 validation error, got %v", err)
	}
}
