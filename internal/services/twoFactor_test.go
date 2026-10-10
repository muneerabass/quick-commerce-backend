package services

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTwoFactorSender(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/API/V1/test-key/SMS/+919999999999/1234/OTP1" {
			t.Errorf("path = %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Status":"Success"}`))
	}))
	defer server.Close()

	sender := NewTwoFactorSender(server.URL, "test-key", "OTP1")
	sender.httpClient = server.Client()
	if err := sender.Send(context.Background(), "+919999999999", "1234"); err != nil {
		t.Fatal(err)
	}
}

func TestTwoFactorSenderRejectsProviderFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Status":"Error"}`))
	}))
	defer server.Close()

	sender := NewTwoFactorSender(server.URL, "test-key", "OTP1")
	sender.httpClient = server.Client()
	if err := sender.Send(context.Background(), "+919999999999", "1234"); err != ErrTwoFactorSend {
		t.Fatalf("expected provider failure, got %v", err)
	}
}
