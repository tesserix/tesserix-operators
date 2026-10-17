package zitadelapi

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"time"

	"github.com/google/uuid"
	identity "github.com/tesserix/devai-sandbox-operator/operators/zitadel/api/v1alpha1"
)

var organizationSourceID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,200}$`)

// EnsureOrganization never deletes an org, moves a user, or replaces a tenant binding.
func (c *Client) EnsureOrganization(ctx context.Context, spec identity.ZitadelOrganizationSpec, owner, projectID string) (string, error) {
	tenant, e := uuid.Parse(spec.TenantID)
	if e != nil || tenant == uuid.Nil || tenant.String() != spec.TenantID || !organizationSourceID.MatchString(spec.Product) || spec.DisplayName == "" || len(spec.DisplayName) > 200 {
		return "", errors.New("invalid organization identity")
	}
	if !organizationSourceID.MatchString(spec.InitialAdminUserID) {
		return "", errors.New("initial administrator must be an existing Zitadel human")
	}
	var administrator struct {
		User struct {
			UserID string `json:"userId"`
			State  string `json:"state"`
			Human  *struct {
				Email struct {
					IsVerified bool `json:"isVerified"`
				} `json:"email"`
			} `json:"human"`
		} `json:"user"`
	}
	if err := c.request(ctx, http.MethodGet, "/v2/users/"+url.PathEscape(spec.InitialAdminUserID), "", nil, &administrator); err != nil {
		return "", err
	}
	if administrator.User.UserID != spec.InitialAdminUserID || administrator.User.State != "USER_STATE_ACTIVE" || administrator.User.Human == nil || !administrator.User.Human.Email.IsVerified {
		return "", errors.New("initial administrator must be an active verified human")
	}
	id := spec.OrganizationID
	if id == "" {
		id = spec.Product + "-" + spec.TenantID
	}
	if !organizationSourceID.MatchString(id) {
		return "", errors.New("invalid organization id")
	}
	var found struct {
		Org struct {
			ID    string `json:"id"`
			Name  string `json:"name"`
			State string `json:"state"`
		} `json:"org"`
	}
	err := c.request(ctx, http.MethodGet, "/admin/v1/orgs/"+url.PathEscape(id), "", nil, &found)
	if err != nil {
		var status *statusError
		if !errors.As(err, &status) || status.code != http.StatusNotFound || spec.OrganizationID != "" {
			return "", err
		}
		var created struct {
			ID string `json:"organizationId"`
		}
		if err = c.request(ctx, http.MethodPost, "/v2/organizations", "", map[string]any{"organizationId": id, "name": spec.DisplayName, "admins": []map[string]any{{"userId": spec.InitialAdminUserID, "roles": []string{"ORG_OWNER"}}}}, &created); err != nil {
			return "", err
		}
		if created.ID != id {
			return "", errors.New("created organization id does not match deterministic binding")
		}
		// A replay reads the same deterministic ID, including after a lost create response.
		if err = c.request(ctx, http.MethodGet, "/admin/v1/orgs/"+url.PathEscape(id), "", nil, &found); err != nil {
			return "", err
		}
	}
	if found.Org.ID != id || found.Org.Name != spec.DisplayName || found.Org.State != "ORG_STATE_ACTIVE" {
		return "", errors.New("organization name, state or identity does not match")
	}
	var metadata struct {
		Metadata struct {
			Key   string `json:"key"`
			Value string `json:"value"`
		} `json:"metadata"`
	}
	err = c.request(ctx, http.MethodGet, "/management/v1/metadata/tenant_id", id, nil, &metadata)
	if err != nil {
		var status *statusError
		if !errors.As(err, &status) || status.code != http.StatusNotFound || spec.OrganizationID != "" {
			return "", err
		}
		if err = c.request(ctx, http.MethodPost, "/management/v1/metadata/tenant_id", id, map[string]string{"value": base64.StdEncoding.EncodeToString([]byte(spec.TenantID))}, nil); err != nil {
			return "", err
		}
	} else {
		value, e := base64.StdEncoding.DecodeString(metadata.Metadata.Value)
		if e != nil || metadata.Metadata.Key != "tenant_id" || string(value) != spec.TenantID {
			return "", errors.New("existing organization is bound to a different tenant")
		}
	}
	project, ok, err := c.FindByID(ctx, owner, projectID)
	if err != nil {
		return "", err
	}
	if !ok || project.ID != projectID {
		return "", errors.New("product project not found")
	}
	if !spec.PreserveLoginPolicy {
		if err = c.ensureHospitalLogin(ctx, id); err != nil {
			return "", err
		}
	}
	ownerID, err := c.organizationID(ctx, owner)
	if err != nil {
		return "", err
	}
	type grant struct {
		ID           string `json:"grantId"`
		GrantedOrgID string `json:"grantedOrgId"`
		ProjectID    string `json:"projectId"`
		State        string `json:"state"`
	}
	grants, err := listAll[grant](ctx, c, "/management/v1/projectgrants/_search", ownerID, map[string]any{"queries": []map[string]any{{"projectIdQuery": map[string]string{"projectId": projectID}}, {"grantedOrgIdQuery": map[string]string{"grantedOrgId": id}}}}, 100)
	if err != nil {
		return "", err
	}
	if len(grants) > 1 {
		return "", errors.New("ambiguous hospital project grant")
	}
	if len(grants) == 1 {
		g := grants[0]
		if g.ID == "" || g.GrantedOrgID != id || g.ProjectID != projectID || g.State != "PROJECT_GRANT_STATE_ACTIVE" {
			return "", errors.New("hospital project grant is not active or has wrong binding")
		}
		return id, nil
	}
	if err = c.request(ctx, http.MethodPost, "/management/v1/projects/"+url.PathEscape(projectID)+"/grants", ownerID, map[string]any{"grantedOrgId": id, "roleKeys": []string{}}, nil); err != nil {
		return "", err
	}
	return id, nil
}

func (c *Client) ensureHospitalLogin(ctx context.Context, orgID string) error {
	var current struct {
		IsDefault bool           `json:"isDefault"`
		Policy    map[string]any `json:"policy"`
	}
	if err := c.request(ctx, http.MethodGet, "/management/v1/policies/login", orgID, nil, &current); err != nil {
		return err
	}
	if current.Policy == nil {
		return errors.New("hospital login policy is missing")
	}
	policy := map[string]any{}
	for _, key := range []string{"allowUsernamePassword", "allowRegister", "allowExternalIdp", "forceMfa", "passwordlessType", "hidePasswordReset", "ignoreUnknownUsernames", "defaultRedirectUri", "passwordCheckLifetime", "externalLoginCheckLifetime", "mfaInitSkipLifetime", "secondFactorCheckLifetime", "multiFactorCheckLifetime", "allowDomainDiscovery", "disableLoginWithEmail", "disableLoginWithPhone", "forceMfaLocalOnly"} {
		if v, ok := current.Policy[key]; ok {
			policy[key] = v
		}
	}
	wanted := map[string]any{"forceMfa": true, "allowRegister": false, "allowDomainDiscovery": false, "forceMfaLocalOnly": false, "ignoreUnknownUsernames": true, "mfaInitSkipLifetime": "0s"}
	dirty := current.IsDefault || current.Policy["isDefault"] == true
	for key, value := range wanted {
		actual := policy[key]
		if actual == nil {
			if _, ok := value.(bool); ok {
				actual = false
			} else if value == "0s" {
				actual = "0s"
			}
		}
		if actual != value {
			dirty = true
		}
		policy[key] = value
	}
	for _, key := range []string{"passwordCheckLifetime", "externalLoginCheckLifetime", "secondFactorCheckLifetime", "multiFactorCheckLifetime"} {
		raw, _ := policy[key].(string)
		if raw == "" {
			raw = "0s"
		}
		duration, err := time.ParseDuration(raw)
		if err != nil {
			return errors.New("invalid hospital login lifetime")
		}
		if duration > 12*time.Hour {
			policy[key] = "43200s"
			dirty = true
		}
	}
	if !dirty {
		return nil
	}
	method := http.MethodPut
	if current.IsDefault {
		method = http.MethodPost
		policy["secondFactors"] = current.Policy["secondFactors"]
		policy["multiFactors"] = current.Policy["multiFactors"]
	}
	return c.request(ctx, method, "/management/v1/policies/login", orgID, policy, nil)
}
