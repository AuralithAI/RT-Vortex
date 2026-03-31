package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/AuralithAI/rtvortex-server/internal/mcp"
)

type TrelloProvider struct {
	client  *http.Client
	baseURL string
}

func NewTrelloProvider() *TrelloProvider {
	return &TrelloProvider{
		client:  &http.Client{Timeout: 20 * time.Second},
		baseURL: "https://api.trello.com/1",
	}
}

func (p *TrelloProvider) Name() string     { return "trello" }
func (p *TrelloProvider) Category() string { return "project_management" }
func (p *TrelloProvider) Description() string {
	return "Boards, lists, cards, checklists, and members."
}

func (p *TrelloProvider) Actions() []mcp.ActionDef {
	return []mcp.ActionDef{
		{Name: "list_boards", Description: "List boards for the authenticated member."},
		{Name: "get_board", Description: "Get board details.", RequiredParams: []string{"board_id"}},
		{Name: "list_cards", Description: "List cards on a board or list.", RequiredParams: []string{"board_id"}, OptionalParams: []string{"list_id"}},
		{Name: "get_card", Description: "Get card details.", RequiredParams: []string{"card_id"}},
		{Name: "create_card", Description: "Create a new card.", RequiredParams: []string{"list_id", "name"}, OptionalParams: []string{"desc", "due", "idMembers"}, ConsentRequired: true},
		{Name: "update_card", Description: "Update a card.", RequiredParams: []string{"card_id"}, OptionalParams: []string{"name", "desc", "idList", "due", "closed"}, ConsentRequired: true},
		{Name: "add_comment", Description: "Add a comment to a card.", RequiredParams: []string{"card_id", "text"}, ConsentRequired: true},
		{Name: "search", Description: "Search Trello.", RequiredParams: []string{"query"}, OptionalParams: []string{"modelTypes"}},
	}
}

func (p *TrelloProvider) Execute(ctx context.Context, action string, params map[string]interface{}, token string) (*mcp.Result, error) {
	// Trello uses OAuth1 token passed as query param: key=APP_KEY&token=MEMBER_TOKEN
	// We expect token to be "key:token" format
	switch action {
	case "list_boards":
		return p.doGet(ctx, p.baseURL+"/members/me/boards", token)

	case "get_board":
		bid := stringParam(params, "board_id", "")
		return p.doGet(ctx, fmt.Sprintf("%s/boards/%s", p.baseURL, bid), token)

	case "list_cards":
		bid := stringParam(params, "board_id", "")
		if lid := stringParam(params, "list_id", ""); lid != "" {
			return p.doGet(ctx, fmt.Sprintf("%s/lists/%s/cards", p.baseURL, lid), token)
		}
		return p.doGet(ctx, fmt.Sprintf("%s/boards/%s/cards", p.baseURL, bid), token)

	case "get_card":
		cid := stringParam(params, "card_id", "")
		return p.doGet(ctx, fmt.Sprintf("%s/cards/%s", p.baseURL, cid), token)

	case "create_card":
		body := map[string]interface{}{
			"idList": stringParam(params, "list_id", ""),
			"name":   stringParam(params, "name", ""),
		}
		if d := stringParam(params, "desc", ""); d != "" {
			body["desc"] = d
		}
		if due := stringParam(params, "due", ""); due != "" {
			body["due"] = due
		}
		if m := stringParam(params, "idMembers", ""); m != "" {
			body["idMembers"] = m
		}
		return p.doPost(ctx, p.baseURL+"/cards", body, token)

	case "update_card":
		cid := stringParam(params, "card_id", "")
		body := map[string]interface{}{}
		if n := stringParam(params, "name", ""); n != "" {
			body["name"] = n
		}
		if d := stringParam(params, "desc", ""); d != "" {
			body["desc"] = d
		}
		if l := stringParam(params, "idList", ""); l != "" {
			body["idList"] = l
		}
		if due := stringParam(params, "due", ""); due != "" {
			body["due"] = due
		}
		if c := stringParam(params, "closed", ""); c != "" {
			body["closed"] = c == "true"
		}
		return p.doPut(ctx, fmt.Sprintf("%s/cards/%s", p.baseURL, cid), body, token)

	case "add_comment":
		cid := stringParam(params, "card_id", "")
		body := map[string]interface{}{"text": stringParam(params, "text", "")}
		return p.doPost(ctx, fmt.Sprintf("%s/cards/%s/actions/comments", p.baseURL, cid), body, token)

	case "search":
		q := url.Values{
			"query": {stringParam(params, "query", "")},
		}
		if mt := stringParam(params, "modelTypes", ""); mt != "" {
			q.Set("modelTypes", mt)
		}
		return p.doGet(ctx, p.baseURL+"/search?"+q.Encode(), token)

	default:
		return nil, fmt.Errorf("unknown Trello action %q", action)
	}
}

func (p *TrelloProvider) RefreshToken(_ context.Context, _ string) (string, string, time.Duration, error) {
	return "", "", 0, fmt.Errorf("Trello uses OAuth tokens; refresh not supported")
}

func (p *TrelloProvider) doGet(ctx context.Context, u, token string) (*mcp.Result, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "OAuth oauth_consumer_key=\"\", oauth_token=\""+token+"\"")
	req.Header.Set("Accept", "application/json")
	return doAPICall(p.client, req)
}

func (p *TrelloProvider) doPost(ctx context.Context, u string, body interface{}, token string) (*mcp.Result, error) {
	data, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "OAuth oauth_consumer_key=\"\", oauth_token=\""+token+"\"")
	req.Header.Set("Content-Type", "application/json")
	return doAPICall(p.client, req)
}

func (p *TrelloProvider) doPut(ctx context.Context, u string, body interface{}, token string) (*mcp.Result, error) {
	data, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, u, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "OAuth oauth_consumer_key=\"\", oauth_token=\""+token+"\"")
	req.Header.Set("Content-Type", "application/json")
	return doAPICall(p.client, req)
}
