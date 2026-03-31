package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// CustomMCPTemplate describes a user-defined MCP provider template that can be
// registered at runtime. Templates are stored in the database and loaded on
// startup (or added live via the API).
type CustomMCPTemplate struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`        // unique snake_case identifier
	Label       string            `json:"label"`       // human-readable label
	Category    string            `json:"category"`    // category for grouping
	Description string            `json:"description"` // short description
	BaseURL     string            `json:"base_url"`    // API base URL
	AuthType    string            `json:"auth_type"`   // bearer, basic, header, query
	AuthHeader  string            `json:"auth_header"` // custom header name (for auth_type=header)
	Actions     []CustomActionDef `json:"actions"`     // list of actions
	CreatedBy   string            `json:"created_by"`  // user ID
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

// CustomActionDef defines a single action within a custom MCP template.
type CustomActionDef struct {
	Name            string   `json:"name"`
	Description     string   `json:"description"`
	Method          string   `json:"method"` // GET, POST, PUT, PATCH, DELETE
	Path            string   `json:"path"`   // path template, e.g. "/users/{user_id}"
	RequiredParams  []string `json:"required_params,omitempty"`
	OptionalParams  []string `json:"optional_params,omitempty"`
	BodyTemplate    string   `json:"body_template,omitempty"` // JSON template with {{param}} placeholders
	ConsentRequired bool     `json:"consent_required"`
}

// ValidationError represents a single validation issue.
type ValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// ValidateTemplate checks a CustomMCPTemplate for correctness and returns any
// validation errors.
func ValidateTemplate(t *CustomMCPTemplate) []ValidationError {
	var errs []ValidationError

	nameRe := regexp.MustCompile(`^[a-z][a-z0-9_]{1,48}$`)

	if t.Name == "" {
		errs = append(errs, ValidationError{Field: "name", Message: "Name is required."})
	} else if !nameRe.MatchString(t.Name) {
		errs = append(errs, ValidationError{Field: "name", Message: "Name must be lowercase alphanumeric with underscores, 2-49 chars, starting with a letter."})
	}

	if t.Label == "" {
		errs = append(errs, ValidationError{Field: "label", Message: "Label is required."})
	}

	if t.BaseURL == "" {
		errs = append(errs, ValidationError{Field: "base_url", Message: "Base URL is required."})
	} else if u, err := url.Parse(t.BaseURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		errs = append(errs, ValidationError{Field: "base_url", Message: "Base URL must be a valid http/https URL."})
	}

	validAuthTypes := map[string]bool{"bearer": true, "basic": true, "header": true, "query": true}
	if t.AuthType == "" {
		errs = append(errs, ValidationError{Field: "auth_type", Message: "Auth type is required."})
	} else if !validAuthTypes[t.AuthType] {
		errs = append(errs, ValidationError{Field: "auth_type", Message: "Auth type must be one of: bearer, basic, header, query."})
	}

	if t.AuthType == "header" && t.AuthHeader == "" {
		errs = append(errs, ValidationError{Field: "auth_header", Message: "Auth header name is required when auth_type is 'header'."})
	}

	if len(t.Actions) == 0 {
		errs = append(errs, ValidationError{Field: "actions", Message: "At least one action is required."})
	}

	validMethods := map[string]bool{"GET": true, "POST": true, "PUT": true, "PATCH": true, "DELETE": true}
	actionNames := make(map[string]bool)
	for i, a := range t.Actions {
		prefix := fmt.Sprintf("actions[%d]", i)
		if a.Name == "" {
			errs = append(errs, ValidationError{Field: prefix + ".name", Message: "Action name is required."})
		} else if !nameRe.MatchString(a.Name) {
			errs = append(errs, ValidationError{Field: prefix + ".name", Message: "Action name must be lowercase alphanumeric with underscores."})
		} else if actionNames[a.Name] {
			errs = append(errs, ValidationError{Field: prefix + ".name", Message: fmt.Sprintf("Duplicate action name %q.", a.Name)})
		} else {
			actionNames[a.Name] = true
		}
		if a.Description == "" {
			errs = append(errs, ValidationError{Field: prefix + ".description", Message: "Action description is required."})
		}
		if !validMethods[strings.ToUpper(a.Method)] {
			errs = append(errs, ValidationError{Field: prefix + ".method", Message: "Method must be GET, POST, PUT, PATCH, or DELETE."})
		}
		if a.Path == "" {
			errs = append(errs, ValidationError{Field: prefix + ".path", Message: "Action path is required."})
		}
		if a.BodyTemplate != "" {
			var js json.RawMessage
			// Replace placeholders with dummy values before parsing
			testJSON := regexp.MustCompile(`\{\{[^}]+\}\}`).ReplaceAllString(a.BodyTemplate, `"__test__"`)
			if err := json.Unmarshal([]byte(testJSON), &js); err != nil {
				errs = append(errs, ValidationError{Field: prefix + ".body_template", Message: "Body template must be valid JSON (with {{param}} placeholders)."})
			}
		}
	}

	return errs
}

// SimulateConnection performs a lightweight connectivity test against the custom
// MCP's base URL. It sends a HEAD (or GET) to baseURL and checks for a non-5xx.
func SimulateConnection(ctx context.Context, baseURL, token, authType, authHeader string) (*Result, error) {
	client := &http.Client{Timeout: 10 * time.Second}

	req, err := http.NewRequestWithContext(ctx, http.MethodHead, baseURL, nil)
	if err != nil {
		return nil, fmt.Errorf("invalid base URL: %w", err)
	}

	applyAuth(req, token, authType, authHeader)

	resp, err := client.Do(req)
	if err != nil {
		return &Result{
			Success: false,
			Error:   fmt.Sprintf("Connection failed: %v", err),
		}, nil
	}
	defer resp.Body.Close()

	// If HEAD gives 405, retry with GET
	if resp.StatusCode == http.StatusMethodNotAllowed {
		req2, _ := http.NewRequestWithContext(ctx, http.MethodGet, baseURL, nil)
		applyAuth(req2, token, authType, authHeader)
		resp2, err2 := client.Do(req2)
		if err2 != nil {
			return &Result{Success: false, Error: fmt.Sprintf("Connection failed: %v", err2)}, nil
		}
		defer resp2.Body.Close()
		resp = resp2
	}

	if resp.StatusCode >= 500 {
		return &Result{
			Success: false,
			Error:   fmt.Sprintf("Server error: HTTP %d", resp.StatusCode),
		}, nil
	}

	return &Result{
		Success: true,
		Data:    map[string]interface{}{"status_code": resp.StatusCode, "message": "Connection successful"},
	}, nil
}

// ── Custom MCP Provider (runtime) ───────────────────────────────────────────

// CustomMCPProvider is a dynamically-created Provider that wraps a
// CustomMCPTemplate and makes real HTTP calls.
type CustomMCPProvider struct {
	template *CustomMCPTemplate
	client   *http.Client
}

func NewCustomMCPProvider(t *CustomMCPTemplate) *CustomMCPProvider {
	return &CustomMCPProvider{
		template: t,
		client:   &http.Client{Timeout: 20 * time.Second},
	}
}

func (p *CustomMCPProvider) Name() string        { return p.template.Name }
func (p *CustomMCPProvider) Category() string    { return p.template.Category }
func (p *CustomMCPProvider) Description() string { return p.template.Description }

func (p *CustomMCPProvider) Actions() []ActionDef {
	out := make([]ActionDef, len(p.template.Actions))
	for i, a := range p.template.Actions {
		out[i] = ActionDef{
			Name:            a.Name,
			Description:     a.Description,
			RequiredParams:  a.RequiredParams,
			OptionalParams:  a.OptionalParams,
			ConsentRequired: a.ConsentRequired,
		}
	}
	return out
}

func (p *CustomMCPProvider) Execute(ctx context.Context, action string, params map[string]interface{}, token string) (*Result, error) {
	var actionDef *CustomActionDef
	for i := range p.template.Actions {
		if strings.EqualFold(p.template.Actions[i].Name, action) {
			actionDef = &p.template.Actions[i]
			break
		}
	}
	if actionDef == nil {
		return nil, fmt.Errorf("unknown action %q for custom provider %q", action, p.template.Name)
	}

	// Build the full URL with path-parameter interpolation
	path := actionDef.Path
	for k, v := range params {
		placeholder := "{" + k + "}"
		if str, ok := v.(string); ok {
			path = strings.ReplaceAll(path, placeholder, url.PathEscape(str))
		}
	}
	fullURL := strings.TrimRight(p.template.BaseURL, "/") + "/" + strings.TrimLeft(path, "/")

	// Build request body if needed
	method := strings.ToUpper(actionDef.Method)
	var bodyReader io.Reader
	if actionDef.BodyTemplate != "" && method != "GET" && method != "DELETE" {
		bodyStr := actionDef.BodyTemplate
		for k, v := range params {
			placeholder := "{{" + k + "}}"
			switch val := v.(type) {
			case string:
				bodyStr = strings.ReplaceAll(bodyStr, placeholder, val)
			default:
				jsonVal, _ := json.Marshal(val)
				bodyStr = strings.ReplaceAll(bodyStr, placeholder, string(jsonVal))
			}
		}
		bodyReader = bytes.NewBufferString(bodyStr)
	} else if method == "POST" || method == "PUT" || method == "PATCH" {
		// Auto-serialize non-path params as JSON body
		bodyData := make(map[string]interface{})
		for k, v := range params {
			if strings.HasPrefix(k, "_") {
				continue
			}
			bodyData[k] = v
		}
		if len(bodyData) > 0 {
			data, _ := json.Marshal(bodyData)
			bodyReader = bytes.NewReader(data)
		}
	}

	req, err := http.NewRequestWithContext(ctx, method, fullURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("failed to build request: %w", err)
	}

	applyAuth(req, token, p.template.AuthType, p.template.AuthHeader)
	req.Header.Set("Accept", "application/json")
	if bodyReader != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("API request failed: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode == http.StatusNoContent {
		return &Result{Success: true, Data: map[string]interface{}{"status": "ok"}}, nil
	}

	var data map[string]interface{}
	if err := json.Unmarshal(raw, &data); err != nil {
		// If not JSON, wrap raw text
		data = map[string]interface{}{
			"raw_response": string(raw[:min(len(raw), 500)]),
			"content_type": resp.Header.Get("Content-Type"),
			"status_code":  resp.StatusCode,
		}
	}

	if resp.StatusCode >= 400 {
		errMsg := fmt.Sprintf("HTTP %d", resp.StatusCode)
		if e, ok := data["error"].(string); ok {
			errMsg = e
		} else if e, ok := data["message"].(string); ok {
			errMsg = e
		}
		return &Result{Success: false, Data: data, Error: errMsg}, nil
	}

	return &Result{Success: true, Data: data}, nil
}

func (p *CustomMCPProvider) RefreshToken(_ context.Context, _ string) (string, string, time.Duration, error) {
	return "", "", 0, fmt.Errorf("custom MCP provider %q does not support token refresh", p.template.Name)
}

// ── Helpers ─────────────────────────────────────────────────────────────────

func applyAuth(req *http.Request, token, authType, authHeader string) {
	switch authType {
	case "bearer":
		req.Header.Set("Authorization", "Bearer "+token)
	case "basic":
		req.Header.Set("Authorization", "Basic "+token)
	case "header":
		if authHeader != "" {
			req.Header.Set(authHeader, token)
		}
	case "query":
		q := req.URL.Query()
		q.Set("api_key", token)
		req.URL.RawQuery = q.Encode()
	}
}
