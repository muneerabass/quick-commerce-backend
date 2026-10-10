package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var ErrTwoFactorSend = errors.New("2Factor OTP delivery failed")

type TwoFactorSender struct {
	baseURL    string
	apiKey     string
	template   string
	httpClient *http.Client
}

func NewTwoFactorSender(baseURL, apiKey, template string) *TwoFactorSender {
	return &TwoFactorSender{
		baseURL: strings.TrimRight(baseURL, "/"), apiKey: apiKey, template: template,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

func (s *TwoFactorSender) Send(ctx context.Context, phone, otp string) error {
	endpoint := fmt.Sprintf("%s/API/V1/%s/SMS/%s/%s/%s",
		s.baseURL, url.PathEscape(s.apiKey), url.PathEscape(phone), url.PathEscape(otp), url.PathEscape(s.template))
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, nil)
	if err != nil {
		return ErrTwoFactorSend
	}
	response, err := s.httpClient.Do(request)
	if err != nil {
		return ErrTwoFactorSend
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 64<<10))
	if err != nil || response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return ErrTwoFactorSend
	}
	if !twoFactorResponseSucceeded(body) {
		return ErrTwoFactorSend
	}
	return nil
}

func twoFactorResponseSucceeded(body []byte) bool {
	if len(strings.TrimSpace(string(body))) == 0 {
		return true
	}
	var response struct {
		Status  string `json:"Status"`
		Status2 string `json:"status"`
		Type    string `json:"type"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return false
	}
	status := strings.ToLower(strings.TrimSpace(response.Status + " " + response.Status2 + " " + response.Type + " " + response.Message))
	if strings.Contains(status, "error") || strings.Contains(status, "fail") || strings.Contains(status, "invalid") {
		return false
	}
	if response.Status == "" && response.Status2 == "" && response.Type == "" && response.Message == "" {
		return false
	}
	return strings.Contains(status, "success") || strings.Contains(status, "sent") || strings.Contains(status, "ok")
}
