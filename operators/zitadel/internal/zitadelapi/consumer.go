package zitadelapi

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"regexp"
	"strings"

	identity "github.com/tesserix/devai-sandbox-operator/operators/zitadel/api/v1alpha1"
)

// ConsumerSecrets resolves only operator-reviewed, product-prefixed OpenBao names.
type ConsumerSecrets interface {
	Latest(context.Context, string) (string, bool, error)
}

func (c *Client) UseConsumerSecrets(secrets ConsumerSecrets) { c.consumerSecrets = secrets }

var consumerName = regexp.MustCompile(`^[a-z][a-z0-9-]{1,62}$`)
var consumerColor = regexp.MustCompile(`^#[A-Fa-f0-9]{6}$`)

type consumerCredential struct {
	ClientID     string `json:"clientId"`
	ClientSecret string `json:"clientSecret,omitempty"`
	TeamID       string `json:"teamId,omitempty"`
	KeyID        string `json:"keyId,omitempty"`
	PrivateKey   string `json:"privateKey,omitempty"`
}

// EnsureConsumerOrganization never adopts a shared organization or changes instance defaults.
func (c *Client) EnsureConsumerOrganization(ctx context.Context, binding, name string, spec identity.ConsumerOrganization, previous *identity.ConsumerIdentity) (*identity.ConsumerIdentity, error) {
	parts := strings.Split(binding, "/")
	if len(parts) != 2 || !consumerName.MatchString(parts[0]) || !consumerName.MatchString(parts[1]) || strings.TrimSpace(name) != name || name == "" || strings.EqualFold(name, "TESSERIX") || strings.EqualFold(name, "ZITADEL") || len(name) > 200 || !organizationSourceID.MatchString(spec.InitialAdminUserID) || !consumerColor.MatchString(spec.PrimaryColor) || !consumerColor.MatchString(spec.BackgroundColor) || !consumerColor.MatchString(spec.FontColor) || (len(spec.Providers) == 0 && spec.EmailOTP == nil) || len(spec.Providers) > 3 || (len(spec.Providers) > 0 && c.consumerSecrets == nil) {
		return nil, errors.New("invalid or unconfigured consumer organization")
	}
	orgID := "consumer-" + parts[1]
	if previous != nil && previous.OrganizationID != orgID {
		return nil, errors.New("consumer organization identity changed")
	}
	credentials := map[string]consumerCredential{}
	digests := map[string]string{}
	for _, provider := range spec.Providers {
		if (provider.Kind != "google" && provider.Kind != "apple" && provider.Kind != "facebook") || !strings.HasPrefix(provider.SecretName, parts[1]+"-") {
			return nil, errors.New("unsupported consumer provider or secret binding")
		}
		if _, duplicate := credentials[provider.Kind]; duplicate {
			return nil, errors.New("duplicate consumer provider")
		}
		raw, found, err := c.consumerSecrets.Latest(ctx, provider.SecretName)
		if err != nil || !found || len(raw) > 16000 {
			return nil, errors.New("consumer provider credential unavailable")
		}
		var credential consumerCredential
		decoder := json.NewDecoder(strings.NewReader(raw))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&credential); err != nil || credential.ClientID == "" || len(credential.ClientID) > 200 {
			return nil, errors.New("invalid consumer provider credential")
		}
		if (provider.Kind == "google" || provider.Kind == "facebook") && (credential.ClientSecret == "" || len(credential.ClientSecret) > 200 || credential.PrivateKey != "" || credential.TeamID != "" || credential.KeyID != "") {
			return nil, errors.New("invalid social provider credential")
		}
		if provider.Kind == "apple" {
			key, err := base64.StdEncoding.DecodeString(credential.PrivateKey)
			if err != nil || len(key) == 0 || len(key) > 5000 || len(credential.TeamID) != 10 || len(credential.KeyID) != 10 || credential.ClientSecret != "" {
				return nil, errors.New("invalid Apple credential")
			}
		}
		var trailing any
		if err := decoder.Decode(&trailing); err != io.EOF {
			return nil, errors.New("invalid consumer provider credential")
		}
		credentials[provider.Kind] = credential
		sum := sha256.Sum256([]byte(raw))
		digests[provider.Kind] = hex.EncodeToString(sum[:])
	}
	var admin struct {
		User struct {
			ID    string `json:"userId"`
			State string `json:"state"`
			Human *struct {
				Email struct {
					Verified bool `json:"isVerified"`
				} `json:"email"`
			} `json:"human"`
		} `json:"user"`
	}
	if err := c.request(ctx, http.MethodGet, "/v2/users/"+url.PathEscape(spec.InitialAdminUserID), "", nil, &admin); err != nil {
		return nil, err
	}
	if admin.User.ID != spec.InitialAdminUserID || admin.User.State != "USER_STATE_ACTIVE" || admin.User.Human == nil || !admin.User.Human.Email.Verified {
		return nil, errors.New("consumer administrator must be an active verified human")
	}
	var existing struct {
		Org struct {
			ID    string `json:"id"`
			Name  string `json:"name"`
			State string `json:"state"`
		} `json:"org"`
	}
	created := false
	err := c.request(ctx, http.MethodGet, "/admin/v1/orgs/"+url.PathEscape(orgID), "", nil, &existing)
	if err != nil {
		var status *statusError
		if !errors.As(err, &status) || status.code != http.StatusNotFound {
			return nil, err
		}
		var result struct {
			ID string `json:"organizationId"`
		}
		body := map[string]any{"organizationId": orgID, "name": name, "admins": []map[string]any{{"userId": spec.InitialAdminUserID, "roles": []string{"ORG_OWNER"}}}}
		if err := c.request(ctx, http.MethodPost, "/v2/organizations", "", body, &result); err != nil {
			return nil, err
		}
		if result.ID != orgID {
			return nil, errors.New("created consumer organization identity differs")
		}
		created = true
		if err := c.request(ctx, http.MethodGet, "/admin/v1/orgs/"+url.PathEscape(orgID), "", nil, &existing); err != nil {
			return nil, err
		}
	}
	if existing.Org.ID != orgID || existing.Org.Name != name || existing.Org.State != "ORG_STATE_ACTIVE" {
		return nil, errors.New("consumer organization ownership differs")
	}
	var metadata struct {
		Metadata struct {
			Key   string `json:"key"`
			Value string `json:"value"`
		} `json:"metadata"`
	}
	err = c.request(ctx, http.MethodGet, "/management/v1/metadata/consumer_binding", orgID, nil, &metadata)
	if err != nil {
		var status *statusError
		if !errors.As(err, &status) || status.code != http.StatusNotFound {
			return nil, errors.New("consumer organization binding unavailable; refusing adoption")
		}
		if !created {
			owners, err := listAll[struct {
				UserID string   `json:"userId"`
				Roles  []string `json:"roles"`
			}](ctx, c, "/management/v1/orgs/me/members/_search", orgID, nil, 100)
			if err != nil {
				return nil, err
			}
			owned := false
			for _, member := range owners {
				if member.UserID == spec.InitialAdminUserID {
					for _, role := range member.Roles {
						if role == "ORG_OWNER" {
							owned = true
						}
					}
				}
			}
			if !owned {
				return nil, errors.New("unbound consumer organization has a different administrator")
			}
		}
		if err := c.request(ctx, http.MethodPost, "/management/v1/metadata/consumer_binding", orgID, map[string]string{"value": base64.StdEncoding.EncodeToString([]byte(binding))}, nil); err != nil {
			return nil, err
		}
	} else {
		value, e := base64.StdEncoding.DecodeString(metadata.Metadata.Value)
		if e != nil || metadata.Metadata.Key != "consumer_binding" || string(value) != binding {
			return nil, errors.New("consumer organization belongs to another claim")
		}
	}
	result := &identity.ConsumerIdentity{OrganizationID: orgID, ProviderIDs: map[string]string{}, CredentialDigests: digests}
	for _, provider := range spec.Providers {
		id, err := c.ensureConsumerProvider(ctx, orgID, provider.Kind, credentials[provider.Kind], digests[provider.Kind], previous)
		if err != nil {
			return nil, err
		}
		result.ProviderIDs[provider.Kind] = id
	}
	if err := c.ensureConsumerLogin(ctx, result); err != nil {
		return nil, fmt.Errorf("reconcile consumer login policy: %w", err)
	}
	if err := c.ensureConsumerBranding(ctx, orgID, spec); err != nil {
		return nil, fmt.Errorf("reconcile consumer branding: %w", err)
	}
	if err := c.ensureConsumerEmailOTP(ctx, orgID, spec.EmailOTP); err != nil {
		return nil, fmt.Errorf("reconcile consumer email OTP: %w", err)
	}
	return result, nil
}

func (c *Client) ensureConsumerProvider(ctx context.Context, orgID, kind string, credential consumerCredential, digest string, previous *identity.ConsumerIdentity) (string, error) {
	type provider struct {
		OwnerType string                     `json:"owner"`
		ID        string                     `json:"id"`
		Name      string                     `json:"name"`
		State     string                     `json:"state"`
		Config    map[string]json.RawMessage `json:"config"`
	}
	providers, err := listAll[provider](ctx, c, "/management/v1/idps/templates/_search", orgID, nil, 100)
	if err != nil {
		return "", err
	}
	id := ""
	var remoteConfig map[string]json.RawMessage
	configKind := kind
	if kind == "facebook" {
		configKind = "oauth"
	}
	for _, provider := range providers {
		if strings.EqualFold(provider.Name, kind) {
			if id != "" || provider.ID == "" || provider.State != "IDP_STATE_ACTIVE" || provider.Config[configKind] == nil || provider.OwnerType != "IDP_OWNER_TYPE_ORG" {
				return "", errors.New("ambiguous, inactive or mismatched consumer provider")
			}
			id = provider.ID
			remoteConfig = provider.Config
		}
	}
	if previous != nil && previous.ProviderIDs[kind] != "" && previous.ProviderIDs[kind] != id {
		return "", errors.New("recorded consumer provider identity changed")
	}
	body := map[string]any{"name": strings.ToUpper(kind[:1]) + kind[1:], "clientId": credential.ClientID, "providerOptions": map[string]any{"isCreationAllowed": true, "isAutoCreation": true, "isLinkingAllowed": false, "isAutoUpdate": false, "autoLinking": "AUTO_LINKING_OPTION_UNSPECIFIED"}}
	if kind == "google" {
		body["clientSecret"] = credential.ClientSecret
		body["scopes"] = []string{"openid", "profile", "email"}
	} else if kind == "facebook" {
		body["clientSecret"] = credential.ClientSecret
		body["scopes"] = []string{"public_profile", "email"}
		body["authorizationEndpoint"] = "https://www.facebook.com/v26.0/dialog/oauth"
		body["tokenEndpoint"] = "https://graph.facebook.com/v26.0/oauth/access_token"
		body["userEndpoint"] = "https://graph.facebook.com/v26.0/me?fields=id,email,first_name,last_name,name"
		body["idAttribute"] = "id"
		body["usePkce"] = false
	} else {
		body["teamId"] = credential.TeamID
		body["keyId"] = credential.KeyID
		body["privateKey"] = credential.PrivateKey
		body["scopes"] = []string{"name", "email"}
	}
	if id != "" && previous != nil && previous.CredentialDigests[kind] == digest && consumerProviderInSync(remoteConfig, configKind, body) {
		return id, nil
	}
	path := "/management/v1/idps/" + configKind
	method := http.MethodPost
	if id != "" {
		method = http.MethodPut
		path += "/" + url.PathEscape(id)
	}
	var response struct {
		ID string `json:"id"`
	}
	if err := c.request(ctx, method, path, orgID, body, &response); err != nil {
		return "", fmt.Errorf("reconcile consumer %s provider: %w", kind, err)
	}
	if id == "" {
		id = response.ID
	}
	if id == "" {
		return "", errors.New("consumer provider response has no id")
	}
	return id, nil
}

func policyBody(policy map[string]any) map[string]any {
	body := map[string]any{"passwordlessType": "PASSWORDLESS_TYPE_NOT_ALLOWED"}
	for _, key := range []string{"allowUsernamePassword", "allowRegister", "allowExternalIdp", "forceMfa", "passwordlessType", "hidePasswordReset", "ignoreUnknownUsernames", "defaultRedirectUri", "passwordCheckLifetime", "externalLoginCheckLifetime", "mfaInitSkipLifetime", "secondFactorCheckLifetime", "multiFactorCheckLifetime", "allowDomainDiscovery", "disableLoginWithEmail", "disableLoginWithPhone", "forceMfaLocalOnly"} {
		if v, ok := policy[key]; ok {
			body[key] = v
		}
	}
	return body
}

func overlay(body, desired map[string]any) bool {
	dirty := false
	for key, value := range desired {
		actual := body[key]
		if actual == nil {
			switch value.(type) {
			case bool:
				actual = false
			case string:
				actual = ""
			}
		}
		if !reflect.DeepEqual(actual, value) {
			dirty = true
		}
		body[key] = value
	}
	return dirty
}

func (c *Client) ensureConsumerLogin(ctx context.Context, receipt *identity.ConsumerIdentity) error {
	var current struct {
		IsDefault bool           `json:"isDefault"`
		Policy    map[string]any `json:"policy"`
	}
	path := "/management/v1/policies/login"
	if err := c.request(ctx, http.MethodGet, path, receipt.OrganizationID, nil, &current); err != nil {
		return err
	}
	if current.Policy == nil {
		return errors.New("consumer login policy missing")
	}
	inherited := current.IsDefault || current.Policy["isDefault"] == true
	body := policyBody(current.Policy)
	dirty := overlay(body, map[string]any{"allowUsernamePassword": false, "allowRegister": false, "allowExternalIdp": true, "allowDomainDiscovery": false, "hidePasswordReset": true, "passwordlessType": "PASSWORDLESS_TYPE_NOT_ALLOWED", "forceMfa": false, "forceMfaLocalOnly": false, "externalLoginCheckLifetime": "43200s"})
	idps := []map[string]string{}
	for _, kind := range []string{"google", "apple", "facebook"} {
		if id := receipt.ProviderIDs[kind]; id != "" {
			idps = append(idps, map[string]string{"idpId": id, "ownerType": "IDP_OWNER_TYPE_ORG"})
		}
	}
	if inherited {
		body["idps"] = idps
		body["secondFactors"] = []string{}
		body["multiFactors"] = []string{}
		if err := c.request(ctx, http.MethodPost, path, receipt.OrganizationID, body, nil); err != nil {
			return err
		}
	} else if dirty {
		if err := c.request(ctx, http.MethodPut, path, receipt.OrganizationID, body, nil); err != nil {
			return err
		}
	}
	linked, err := listAll[struct {
		ID string `json:"idpId"`
	}](ctx, c, path+"/idps/_search", receipt.OrganizationID, nil, 100)
	if err != nil {
		return err
	}
	wanted := map[string]bool{}
	for _, item := range idps {
		wanted[item["idpId"]] = true
	}
	for _, item := range linked {
		if wanted[item.ID] {
			delete(wanted, item.ID)
		} else {
			if item.ID == "" {
				return errors.New("invalid consumer provider link")
			}
			if err := c.request(ctx, http.MethodDelete, path+"/idps/"+url.PathEscape(item.ID), receipt.OrganizationID, nil, nil); err != nil {
				return err
			}
		}
	}
	for id := range wanted {
		if err := c.request(ctx, http.MethodPost, path+"/idps", receipt.OrganizationID, map[string]string{"idpId": id, "ownerType": "IDP_OWNER_TYPE_ORG"}, nil); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) ensureConsumerBranding(ctx context.Context, orgID string, spec identity.ConsumerOrganization) error {
	var current struct {
		IsDefault bool           `json:"isDefault"`
		Policy    map[string]any `json:"policy"`
	}
	path := "/management/v1/policies/label"
	if err := c.request(ctx, http.MethodGet, path, orgID, nil, &current); err != nil {
		return err
	}
	if current.Policy == nil {
		return errors.New("consumer branding policy missing")
	}
	body := map[string]any{}
	for _, key := range []string{"primaryColor", "backgroundColor", "fontColor", "warnColor", "primaryColorDark", "backgroundColorDark", "fontColorDark", "warnColorDark", "disableWatermark", "hideLoginNameSuffix", "themeMode"} {
		if value, ok := current.Policy[key]; ok {
			body[key] = value
		}
	}
	dirty := overlay(body, map[string]any{"primaryColor": spec.PrimaryColor, "backgroundColor": spec.BackgroundColor, "fontColor": spec.FontColor, "primaryColorDark": "#8EA2FF", "backgroundColorDark": "#151923", "fontColorDark": "#FFFFFF", "warnColor": "#B42318", "warnColorDark": "#FDA29B", "disableWatermark": true, "hideLoginNameSuffix": true, "themeMode": "THEME_MODE_LIGHT"})
	inherited := current.IsDefault || current.Policy["isDefault"] == true
	if !dirty && !inherited {
		return nil
	}
	method := http.MethodPut
	if inherited {
		method = http.MethodPost
	}
	if err := c.request(ctx, method, path, orgID, body, nil); err != nil {
		return err
	}
	return c.request(ctx, http.MethodPost, path+"/_activate", orgID, map[string]any{}, nil)
}

func consumerProviderInSync(config map[string]json.RawMessage, kind string, body map[string]any) bool {
	var live, options map[string]any
	if json.Unmarshal(config[kind], &live) != nil || json.Unmarshal(config["options"], &options) != nil {
		return false
	}
	desired := map[string]any{}
	for key, value := range body {
		if key != "name" && key != "clientSecret" && key != "privateKey" && key != "providerOptions" {
			raw, err := json.Marshal(value)
			if err != nil {
				return false
			}
			var normalized any
			if json.Unmarshal(raw, &normalized) != nil {
				return false
			}
			desired[key] = normalized
		}
	}
	if overlay(live, desired) {
		return false
	}
	if options["autoLinking"] == nil {
		options["autoLinking"] = "AUTO_LINKING_OPTION_UNSPECIFIED"
	}
	return !overlay(options, body["providerOptions"].(map[string]any))
}
