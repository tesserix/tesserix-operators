// Package onboardingarchive stores create-only, held customer YAML revisions.
package onboardingarchive

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"regexp"
	"strings"
)

type Store struct {
	bucket, base string
	http         http.Client
}

var bucketName = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{1,220}[a-z0-9]$`)

func New(bucket, base string, httpClient *http.Client) (*Store, error) {
	u, err := url.Parse(base)
	if !bucketName.MatchString(bucket) || err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || httpClient == nil {
		return nil, errors.New("invalid archive configuration")
	}
	if u.Scheme != "https" && (u.Scheme != "http" || u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost") {
		return nil, errors.New("archive endpoint requires HTTPS")
	}
	c := *httpClient
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Store{bucket: bucket, base: strings.TrimRight(base, "/"), http: c}, nil
}

type object struct {
	Generation     string `json:"generation"`
	EventBasedHold bool   `json:"eventBasedHold"`
}

func (s *Store) Put(ctx context.Context, name string, content []byte) (string, error) {
	if len(content) == 0 || len(content) > 16384 || strings.Contains(name, "..") || strings.HasPrefix(name, "/") || !strings.HasSuffix(name, ".yaml") {
		return "", errors.New("invalid archive revision")
	}
	var body bytes.Buffer
	m := multipart.NewWriter(&body)
	h := textproto.MIMEHeader{}
	h.Set("Content-Type", "application/json; charset=UTF-8")
	p, err := m.CreatePart(h)
	if err != nil {
		return "", err
	}
	if err = json.NewEncoder(p).Encode(map[string]any{"name": name, "contentType": "application/yaml", "eventBasedHold": true}); err != nil {
		return "", err
	}
	h = textproto.MIMEHeader{}
	h.Set("Content-Type", "application/yaml")
	p, err = m.CreatePart(h)
	if err != nil {
		return "", err
	}
	if _, err = p.Write(content); err != nil {
		return "", err
	}
	if err = m.Close(); err != nil {
		return "", err
	}
	endpoint := s.base + "/upload/storage/v1/b/" + url.PathEscape(s.bucket) + "/o?uploadType=multipart&ifGenerationMatch=0"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "multipart/related; boundary="+m.Boundary())
	res, err := s.http.Do(req)
	if err != nil {
		return "", errors.New("archive write unavailable")
	}
	defer func() { _ = res.Body.Close() }()
	var attrs object
	switch res.StatusCode {
	case http.StatusPreconditionFailed:
		objectURL := s.base + "/storage/v1/b/" + url.PathEscape(s.bucket) + "/o/" + url.PathEscape(name)
		raw, err := s.get(ctx, objectURL)
		if err != nil {
			return "", err
		}
		if json.Unmarshal(raw, &attrs) != nil {
			return "", errors.New("invalid existing archive metadata")
		}
		raw, err = s.get(ctx, objectURL+"?alt=media&generation="+url.QueryEscape(attrs.Generation))
		if err != nil {
			return "", err
		}
		if !bytes.Equal(raw, content) {
			return "", errors.New("immutable archive revision conflicts")
		}
	case http.StatusOK, http.StatusCreated:
		if err = json.NewDecoder(io.LimitReader(res.Body, 32768)).Decode(&attrs); err != nil {
			return "", errors.New("invalid archive response")
		}
	default:
		return "", fmt.Errorf("archive write status %d", res.StatusCode)
	}
	if attrs.Generation == "" || !attrs.EventBasedHold {
		return "", errors.New("archive revision is not protected by an event-based hold")
	}
	return attrs.Generation, nil
}
func (s *Store) get(ctx context.Context, endpoint string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	res, err := s.http.Do(req)
	if err != nil {
		return nil, errors.New("archive read unavailable")
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("archive read status %d", res.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, 32769))
	if err != nil || len(raw) > 32768 {
		return nil, errors.New("invalid archive read")
	}
	return raw, nil
}
