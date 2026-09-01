package zitadelapi

import (
	"bytes"
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

type Project struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type ProjectInput struct {
	DisplayName string
}

type Application struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	ClientID string `json:"clientId"`
}

type ApplicationInput struct {
	DisplayName            string
	AppType                string
	AuthMethod             string
	ResponseType           string
	GrantType              string
	RedirectURIs           []string
	PostLogoutRedirectURIs []string
}

type TokenSource func() (string, error)

type Client struct {
	baseURL     *url.URL
	host        string
	tokenSource TokenSource
	httpClient  *http.Client
}

func NewClient(baseURL, host string, tokenSource TokenSource) (*Client, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse Zitadel API URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, errors.New("Zitadel API URL must use http or https")
	}
	if parsed.Host == "" {
		return nil, errors.New("Zitadel API URL must include a host")
	}
	if host == "" {
		return nil, errors.New("Zitadel instance host is required")
	}
	if tokenSource == nil {
		return nil, errors.New("Zitadel token source is required")
	}
	return &Client{
		baseURL:     parsed,
		host:        host,
		tokenSource: tokenSource,
		httpClient:  &http.Client{Timeout: 10 * time.Second},
	}, nil
}

func (c *Client) FindByID(ctx context.Context, organization, id string) (Project, bool, error) {
	organizationID, err := c.organizationID(ctx, organization)
	if err != nil {
		return Project{}, false, err
	}
	response := struct {
		Project Project `json:"project"`
	}{}
	if err := c.request(ctx, http.MethodGet, "/management/v1/projects/"+url.PathEscape(id), organizationID, nil, &response); err != nil {
		var statusError *statusError
		if errors.As(err, &statusError) && statusError.code == http.StatusNotFound {
			return Project{}, false, nil
		}
		return Project{}, false, err
	}
	return response.Project, true, nil
}

func (c *Client) FindByName(ctx context.Context, organization, name string) (Project, bool, error) {
	organizationID, err := c.organizationID(ctx, organization)
	if err != nil {
		return Project{}, false, err
	}
	request := map[string]any{
		"query": map[string]any{"limit": 2},
		"queries": []map[string]any{{
			"nameQuery": map[string]string{"name": name, "method": "TEXT_QUERY_METHOD_EQUALS"},
		}},
	}
	response := struct {
		Result []Project `json:"result"`
	}{}
	if err := c.request(ctx, http.MethodPost, "/management/v1/projects/_search", organizationID, request, &response); err != nil {
		return Project{}, false, err
	}
	if len(response.Result) == 0 {
		return Project{}, false, nil
	}
	if len(response.Result) > 1 {
		return Project{}, false, fmt.Errorf("multiple Zitadel projects named %q", name)
	}
	return response.Result[0], true, nil
}

func (c *Client) Create(ctx context.Context, organization string, input ProjectInput) (Project, error) {
	organizationID, err := c.organizationID(ctx, organization)
	if err != nil {
		return Project{}, err
	}
	request := map[string]any{
		"name":                 input.DisplayName,
		"projectRoleAssertion": true,
		"projectRoleCheck":     false,
		"hasProjectCheck":      false,
	}
	response := struct {
		ID string `json:"id"`
	}{}
	if err := c.request(ctx, http.MethodPost, "/management/v1/projects", organizationID, request, &response); err != nil {
		return Project{}, err
	}
	if response.ID == "" {
		return Project{}, errors.New("Zitadel create project response has no id")
	}
	return Project{ID: response.ID, Name: input.DisplayName}, nil
}

func (c *Client) FindApplicationByID(ctx context.Context, organization, projectID, appID string) (Application, bool, error) {
	organizationID, err := c.organizationID(ctx, organization)
	if err != nil {
		return Application{}, false, err
	}
	response := struct {
		App Application `json:"app"`
	}{}
	path := "/management/v1/projects/" + url.PathEscape(projectID) + "/apps/" + url.PathEscape(appID)
	if err := c.request(ctx, http.MethodGet, path, organizationID, nil, &response); err != nil {
		var statusError *statusError
		if errors.As(err, &statusError) && statusError.code == http.StatusNotFound {
			return Application{}, false, nil
		}
		return Application{}, false, err
	}
	return response.App, true, nil
}

func (c *Client) FindApplicationByName(ctx context.Context, organization, projectID, name string) (Application, bool, error) {
	organizationID, err := c.organizationID(ctx, organization)
	if err != nil {
		return Application{}, false, err
	}
	request := map[string]any{
		"query":   map[string]any{"limit": 2},
		"queries": []map[string]any{{"nameQuery": map[string]string{"name": name, "method": "TEXT_QUERY_METHOD_EQUALS"}}},
	}
	response := struct {
		Result []Application `json:"result"`
	}{}
	path := "/management/v1/projects/" + url.PathEscape(projectID) + "/apps/_search"
	if err := c.request(ctx, http.MethodPost, path, organizationID, request, &response); err != nil {
		return Application{}, false, err
	}
	if len(response.Result) == 0 {
		return Application{}, false, nil
	}
	if len(response.Result) > 1 {
		return Application{}, false, fmt.Errorf("multiple Zitadel applications named %q", name)
	}
	return response.Result[0], true, nil
}

func (c *Client) CreateApplication(ctx context.Context, organization, projectID string, input ApplicationInput) (Application, error) {
	organizationID, err := c.organizationID(ctx, organization)
	if err != nil {
		return Application{}, err
	}
	request := map[string]any{
		"projectId":              projectID,
		"name":                   input.DisplayName,
		"redirectUris":           input.RedirectURIs,
		"postLogoutRedirectUris": input.PostLogoutRedirectURIs,
		"responseTypes":          []string{"OIDC_RESPONSE_TYPE_CODE"},
		"grantTypes":             []string{"OIDC_GRANT_TYPE_AUTHORIZATION_CODE"},
		"appType":                oidcAppType(input.AppType),
		"authMethodType":         "OIDC_AUTH_METHOD_TYPE_NONE",
		"version":                "OIDC_VERSION_1_0",
		"accessTokenType":        "OIDC_TOKEN_TYPE_JWT",
	}
	response := struct {
		AppID    string `json:"appId"`
		ClientID string `json:"clientId"`
	}{}
	path := "/management/v1/projects/" + url.PathEscape(projectID) + "/apps/oidc"
	if err := c.request(ctx, http.MethodPost, path, organizationID, request, &response); err != nil {
		return Application{}, err
	}
	if response.AppID == "" || response.ClientID == "" {
		return Application{}, errors.New("Zitadel create application response is missing an identifier")
	}
	return Application{ID: response.AppID, ClientID: response.ClientID, Name: input.DisplayName}, nil
}

func oidcAppType(value string) string {
	if value == "native" {
		return "OIDC_APP_TYPE_NATIVE"
	}
	return "OIDC_APP_TYPE_USER_AGENT"
}

func (c *Client) organizationID(ctx context.Context, name string) (string, error) {
	request := map[string]any{
		"query": map[string]any{"limit": 2},
		"queries": []map[string]any{{
			"nameQuery": map[string]string{"name": name, "method": "TEXT_QUERY_METHOD_EQUALS"},
		}},
	}
	response := struct {
		Result []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"result"`
	}{}
	if err := c.request(ctx, http.MethodPost, "/v2/organizations/_search", "", request, &response); err != nil {
		return "", fmt.Errorf("lookup Zitadel organization: %w", err)
	}
	if len(response.Result) == 0 {
		return "", fmt.Errorf("Zitadel organization %q not found", name)
	}
	if len(response.Result) > 1 {
		return "", fmt.Errorf("multiple Zitadel organizations named %q", name)
	}
	if response.Result[0].ID == "" {
		return "", fmt.Errorf("Zitadel organization %q has no id", name)
	}
	return response.Result[0].ID, nil
}

func (c *Client) request(ctx context.Context, method, path, organization string, payload any, response any) error {
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("encode Zitadel request: %w", err)
		}
		body = bytes.NewReader(encoded)
	}

	token, err := c.tokenSource()
	if err != nil {
		return fmt.Errorf("read Zitadel management token: %w", err)
	}
	if strings.TrimSpace(token) == "" {
		return errors.New("Zitadel management token is empty")
	}

	endpoint := c.baseURL.ResolveReference(&url.URL{Path: path})
	req, err := http.NewRequestWithContext(ctx, method, endpoint.String(), body)
	if err != nil {
		return fmt.Errorf("build Zitadel request: %w", err)
	}
	req.Host = c.host
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token))
	if organization != "" {
		req.Header.Set("X-Zitadel-Orgid", organization)
	}
	req.Header.Set("Content-Type", "application/json")

	res, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("call Zitadel management API: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode < http.StatusOK || res.StatusCode >= http.StatusMultipleChoices {
		_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 4096))
		return &statusError{code: res.StatusCode}
	}
	if response == nil || res.StatusCode == http.StatusNoContent {
		return nil
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(response); err != nil {
		return fmt.Errorf("decode Zitadel response: %w", err)
	}
	return nil
}

type statusError struct {
	code int
}

func (e *statusError) Error() string {
	return fmt.Sprintf("Zitadel management API returned HTTP %d", e.code)
}
