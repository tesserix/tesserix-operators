package openbaostore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

type Secrets interface {
	Latest(context.Context, string) (string, bool, error)
	Ensure(context.Context, string, string) error
}

type Store struct {
	address, role string
	jwt           func() (string, error)
	paths         map[string]string
	fallback      Secrets
	http          *http.Client
}

var pathPattern = regexp.MustCompile(`^[a-z0-9-]+/app/[a-z0-9-]+$`)

func New(address, role string, jwt func() (string, error), paths map[string]string, fallback Secrets, httpClient *http.Client) (*Store, error) {
	u, err := url.Parse(address)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || strings.Trim(u.Path, "/") != "" {
		return nil, errors.New("invalid OpenBao address")
	}
	if role == "" || jwt == nil || fallback == nil || httpClient == nil {
		return nil, errors.New("OpenBao role, JWT source, fallback and HTTP client required")
	}
	copied := make(map[string]string, len(paths))
	for name, path := range paths {
		if name == "" || !pathPattern.MatchString(path) {
			return nil, errors.New("invalid OpenBao mapping")
		}
		copied[name] = path
	}
	client := *httpClient
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Store{strings.TrimRight(address, "/"), role, jwt, copied, fallback, &client}, nil
}

func (s *Store) Latest(ctx context.Context, name string) (string, bool, error) {
	path, mapped := s.paths[name]
	if !mapped {
		return s.fallback.Latest(ctx, name)
	}
	token, err := s.login(ctx)
	if err != nil {
		return "", false, err
	}
	defer s.revoke(token)
	value, version, err := s.read(ctx, token, path)
	return value, version > 0, err
}

func (s *Store) Ensure(ctx context.Context, name, value string) error {
	path, mapped := s.paths[name]
	if !mapped {
		return s.fallback.Ensure(ctx, name, value)
	}
	if value == "" {
		return errors.New("refusing empty OpenBao credential")
	}
	token, err := s.login(ctx)
	if err != nil {
		return err
	}
	defer s.revoke(token)
	current, version, err := s.read(ctx, token, path)
	if err != nil {
		return err
	}
	if version > 0 && current == value {
		return nil
	}
	_, err = s.request(ctx, "POST", "kv/data/"+path, token, map[string]any{"options": map[string]int{"cas": version}, "data": map[string]string{"value": value}}, nil)
	return err
}

func (s *Store) login(ctx context.Context) (string, error) {
	jwt, err := s.jwt()
	if err != nil || strings.TrimSpace(jwt) == "" {
		return "", errors.New("read OpenBao authentication JWT")
	}
	var response struct {
		Auth struct {
			Token string `json:"client_token"`
		}
	}
	_, err = s.request(ctx, "POST", "auth/kubernetes/login", "", map[string]string{"role": s.role, "jwt": strings.TrimSpace(jwt)}, &response)
	if err != nil {
		return "", err
	}
	if response.Auth.Token == "" {
		return "", errors.New("OpenBao login returned no token")
	}
	return response.Auth.Token, nil
}

func (s *Store) revoke(token string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// Tokens also have a short server-enforced TTL if revocation cannot reach OpenBao.
	_, _ = s.request(ctx, "POST", "auth/token/revoke-self", token, map[string]any{}, nil)
}

func (s *Store) read(ctx context.Context, token, path string) (string, int, error) {
	var response struct {
		Data struct {
			Data     map[string]string
			Metadata struct {
				Version      int
				Destroyed    bool
				DeletionTime string `json:"deletion_time"`
			}
		}
	}
	status, err := s.request(ctx, "GET", "kv/data/"+path, token, nil, &response)
	if status == 404 {
		return "", 0, nil
	}
	if err != nil {
		return "", 0, err
	}
	if response.Data.Metadata.Version < 1 || response.Data.Metadata.Destroyed || response.Data.Metadata.DeletionTime != "" || response.Data.Data["value"] == "" {
		return "", 0, errors.New("OpenBao credential has no usable value")
	}
	return response.Data.Data["value"], response.Data.Metadata.Version, nil
}

func (s *Store) request(ctx context.Context, method, path, token string, payload, response any) (int, error) {
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return 0, errors.New("encode OpenBao request")
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, s.address+"/v1/"+path, body)
	if err != nil {
		return 0, errors.New("build OpenBao request")
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("X-Vault-Token", token)
	}
	res, err := s.http.Do(req)
	if err != nil {
		return 0, errors.New("OpenBao request failed")
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return res.StatusCode, fmt.Errorf("OpenBao returned HTTP %d", res.StatusCode)
	}
	if response != nil {
		if err := json.NewDecoder(io.LimitReader(res.Body, 1024*1024)).Decode(response); err != nil {
			return res.StatusCode, errors.New("decode OpenBao response")
		}
	}
	return res.StatusCode, nil
}
