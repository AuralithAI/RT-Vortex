package swarm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/google/uuid"

	"github.com/AuralithAI/rtvortex-server/internal/mcp"
	"github.com/AuralithAI/rtvortex-server/internal/store"
)

type MCPCaller interface {
	Execute(ctx context.Context, req mcp.ExecuteRequest) (*mcp.Result, error)
	ListProviders() []mcp.ProviderInfo
	ListConnections(ctx context.Context, userID uuid.UUID, orgID *uuid.UUID) ([]store.MCPConnection, error)
}

func (h *Handler) HandleMCPCall(w http.ResponseWriter, r *http.Request) {
	if h.MCPSvc == nil {
		http.Error(w, `{"error":"MCP integrations not available"}`, http.StatusServiceUnavailable)
		return
	}

	var body struct {
		Provider string                 `json:"provider"`
		Action   string                 `json:"action"`
		Params   map[string]interface{} `json:"params"`
		UserID   string                 `json:"user_id"`
		OrgID    string                 `json:"org_id"`
		AgentID  string                 `json:"agent_id"`
		TaskID   string                 `json:"task_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	if body.Provider == "" || body.Action == "" {
		http.Error(w, `{"error":"provider and action are required"}`, http.StatusBadRequest)
		return
	}

	userID, err := uuid.Parse(body.UserID)
	if err != nil {
		http.Error(w, `{"error":"invalid user_id"}`, http.StatusBadRequest)
		return
	}

	var orgID *uuid.UUID
	if body.OrgID != "" {
		id, err := uuid.Parse(body.OrgID)
		if err == nil {
			orgID = &id
		}
	}

	req := mcp.ExecuteRequest{
		UserID:   userID,
		OrgID:    orgID,
		Provider: body.Provider,
		Action:   body.Action,
		Params:   body.Params,
		AgentID:  body.AgentID,
		TaskID:   body.TaskID,
	}

	result, err := h.MCPSvc.Execute(r.Context(), req)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

func (h *Handler) HandleMCPListProviders(w http.ResponseWriter, r *http.Request) {
	if h.MCPSvc == nil {
		http.Error(w, `{"error":"MCP integrations not available"}`, http.StatusServiceUnavailable)
		return
	}

	providers := h.MCPSvc.ListProviders()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(providers)
}

func (h *Handler) HandleMCPDescribeAction(w http.ResponseWriter, r *http.Request) {
	if h.MCPSvc == nil {
		http.Error(w, `{"error":"MCP integrations not available"}`, http.StatusServiceUnavailable)
		return
	}

	providerName := r.URL.Query().Get("provider")
	actionName := r.URL.Query().Get("action")

	providers := h.MCPSvc.ListProviders()
	for _, p := range providers {
		if p.Name != providerName {
			continue
		}
		for _, a := range p.Actions {
			if a.Name == actionName {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(a)
				return
			}
		}
	}

	http.Error(w, fmt.Sprintf(`{"error":"action %q not found for provider %q"}`, actionName, providerName), http.StatusNotFound)
}

// ── Agent Tool Discovery ────────────────────────────────────────────────────

// HandleMCPListTools returns a flattened list of all available MCP tools
// in the format expected by the agent swarm — each tool is a
// provider/action pair with full metadata.
func (h *Handler) HandleMCPListTools(w http.ResponseWriter, r *http.Request) {
	if h.MCPSvc == nil {
		http.Error(w, `{"error":"MCP integrations not available"}`, http.StatusServiceUnavailable)
		return
	}

	type AgentTool struct {
		ToolID          string   `json:"tool_id"` // "provider.action"
		Provider        string   `json:"provider"`
		Action          string   `json:"action"`
		Description     string   `json:"description"`
		Category        string   `json:"category"`
		RequiredParams  []string `json:"required_params"`
		OptionalParams  []string `json:"optional_params,omitempty"`
		ConsentRequired bool     `json:"consent_required"`
	}

	providers := h.MCPSvc.ListProviders()
	tools := make([]AgentTool, 0)
	for _, p := range providers {
		for _, a := range p.Actions {
			tools = append(tools, AgentTool{
				ToolID:          p.Name + "." + a.Name,
				Provider:        p.Name,
				Action:          a.Name,
				Description:     a.Description,
				Category:        p.Category,
				RequiredParams:  a.RequiredParams,
				OptionalParams:  a.OptionalParams,
				ConsentRequired: a.ConsentRequired,
			})
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"tools": tools,
		"count": len(tools),
	})
}

// HandleMCPBatchCall executes multiple MCP calls in sequence for a single
// agent step, returning results keyed by a caller-chosen ID.
func (h *Handler) HandleMCPBatchCall(w http.ResponseWriter, r *http.Request) {
	if h.MCPSvc == nil {
		http.Error(w, `{"error":"MCP integrations not available"}`, http.StatusServiceUnavailable)
		return
	}

	var body struct {
		UserID  string `json:"user_id"`
		OrgID   string `json:"org_id"`
		AgentID string `json:"agent_id"`
		TaskID  string `json:"task_id"`
		Calls   []struct {
			ID       string                 `json:"id"`
			Provider string                 `json:"provider"`
			Action   string                 `json:"action"`
			Params   map[string]interface{} `json:"params"`
		} `json:"calls"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	userID, err := uuid.Parse(body.UserID)
	if err != nil {
		http.Error(w, `{"error":"invalid user_id"}`, http.StatusBadRequest)
		return
	}

	var orgID *uuid.UUID
	if body.OrgID != "" {
		id, err := uuid.Parse(body.OrgID)
		if err == nil {
			orgID = &id
		}
	}

	type BatchResult struct {
		ID      string      `json:"id"`
		Success bool        `json:"success"`
		Data    interface{} `json:"data,omitempty"`
		Error   string      `json:"error,omitempty"`
	}

	results := make([]BatchResult, 0, len(body.Calls))
	for _, call := range body.Calls {
		req := mcp.ExecuteRequest{
			UserID:   userID,
			OrgID:    orgID,
			Provider: call.Provider,
			Action:   call.Action,
			Params:   call.Params,
			AgentID:  body.AgentID,
			TaskID:   body.TaskID,
		}

		result, err := h.MCPSvc.Execute(r.Context(), req)
		if err != nil {
			results = append(results, BatchResult{
				ID:      call.ID,
				Success: false,
				Error:   err.Error(),
			})
			continue
		}

		results = append(results, BatchResult{
			ID:      call.ID,
			Success: result.Success,
			Data:    result.Data,
			Error:   result.Error,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"results": results,
	})
}

// HandleMCPCheckConnections returns which providers have active connections
// for a given user — used by agents to filter available tools.
func (h *Handler) HandleMCPCheckConnections(w http.ResponseWriter, r *http.Request) {
	if h.MCPSvc == nil {
		http.Error(w, `{"error":"MCP integrations not available"}`, http.StatusServiceUnavailable)
		return
	}

	userIDStr := r.URL.Query().Get("user_id")
	orgIDStr := r.URL.Query().Get("org_id")

	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		http.Error(w, `{"error":"invalid user_id"}`, http.StatusBadRequest)
		return
	}

	var orgID *uuid.UUID
	if orgIDStr != "" {
		id, err := uuid.Parse(orgIDStr)
		if err == nil {
			orgID = &id
		}
	}

	connections, err := h.MCPSvc.ListConnections(r.Context(), userID, orgID)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	connected := make(map[string]bool)
	for _, c := range connections {
		if c.Status == "active" {
			connected[c.Provider] = true
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"connected_providers": connected,
	})
}
