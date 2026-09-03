package langfuseapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// CredentialsSource returns an organization-scoped Langfuse key pair on every call.
type CredentialsSource func() (publicKey, secretKey string, err error)

type Project struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type APIKey struct {
	ID        string `json:"id"`
	PublicKey string `json:"publicKey"`
	SecretKey string `json:"secretKey,omitempty"`
	Note      string `json:"note,omitempty"`
}

type Client struct {
	baseURL     *url.URL
	httpClient  *http.Client
	credentials CredentialsSource
}

func NewClient(baseURL string, httpClient *http.Client, credentials CredentialsSource) (*Client, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse Langfuse URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, errors.New("Langfuse URL must use http or https")
	}
	if parsed.Host == "" {
		return nil, errors.New("Langfuse URL must include a host")
	}
	if httpClient == nil {
		return nil, errors.New("Langfuse HTTP client is required")
	}
	if credentials == nil {
		return nil, errors.New("Langfuse credentials source is required")
	}
	return &Client{baseURL: parsed, httpClient: httpClient, credentials: credentials}, nil
}

// EnsureProject adopts the recorded project, then a project with the same name, and creates one otherwise.
func (c *Client) EnsureProject(ctx context.Context, recordedID, name string, metadata map[string]string) (Project, error) {
	var listed struct {
		Data []Project `json:"data"`
	}
	if err := c.request(ctx, http.MethodGet, "/api/public/projects", nil, &listed); err != nil {
		return Project{}, fmt.Errorf("list Langfuse projects: %w", err)
	}
	for _, candidate := range listed.Data {
		if recordedID != "" && candidate.ID == recordedID {
			return candidate, nil
		}
	}
	for _, candidate := range listed.Data {
		if candidate.Name == name {
			return candidate, nil
		}
	}
	payload := map[string]any{"name": name, "retention": 0, "metadata": metadata}
	var created Project
	if err := c.request(ctx, http.MethodPost, "/api/public/projects", payload, &created); err != nil {
		return Project{}, fmt.Errorf("create Langfuse project: %w", err)
	}
	if created.ID == "" {
		return Project{}, errors.New("Langfuse create project response omitted id")
	}
	return created, nil
}

func (c *Client) ListAPIKeys(ctx context.Context, projectID string) ([]APIKey, error) {
	var response struct {
		APIKeys []APIKey `json:"apiKeys"`
	}
	path := "/api/public/projects/" + url.PathEscape(projectID) + "/apiKeys"
	if err := c.request(ctx, http.MethodGet, path, nil, &response); err != nil {
		return nil, fmt.Errorf("list Langfuse API keys: %w", err)
	}
	return response.APIKeys, nil
}

// CreateAPIKey returns the only copy of the secret key Langfuse ever exposes.
func (c *Client) CreateAPIKey(ctx context.Context, projectID, note string) (APIKey, error) {
	var created APIKey
	path := "/api/public/projects/" + url.PathEscape(projectID) + "/apiKeys"
	if err := c.request(ctx, http.MethodPost, path, map[string]string{"note": note}, &created); err != nil {
		return APIKey{}, fmt.Errorf("create Langfuse API key: %w", err)
	}
	if created.PublicKey == "" || created.SecretKey == "" {
		return APIKey{}, errors.New("Langfuse create API key response omitted a key")
	}
	return created, nil
}

func (c *Client) request(ctx context.Context, method, path string, payload, response any) error {
	publicKey, secretKey, err := c.credentials()
	if err != nil {
		return err
	}
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("encode Langfuse request: %w", err)
		}
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL.ResolveReference(&url.URL{Path: path}).String(), body)
	if err != nil {
		return fmt.Errorf("build Langfuse request: %w", err)
	}
	req.SetBasicAuth(publicKey, secretKey)
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("call Langfuse API: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode < http.StatusOK || res.StatusCode >= http.StatusMultipleChoices {
		_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 4096))
		return &StatusError{Code: res.StatusCode}
	}
	if response == nil {
		_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 4096))
		return nil
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(response); err != nil {
		return fmt.Errorf("decode Langfuse response: %w", err)
	}
	return nil
}

type StatusError struct {
	Code int
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("Langfuse API returned HTTP %d", e.Code)
}

func (e *StatusError) Permanent() bool {
	return e.Code >= 400 && e.Code < 500 && e.Code != http.StatusConflict && e.Code != http.StatusTooManyRequests
}
