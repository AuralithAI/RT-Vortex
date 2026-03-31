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

const twilioBaseURL = "https://api.twilio.com/2010-04-01"

type TwilioProvider struct {
	client *http.Client
}

func NewTwilioProvider() *TwilioProvider {
	return &TwilioProvider{
		client: &http.Client{Timeout: 20 * time.Second},
	}
}

func (p *TwilioProvider) Name() string     { return "twilio" }
func (p *TwilioProvider) Category() string { return "messaging" }
func (p *TwilioProvider) Description() string {
	return "SMS, calls, messaging services, phone number lookup, and communication APIs."
}

func (p *TwilioProvider) Actions() []mcp.ActionDef {
	return []mcp.ActionDef{
		{Name: "send_sms", Description: "Send an SMS message.", RequiredParams: []string{"account_sid", "to", "from", "body"}, ConsentRequired: true},
		{Name: "list_messages", Description: "List SMS messages.", RequiredParams: []string{"account_sid"}, OptionalParams: []string{"to", "from", "date_sent", "page_size"}},
		{Name: "get_message", Description: "Get message details.", RequiredParams: []string{"account_sid", "message_sid"}},
		{Name: "list_calls", Description: "List calls.", RequiredParams: []string{"account_sid"}, OptionalParams: []string{"to", "from", "status", "page_size"}},
		{Name: "get_call", Description: "Get call details.", RequiredParams: []string{"account_sid", "call_sid"}},
		{Name: "list_phone_numbers", Description: "List account phone numbers.", RequiredParams: []string{"account_sid"}},
		{Name: "lookup_phone", Description: "Look up a phone number.", RequiredParams: []string{"phone_number"}, OptionalParams: []string{"type"}},
		{Name: "get_account", Description: "Get account info.", RequiredParams: []string{"account_sid"}},
	}
}

func (p *TwilioProvider) Execute(ctx context.Context, action string, params map[string]interface{}, token string) (*mcp.Result, error) {
	// Token is the auth_token; account_sid comes from params
	sid := stringParam(params, "account_sid", "")

	switch action {
	case "send_sms":
		form := url.Values{
			"To":   {stringParam(params, "to", "")},
			"From": {stringParam(params, "from", "")},
			"Body": {stringParam(params, "body", "")},
		}
		return p.doPostForm(ctx, fmt.Sprintf("/Accounts/%s/Messages.json", sid), form, sid, token)

	case "list_messages":
		q := url.Values{}
		if t := stringParam(params, "to", ""); t != "" {
			q.Set("To", t)
		}
		if f := stringParam(params, "from", ""); f != "" {
			q.Set("From", f)
		}
		if ps := stringParam(params, "page_size", ""); ps != "" {
			q.Set("PageSize", ps)
		}
		return p.doGet(ctx, fmt.Sprintf("/Accounts/%s/Messages.json?%s", sid, q.Encode()), sid, token)

	case "get_message":
		msid := stringParam(params, "message_sid", "")
		return p.doGet(ctx, fmt.Sprintf("/Accounts/%s/Messages/%s.json", sid, msid), sid, token)

	case "list_calls":
		q := url.Values{}
		if t := stringParam(params, "to", ""); t != "" {
			q.Set("To", t)
		}
		if f := stringParam(params, "from", ""); f != "" {
			q.Set("From", f)
		}
		if s := stringParam(params, "status", ""); s != "" {
			q.Set("Status", s)
		}
		return p.doGet(ctx, fmt.Sprintf("/Accounts/%s/Calls.json?%s", sid, q.Encode()), sid, token)

	case "get_call":
		csid := stringParam(params, "call_sid", "")
		return p.doGet(ctx, fmt.Sprintf("/Accounts/%s/Calls/%s.json", sid, csid), sid, token)

	case "list_phone_numbers":
		return p.doGet(ctx, fmt.Sprintf("/Accounts/%s/IncomingPhoneNumbers.json", sid), sid, token)

	case "lookup_phone":
		phone := stringParam(params, "phone_number", "")
		q := url.Values{}
		if t := stringParam(params, "type", ""); t != "" {
			q.Set("Type", t)
		}
		lookupURL := fmt.Sprintf("https://lookups.twilio.com/v1/PhoneNumbers/%s?%s", url.PathEscape(phone), q.Encode())
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, lookupURL, nil)
		if err != nil {
			return nil, err
		}
		req.SetBasicAuth(sid, token)
		return doAPICall(p.client, req)

	case "get_account":
		return p.doGet(ctx, fmt.Sprintf("/Accounts/%s.json", sid), sid, token)

	default:
		return nil, fmt.Errorf("unknown Twilio action %q", action)
	}
}

func (p *TwilioProvider) RefreshToken(_ context.Context, _ string) (string, string, time.Duration, error) {
	return "", "", 0, fmt.Errorf("Twilio uses account SID + auth token; refresh not supported")
}

func (p *TwilioProvider) doGet(ctx context.Context, path, sid, authToken string) (*mcp.Result, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, twilioBaseURL+path, nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(sid, authToken)
	return doAPICall(p.client, req)
}

func (p *TwilioProvider) doPostForm(ctx context.Context, path string, form url.Values, sid, authToken string) (*mcp.Result, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, twilioBaseURL+path, bytes.NewBufferString(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(sid, authToken)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return doAPICall(p.client, req)
}
