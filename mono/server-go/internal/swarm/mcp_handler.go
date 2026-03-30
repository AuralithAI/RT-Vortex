package swarm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/google/uuid"

	"github.com/AuralithAI/rtvortex-server/internal/mcp"
)

type MCPCaller interface {
	Execute(ctx context.Context, req mcp.ExecuteRequest) (*mcp.Result, error)
	ListProviders() []mcp.ProviderInfo
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
