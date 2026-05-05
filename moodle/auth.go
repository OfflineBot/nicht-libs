package moodle

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
)

type tokenResponse struct {
	Token        string `json:"token"`
	PrivateToken string `json:"privatetoken"`
	Error        string `json:"error"`
	ErrorCode    string `json:"errorcode"`
}

type TokenPair struct {
	Token        string
	PrivateToken string
}

func GetToken(baseURL, username, password string) (*TokenPair, error) {
	slog.Debug("moodle: requesting token", "url", baseURL, "user", username)

	resp, err := http.PostForm(baseURL+"/login/token.php", url.Values{
		"username": {username},
		"password": {password},
		"service":  {"moodle_mobile_app"},
	})
	if err != nil {
		slog.Error("moodle: token request failed", "url", baseURL, "err", err)
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	slog.Debug("moodle: token response", "http_status", resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read answer failed: %w", err)
	}

	var result tokenResponse
	if err := json.Unmarshal(body, &result); err != nil {
		slog.Error("moodle: token response parse failed", "body", string(body), "err", err)
		return nil, fmt.Errorf("json parse failed: %w", err)
	}
	if result.Error != "" {
		slog.Warn("moodle: token denied", "user", username, "error", result.Error, "errorcode", result.ErrorCode)
		return nil, fmt.Errorf("%s", result.Error)
	}

	slog.Info("moodle: token obtained", "user", username)
	return &TokenPair{Token: result.Token, PrivateToken: result.PrivateToken}, nil
}
