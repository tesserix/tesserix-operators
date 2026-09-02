package openpanelapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
)

type CredentialsSource func() (clientID, clientSecret string, err error)

type ProjectInput struct {
	Name   string   `json:"name"`
	Domain string   `json:"domain"`
	CORS   []string `json:"cors"`
	Types  []string `json:"types"`
}

type Result struct {
	ProjectID string
	ClientID  string
}

type project struct {
	ID     string   `json:"id"`
	Name   string   `json:"name"`
	Domain string   `json:"domain"`
	CORS   []string `json:"cors"`
	Types  []string `json:"types"`
	Client *client  `json:"client,omitempty"`
}

type client struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Type      string `json:"type"`
	ProjectID string `json:"projectId"`
}

type Client struct {
	baseURL     *url.URL
	httpClient  *http.Client
	credentials CredentialsSource
}

func NewClient(baseURL string, httpClient *http.Client, credentials CredentialsSource) (*Client, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse OpenPanel API URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, errors.New("OpenPanel API URL must use http or https")
	}
	if parsed.Host == "" {
		return nil, errors.New("OpenPanel API URL must include a host")
	}
	if httpClient == nil {
		return nil, errors.New("OpenPanel HTTP client is required")
	}
	if credentials == nil {
		return nil, errors.New("OpenPanel credentials source is required")
	}
	return &Client{baseURL: parsed, httpClient: httpClient, credentials: credentials}, nil
}

func (c *Client) EnsureProject(ctx context.Context, recordedID string, input ProjectInput) (Result, error) {
	current, found, err := c.findProject(ctx, recordedID, input.Name)
	if err != nil {
		return Result{}, err
	}
	if !found {
		created, err := c.createProject(ctx, input)
		if err != nil {
			return Result{}, err
		}
		if created.ID == "" || created.Client == nil || created.Client.ID == "" {
			return Result{}, errors.New("OpenPanel create project response omitted project or client id")
		}
		return Result{ProjectID: created.ID, ClientID: created.Client.ID}, nil
	}

	if current.Name != input.Name || current.Domain != input.Domain || !slices.Equal(current.CORS, input.CORS) {
		if err := c.patchProject(ctx, current.ID, input); err != nil {
			return Result{}, err
		}
	}
	writeClient, found, err := c.findWriteClient(ctx, current.ID)
	if err != nil {
		return Result{}, err
	}
	if !found {
		writeClient, err = c.createWriteClient(ctx, current.ID, input.Name)
		if err != nil {
			return Result{}, err
		}
	}
	if writeClient.ID == "" {
		return Result{}, errors.New("OpenPanel write client response omitted id")
	}
	return Result{ProjectID: current.ID, ClientID: writeClient.ID}, nil
}

func (c *Client) findProject(ctx context.Context, recordedID, name string) (project, bool, error) {
	if recordedID != "" {
		var response struct {
			Data project `json:"data"`
		}
		err := c.request(ctx, http.MethodGet, "/manage/projects/"+url.PathEscape(recordedID), nil, &response)
		if err == nil {
			return response.Data, true, nil
		}
		var status *StatusError
		if !errors.As(err, &status) || status.Code != http.StatusNotFound {
			return project{}, false, fmt.Errorf("get OpenPanel project: %w", err)
		}
	}

	var response struct {
		Data []project `json:"data"`
	}
	if err := c.request(ctx, http.MethodGet, "/manage/projects", nil, &response); err != nil {
		return project{}, false, fmt.Errorf("list OpenPanel projects: %w", err)
	}
	for _, candidate := range response.Data {
		if candidate.Name == name {
			return candidate, true, nil
		}
	}
	return project{}, false, nil
}

func (c *Client) createProject(ctx context.Context, input ProjectInput) (project, error) {
	var response struct {
		Data project `json:"data"`
	}
	if err := c.request(ctx, http.MethodPost, "/manage/projects", input, &response); err != nil {
		return project{}, fmt.Errorf("create OpenPanel project: %w", err)
	}
	return response.Data, nil
}

func (c *Client) patchProject(ctx context.Context, projectID string, input ProjectInput) error {
	payload := struct {
		Name   string   `json:"name"`
		Domain string   `json:"domain"`
		CORS   []string `json:"cors"`
	}{Name: input.Name, Domain: input.Domain, CORS: input.CORS}
	if err := c.request(ctx, http.MethodPatch, "/manage/projects/"+url.PathEscape(projectID), payload, nil); err != nil {
		return fmt.Errorf("update OpenPanel project: %w", err)
	}
	return nil
}

func (c *Client) findWriteClient(ctx context.Context, projectID string) (client, bool, error) {
	var response struct {
		Data []client `json:"data"`
	}
	path := "/manage/clients?projectId=" + url.QueryEscape(projectID)
	if err := c.request(ctx, http.MethodGet, path, nil, &response); err != nil {
		return client{}, false, fmt.Errorf("list OpenPanel clients: %w", err)
	}
	for _, candidate := range response.Data {
		if candidate.Type == "write" && candidate.ProjectID == projectID {
			return candidate, true, nil
		}
	}
	return client{}, false, nil
}

func (c *Client) createWriteClient(ctx context.Context, projectID, projectName string) (client, error) {
	payload := struct {
		Name      string `json:"name"`
		ProjectID string `json:"projectId"`
		Type      string `json:"type"`
	}{Name: projectName + " write", ProjectID: projectID, Type: "write"}
	var response struct {
		Data client `json:"data"`
	}
	if err := c.request(ctx, http.MethodPost, "/manage/clients", payload, &response); err != nil {
		return client{}, fmt.Errorf("create OpenPanel write client: %w", err)
	}
	return response.Data, nil
}

func (c *Client) request(ctx context.Context, method, path string, payload, response any) error {
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("encode OpenPanel request: %w", err)
		}
		body = bytes.NewReader(encoded)
	}
	clientID, clientSecret, err := c.credentials()
	if err != nil {
		return fmt.Errorf("read OpenPanel management credentials: %w", err)
	}
	if strings.TrimSpace(clientID) == "" || strings.TrimSpace(clientSecret) == "" {
		return errors.New("OpenPanel management credentials are empty")
	}
	endpoint := c.baseURL.ResolveReference(&url.URL{Path: path})
	if strings.Contains(path, "?") {
		parts := strings.SplitN(path, "?", 2)
		endpoint = c.baseURL.ResolveReference(&url.URL{Path: parts[0], RawQuery: parts[1]})
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint.String(), body)
	if err != nil {
		return fmt.Errorf("build OpenPanel request: %w", err)
	}
	req.Header.Set("openpanel-client-id", strings.TrimSpace(clientID))
	req.Header.Set("openpanel-client-secret", strings.TrimSpace(clientSecret))
	req.Header.Set("Content-Type", "application/json")

	res, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("call OpenPanel management API: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode < http.StatusOK || res.StatusCode >= http.StatusMultipleChoices {
		_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 4096))
		return &StatusError{Code: res.StatusCode}
	}
	if response == nil || res.StatusCode == http.StatusNoContent {
		_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 4096))
		return nil
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(response); err != nil {
		return fmt.Errorf("decode OpenPanel response: %w", err)
	}
	return nil
}

type StatusError struct {
	Code int
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("OpenPanel management API returned HTTP %d", e.Code)
}

func (e *StatusError) Permanent() bool {
	return e.Code >= 400 && e.Code < 500 && e.Code != http.StatusTooManyRequests
}
