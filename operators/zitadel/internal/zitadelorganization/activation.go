package zitadelorganization

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

	identity "github.com/tesserix/devai-sandbox-operator/operators/zitadel/api/v1alpha1"
)

type API struct {
	base  string
	token func() (string, error)
	http  *http.Client
}

func NewAPI(base string, token func() (string, error)) (*API, error) {
	u, e := url.Parse(base)
	if e != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || token == nil || (u.Scheme != "https" && u.Scheme != "http") {
		return nil, errors.New("invalid onboarding API configuration")
	}
	return &API{base: strings.TrimRight(base, "/"), token: token, http: &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}
func (a *API) Complete(ctx context.Context, spec identity.ZitadelOrganizationSpec, organizationID, projectID string) error {
	token, e := a.token()
	if e != nil || strings.TrimSpace(token) == "" {
		return errors.New("onboarding workload credential unavailable")
	}
	payload := struct {
		Product            string `json:"product"`
		TenantID           string `json:"tenantId"`
		OrganizationID     string `json:"organizationId"`
		ProjectID          string `json:"projectId"`
		DisplayName        string `json:"displayName"`
		InitialAdminUserID string `json:"initialAdminUserId"`
	}{spec.Product, spec.TenantID, organizationID, projectID, spec.DisplayName, spec.InitialAdminUserID}
	raw, e := json.Marshal(payload)
	if e != nil {
		return e
	}
	req, e := http.NewRequestWithContext(ctx, http.MethodPost, a.base+"/internal/v1/hospital-enrolments", bytes.NewReader(raw))
	if e != nil {
		return e
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token))
	res, e := a.http.Do(req)
	if e != nil {
		return errors.New("onboarding API unavailable")
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("onboarding API returned status %d", res.StatusCode)
	}
	var result struct {
		TenantID       string `json:"tenantId"`
		OrganizationID string `json:"organizationId"`
		State          string `json:"state"`
	}
	if json.NewDecoder(io.LimitReader(res.Body, 8192)).Decode(&result) != nil || result.TenantID != spec.TenantID || result.OrganizationID != organizationID || result.State != "complete" {
		return errors.New("onboarding API returned an incomplete or mismatched tenant")
	}
	return nil
}
