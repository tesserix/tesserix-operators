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
	// ProjectRoleCheck mirrors Zitadel's flag: when true, only users holding
	// a role grant in the project can authenticate to its applications.
	ProjectRoleCheck bool `json:"projectRoleCheck"`
}

// UserGrant is a user's role membership in a project.
type UserGrant struct {
	ID       string   `json:"id"`
	UserID   string   `json:"userId"`
	RoleKeys []string `json:"roleKeys"`
}

type ProjectInput struct {
	DisplayName string
}

type Application struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	ClientID   string     `json:"clientId"`
	OIDCConfig OIDCConfig `json:"oidcConfig"`
}

// OIDCConfig carries the subset of the remote OIDC configuration the operator
// manages for drift detection.
type OIDCConfig struct {
	// IDTokenUserinfoAssertion makes Zitadel embed userinfo (email, profile)
	// in the id_token. The auth BFF requires it: both the web callback and the
	// mobile auto-login read the email claim straight from the id_token.
	IDTokenUserinfoAssertion bool `json:"idTokenUserinfoAssertion"`
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
		// Without this Zitadel omits email/profile from the id_token and every
		// BFF sign-in fails on the user upsert (email is a required field).
		"idTokenUserinfoAssertion": true,
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

// UpdateOIDCConfig replaces the OIDC configuration of an existing application
// with the desired one. Used to heal applications created by operator builds
// that predate idTokenUserinfoAssertion; call it only when drift is detected —
// Zitadel rejects a no-op update with an error.
func (c *Client) UpdateOIDCConfig(ctx context.Context, organization, projectID, appID string, input ApplicationInput) error {
	organizationID, err := c.organizationID(ctx, organization)
	if err != nil {
		return err
	}
	request := map[string]any{
		"redirectUris":             input.RedirectURIs,
		"postLogoutRedirectUris":   input.PostLogoutRedirectURIs,
		"responseTypes":            []string{"OIDC_RESPONSE_TYPE_CODE"},
		"grantTypes":               []string{"OIDC_GRANT_TYPE_AUTHORIZATION_CODE"},
		"appType":                  oidcAppType(input.AppType),
		"authMethodType":           "OIDC_AUTH_METHOD_TYPE_NONE",
		"accessTokenType":          "OIDC_TOKEN_TYPE_JWT",
		"idTokenUserinfoAssertion": true,
	}
	path := "/management/v1/projects/" + url.PathEscape(projectID) + "/apps/" + url.PathEscape(appID) + "/oidc_config"
	return c.request(ctx, http.MethodPut, path, organizationID, request, nil)
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

// UpdateProject sets the project's authentication gate. Callers must drift-guard:
// Zitadel rejects updates that change nothing.
func (c *Client) UpdateProject(ctx context.Context, organization, projectID, name string, roleCheck bool) error {
	organizationID, err := c.organizationID(ctx, organization)
	if err != nil {
		return err
	}
	request := map[string]any{
		"name":                 name,
		"projectRoleAssertion": true,
		"projectRoleCheck":     roleCheck,
		"hasProjectCheck":      false,
	}
	return c.request(ctx, http.MethodPut, "/management/v1/projects/"+url.PathEscape(projectID), organizationID, request, nil)
}

func (c *Client) ListRoles(ctx context.Context, organization, projectID string) ([]string, error) {
	organizationID, err := c.organizationID(ctx, organization)
	if err != nil {
		return nil, err
	}
	response := struct {
		Result []struct {
			Key string `json:"key"`
		} `json:"result"`
	}{}
	if err := c.request(ctx, http.MethodPost, "/management/v1/projects/"+url.PathEscape(projectID)+"/roles/_search", organizationID, map[string]any{"query": map[string]any{"limit": 100}}, &response); err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(response.Result))
	for _, role := range response.Result {
		keys = append(keys, role.Key)
	}
	return keys, nil
}

func (c *Client) AddRole(ctx context.Context, organization, projectID, key string) error {
	organizationID, err := c.organizationID(ctx, organization)
	if err != nil {
		return err
	}
	request := map[string]any{"roleKey": key, "displayName": key}
	return c.request(ctx, http.MethodPost, "/management/v1/projects/"+url.PathEscape(projectID)+"/roles", organizationID, request, nil)
}

func (c *Client) FindUserByEmail(ctx context.Context, organization, email string) (string, bool, error) {
	organizationID, err := c.organizationID(ctx, organization)
	if err != nil {
		return "", false, err
	}
	request := map[string]any{
		"query": map[string]any{"limit": 2},
		"queries": []map[string]any{{
			"emailQuery": map[string]string{"emailAddress": email, "method": "TEXT_QUERY_METHOD_EQUALS_IGNORE_CASE"},
		}},
	}
	response := struct {
		Result []struct {
			ID string `json:"id"`
		} `json:"result"`
	}{}
	if err := c.request(ctx, http.MethodPost, "/management/v1/users/_search", organizationID, request, &response); err != nil {
		return "", false, err
	}
	if len(response.Result) == 0 {
		return "", false, nil
	}
	if len(response.Result) > 1 {
		return "", false, fmt.Errorf("multiple Zitadel users with email %q", email)
	}
	return response.Result[0].ID, true, nil
}

func (c *Client) ListGrants(ctx context.Context, organization, projectID string) ([]UserGrant, error) {
	organizationID, err := c.organizationID(ctx, organization)
	if err != nil {
		return nil, err
	}
	request := map[string]any{
		"query": map[string]any{"limit": 200},
		"queries": []map[string]any{{
			"projectIdQuery": map[string]string{"projectId": projectID},
		}},
	}
	response := struct {
		Result []UserGrant `json:"result"`
	}{}
	if err := c.request(ctx, http.MethodPost, "/management/v1/users/grants/_search", organizationID, request, &response); err != nil {
		return nil, err
	}
	return response.Result, nil
}

func (c *Client) AddGrant(ctx context.Context, organization, userID, projectID string, roles []string) error {
	organizationID, err := c.organizationID(ctx, organization)
	if err != nil {
		return err
	}
	request := map[string]any{"projectId": projectID, "roleKeys": roles}
	return c.request(ctx, http.MethodPost, "/management/v1/users/"+url.PathEscape(userID)+"/grants", organizationID, request, nil)
}

func (c *Client) UpdateGrant(ctx context.Context, organization, userID, grantID string, roles []string) error {
	organizationID, err := c.organizationID(ctx, organization)
	if err != nil {
		return err
	}
	request := map[string]any{"roleKeys": roles}
	return c.request(ctx, http.MethodPut, "/management/v1/users/"+url.PathEscape(userID)+"/grants/"+url.PathEscape(grantID), organizationID, request, nil)
}

func (c *Client) RemoveGrant(ctx context.Context, organization, userID, grantID string) error {
	organizationID, err := c.organizationID(ctx, organization)
	if err != nil {
		return err
	}
	return c.request(ctx, http.MethodDelete, "/management/v1/users/"+url.PathEscape(userID)+"/grants/"+url.PathEscape(grantID), organizationID, nil, nil)
}
