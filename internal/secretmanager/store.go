package secretmanager

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
)

var secretIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,255}$`)

type Store struct {
	baseURL    *url.URL
	projectID  string
	httpClient *http.Client
}

func NewStore(baseURL, projectID string, httpClient *http.Client) (*Store, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse Secret Manager URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, errors.New("Secret Manager URL must use http or https")
	}
	if parsed.Host == "" {
		return nil, errors.New("Secret Manager URL must include a host")
	}
	if projectID == "" {
		return nil, errors.New("GCP project id is required")
	}
	if httpClient == nil {
		return nil, errors.New("Secret Manager HTTP client is required")
	}
	return &Store{baseURL: parsed, projectID: projectID, httpClient: httpClient}, nil
}

func (s *Store) Ensure(ctx context.Context, secretID, value string) error {
	if !secretIDPattern.MatchString(secretID) {
		return fmt.Errorf("invalid Secret Manager secret id %q", secretID)
	}
	if value == "" {
		return errors.New("refusing to store an empty Secret Manager value")
	}

	current, found, err := s.Latest(ctx, secretID)
	if err != nil {
		return err
	}
	if found && current == value {
		return nil
	}
	if !found {
		if err := s.create(ctx, secretID); err != nil {
			var status *StatusError
			if !errors.As(err, &status) || status.Code != http.StatusConflict {
				return err
			}
		}
	}
	if err := s.addVersion(ctx, secretID, value); err != nil {
		return err
	}
	return nil
}

// Latest returns the newest enabled version, or found=false when the secret does not exist.
func (s *Store) Latest(ctx context.Context, secretID string) (string, bool, error) {
	if !secretIDPattern.MatchString(secretID) {
		return "", false, fmt.Errorf("invalid Secret Manager secret id %q", secretID)
	}
	path := fmt.Sprintf("/v1/projects/%s/secrets/%s/versions/latest:access", url.PathEscape(s.projectID), url.PathEscape(secretID))
	var response struct {
		Payload struct {
			Data string `json:"data"`
		} `json:"payload"`
	}
	err := s.request(ctx, http.MethodGet, path, nil, &response)
	if err != nil {
		var status *StatusError
		if errors.As(err, &status) && status.Code == http.StatusNotFound {
			return "", false, nil
		}
		return "", false, fmt.Errorf("access latest Secret Manager version: %w", err)
	}
	decoded, err := base64.StdEncoding.DecodeString(response.Payload.Data)
	if err != nil {
		return "", false, errors.New("decode latest Secret Manager version")
	}
	return string(decoded), true, nil
}

func (s *Store) create(ctx context.Context, secretID string) error {
	path := fmt.Sprintf("/v1/projects/%s/secrets?secretId=%s", url.PathEscape(s.projectID), url.QueryEscape(secretID))
	payload := map[string]any{"replication": map[string]any{"automatic": map[string]any{}}}
	if err := s.request(ctx, http.MethodPost, path, payload, nil); err != nil {
		return fmt.Errorf("create Secret Manager secret: %w", err)
	}
	return nil
}

func (s *Store) addVersion(ctx context.Context, secretID, value string) error {
	path := fmt.Sprintf("/v1/projects/%s/secrets/%s:addVersion", url.PathEscape(s.projectID), url.PathEscape(secretID))
	payload := map[string]any{"payload": map[string]string{"data": base64.StdEncoding.EncodeToString([]byte(value))}}
	if err := s.request(ctx, http.MethodPost, path, payload, nil); err != nil {
		return fmt.Errorf("add Secret Manager version: %w", err)
	}
	return nil
}

func (s *Store) request(ctx context.Context, method, path string, payload, response any) error {
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("encode Secret Manager request: %w", err)
		}
		body = bytes.NewReader(encoded)
	}
	endpoint := s.baseURL.ResolveReference(&url.URL{Path: path})
	if parsed, err := url.Parse(path); err == nil {
		endpoint = s.baseURL.ResolveReference(parsed)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint.String(), body)
	if err != nil {
		return fmt.Errorf("build Secret Manager request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("call Secret Manager API: %w", err)
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
		return fmt.Errorf("decode Secret Manager response: %w", err)
	}
	return nil
}

type StatusError struct {
	Code int
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("Secret Manager API returned HTTP %d", e.Code)
}

func (e *StatusError) Permanent() bool {
	return e.Code >= 400 && e.Code < 500 && e.Code != http.StatusConflict && e.Code != http.StatusTooManyRequests
}
