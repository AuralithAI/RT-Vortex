package providers

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/AuralithAI/rtvortex-server/internal/mcp"
)

const stripeBaseURL = "https://api.stripe.com/v1"

type StripeProvider struct {
	client *http.Client
}

func NewStripeProvider() *StripeProvider {
	return &StripeProvider{
		client: &http.Client{Timeout: 20 * time.Second},
	}
}

func (p *StripeProvider) Name() string     { return "stripe" }
func (p *StripeProvider) Category() string { return "finance" }
func (p *StripeProvider) Description() string {
	return "Customers, charges, invoices, subscriptions, and payment management."
}

func (p *StripeProvider) Actions() []mcp.ActionDef {
	return []mcp.ActionDef{
		{Name: "list_customers", Description: "List customers.", OptionalParams: []string{"email", "limit", "starting_after"}},
		{Name: "get_customer", Description: "Get customer details.", RequiredParams: []string{"customer_id"}},
		{Name: "list_charges", Description: "List charges.", OptionalParams: []string{"customer", "limit", "starting_after"}},
		{Name: "list_invoices", Description: "List invoices.", OptionalParams: []string{"customer", "status", "limit"}},
		{Name: "get_invoice", Description: "Get invoice details.", RequiredParams: []string{"invoice_id"}},
		{Name: "list_subscriptions", Description: "List subscriptions.", OptionalParams: []string{"customer", "status", "limit"}},
		{Name: "get_balance", Description: "Get account balance."},
		{Name: "list_events", Description: "List recent events.", OptionalParams: []string{"type", "limit"}},
	}
}

func (p *StripeProvider) Execute(ctx context.Context, action string, params map[string]interface{}, token string) (*mcp.Result, error) {
	switch action {
	case "list_customers":
		q := url.Values{}
		if e := stringParam(params, "email", ""); e != "" {
			q.Set("email", e)
		}
		if l := stringParam(params, "limit", ""); l != "" {
			q.Set("limit", l)
		}
		if s := stringParam(params, "starting_after", ""); s != "" {
			q.Set("starting_after", s)
		}
		return p.doGet(ctx, "/customers?"+q.Encode(), token)

	case "get_customer":
		cid := stringParam(params, "customer_id", "")
		return p.doGet(ctx, fmt.Sprintf("/customers/%s", cid), token)

	case "list_charges":
		q := url.Values{}
		if c := stringParam(params, "customer", ""); c != "" {
			q.Set("customer", c)
		}
		if l := stringParam(params, "limit", ""); l != "" {
			q.Set("limit", l)
		}
		return p.doGet(ctx, "/charges?"+q.Encode(), token)

	case "list_invoices":
		q := url.Values{}
		if c := stringParam(params, "customer", ""); c != "" {
			q.Set("customer", c)
		}
		if s := stringParam(params, "status", ""); s != "" {
			q.Set("status", s)
		}
		if l := stringParam(params, "limit", ""); l != "" {
			q.Set("limit", l)
		}
		return p.doGet(ctx, "/invoices?"+q.Encode(), token)

	case "get_invoice":
		iid := stringParam(params, "invoice_id", "")
		return p.doGet(ctx, fmt.Sprintf("/invoices/%s", iid), token)

	case "list_subscriptions":
		q := url.Values{}
		if c := stringParam(params, "customer", ""); c != "" {
			q.Set("customer", c)
		}
		if s := stringParam(params, "status", ""); s != "" {
			q.Set("status", s)
		}
		if l := stringParam(params, "limit", ""); l != "" {
			q.Set("limit", l)
		}
		return p.doGet(ctx, "/subscriptions?"+q.Encode(), token)

	case "get_balance":
		return p.doGet(ctx, "/balance", token)

	case "list_events":
		q := url.Values{}
		if t := stringParam(params, "type", ""); t != "" {
			q.Set("type", t)
		}
		if l := stringParam(params, "limit", ""); l != "" {
			q.Set("limit", l)
		}
		return p.doGet(ctx, "/events?"+q.Encode(), token)

	default:
		return nil, fmt.Errorf("unknown Stripe action %q", action)
	}
}

func (p *StripeProvider) RefreshToken(_ context.Context, _ string) (string, string, time.Duration, error) {
	return "", "", 0, fmt.Errorf("Stripe uses API keys; refresh not supported")
}

func (p *StripeProvider) doGet(ctx context.Context, path, token string) (*mcp.Result, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, stripeBaseURL+path, nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(token, "")
	req.Header.Set("Accept", "application/json")
	return doAPICall(p.client, req)
}

func (p *StripeProvider) doPost(ctx context.Context, path string, form url.Values, token string) (*mcp.Result, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, stripeBaseURL+path, bytes.NewBufferString(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(token, "")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return doAPICall(p.client, req)
}
