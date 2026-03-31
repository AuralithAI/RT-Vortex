package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/AuralithAI/rtvortex-server/internal/mcp"
)

// doAPICall is a shared helper for all providers to make an HTTP request
// and return the response as a standardised mcp.Result.
func doAPICall(client *http.Client, req *http.Request) (*mcp.Result, error) {
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("API request failed: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode == http.StatusNoContent {
		return &mcp.Result{Success: true, Data: map[string]interface{}{"status": "ok"}}, nil
	}

	var data map[string]interface{}
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, fmt.Errorf("invalid JSON (status %d): %s", resp.StatusCode, string(raw[:min(len(raw), 200)]))
	}

	if resp.StatusCode >= 400 {
		errMsg := ""
		if e, ok := data["error"].(map[string]interface{}); ok {
			errMsg, _ = e["message"].(string)
		} else if e, ok := data["error"].(string); ok {
			errMsg = e
		}
		return &mcp.Result{Success: false, Data: data, Error: errMsg}, nil
	}

	return &mcp.Result{Success: true, Data: data}, nil
}

// googleRefreshToken performs a Google OAuth2 token refresh using the given
// token URL and refresh token. This is shared by all Google providers.
func googleRefreshToken(ctx context.Context, tokenURL, refreshToken string) (string, string, time.Duration, error) {
	data := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
	}
	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, bytes.NewBufferString(data.Encode()))
	if err != nil {
		return "", "", 0, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := client.Do(req)
	if err != nil {
		return "", "", 0, fmt.Errorf("Google token refresh failed: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		Error        string `json:"error"`
		ErrorDesc    string `json:"error_description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", "", 0, err
	}
	if result.Error != "" {
		return "", "", 0, fmt.Errorf("Google refresh error: %s — %s", result.Error, result.ErrorDesc)
	}
	if result.AccessToken == "" {
		return "", "", 0, fmt.Errorf("empty access token in Google refresh response")
	}
	return result.AccessToken, result.RefreshToken, time.Duration(result.ExpiresIn) * time.Second, nil
}
