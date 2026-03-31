package providers

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/AuralithAI/rtvortex-server/internal/mcp"
)

type DatadogProvider struct {
	client *http.Client
}

func NewDatadogProvider() *DatadogProvider {
	return &DatadogProvider{
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

func (p *DatadogProvider) Name() string     { return "datadog" }
func (p *DatadogProvider) Category() string { return "monitoring" }
func (p *DatadogProvider) Description() string {
	return "Monitors, events, dashboards, metrics, and infrastructure observability."
}

func (p *DatadogProvider) Actions() []mcp.ActionDef {
	return []mcp.ActionDef{
		{Name: "list_monitors", Description: "List monitors.", OptionalParams: []string{"query", "per_page", "page"}},
		{Name: "get_monitor", Description: "Get monitor details.", RequiredParams: []string{"monitor_id"}},
		{Name: "mute_monitor", Description: "Mute a monitor.", RequiredParams: []string{"monitor_id"}, OptionalParams: []string{"end"}},
		{Name: "list_events", Description: "List events in a time range.", RequiredParams: []string{"start", "end"}, OptionalParams: []string{"sources", "tags"}},
		{Name: "list_dashboards", Description: "List all dashboards.", OptionalParams: []string{"filter"}},
		{Name: "query_metrics", Description: "Query timeseries metrics.", RequiredParams: []string{"from", "to", "query"}},
		{Name: "list_hosts", Description: "List infrastructure hosts.", OptionalParams: []string{"filter", "count", "start"}},
		{Name: "get_host_totals", Description: "Get total host counts.", OptionalParams: []string{"from"}},
	}
}

func (p *DatadogProvider) Execute(ctx context.Context, action string, params map[string]interface{}, token string) (*mcp.Result, error) {
	// Token format: "api_key:app_key" or just "api_key"
	baseURL := "https://api.datadoghq.com/api/v1"

	switch action {
	case "list_monitors":
		q := url.Values{}
		if s := stringParam(params, "query", ""); s != "" {
			q.Set("query", s)
		}
		if pp := stringParam(params, "per_page", ""); pp != "" {
			q.Set("per_page", pp)
		}
		if pg := stringParam(params, "page", ""); pg != "" {
			q.Set("page", pg)
		}
		return p.doGet(ctx, baseURL+"/monitor?"+q.Encode(), token)

	case "get_monitor":
		mid := stringParam(params, "monitor_id", "")
		return p.doGet(ctx, fmt.Sprintf("%s/monitor/%s", baseURL, mid), token)

	case "mute_monitor":
		mid := stringParam(params, "monitor_id", "")
		u := fmt.Sprintf("%s/monitor/%s/mute", baseURL, mid)
		if e := stringParam(params, "end", ""); e != "" {
			u += "?end=" + e
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, nil)
		if err != nil {
			return nil, err
		}
		p.setAuth(req, token)
		return doAPICall(p.client, req)

	case "list_events":
		q := url.Values{
			"start": {stringParam(params, "start", "")},
			"end":   {stringParam(params, "end", "")},
		}
		if s := stringParam(params, "sources", ""); s != "" {
			q.Set("sources", s)
		}
		if t := stringParam(params, "tags", ""); t != "" {
			q.Set("tags", t)
		}
		return p.doGet(ctx, baseURL+"/events?"+q.Encode(), token)

	case "list_dashboards":
		q := url.Values{}
		if f := stringParam(params, "filter", ""); f != "" {
			q.Set("filter[shared]", f)
		}
		return p.doGet(ctx, baseURL+"/dashboard?"+q.Encode(), token)

	case "query_metrics":
		q := url.Values{
			"from":  {stringParam(params, "from", "")},
			"to":    {stringParam(params, "to", "")},
			"query": {stringParam(params, "query", "")},
		}
		return p.doGet(ctx, baseURL+"/query?"+q.Encode(), token)

	case "list_hosts":
		q := url.Values{}
		if f := stringParam(params, "filter", ""); f != "" {
			q.Set("filter", f)
		}
		if c := stringParam(params, "count", ""); c != "" {
			q.Set("count", c)
		}
		return p.doGet(ctx, baseURL+"/hosts?"+q.Encode(), token)

	case "get_host_totals":
		q := url.Values{}
		if f := stringParam(params, "from", ""); f != "" {
			q.Set("from", f)
		}
		return p.doGet(ctx, baseURL+"/hosts/totals?"+q.Encode(), token)

	default:
		return nil, fmt.Errorf("unknown Datadog action %q", action)
	}
}

func (p *DatadogProvider) RefreshToken(_ context.Context, _ string) (string, string, time.Duration, error) {
	return "", "", 0, fmt.Errorf("Datadog uses API/app keys; refresh not supported")
}

func (p *DatadogProvider) setAuth(req *http.Request, token string) {
	// Token could be "api_key:app_key"
	req.Header.Set("DD-API-KEY", token)
	req.Header.Set("Accept", "application/json")
}

func (p *DatadogProvider) doGet(ctx context.Context, u, token string) (*mcp.Result, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	p.setAuth(req, token)
	return doAPICall(p.client, req)
}
