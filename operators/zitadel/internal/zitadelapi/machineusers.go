package zitadelapi

import (
	"context"
	"errors"
	"net/http"
	"net/url"
)

// AccessTokenTypeJWT makes Zitadel issue the machine user self-contained JWT
// access tokens, which gateways and APIs can verify locally. Opaque tokens
// would need introspection that no Tesserix consumer performs.
const AccessTokenTypeJWT = "ACCESS_TOKEN_TYPE_JWT"

type MachineUser struct {
	ID              string
	UserName        string
	Name            string
	AccessTokenType string
}

type machineUserWire struct {
	ID       string `json:"id"`
	UserName string `json:"userName"`
	Machine  *struct {
		Name            string `json:"name"`
		AccessTokenType string `json:"accessTokenType"`
	} `json:"machine"`
}

func (w machineUserWire) user() (MachineUser, bool) {
	if w.Machine == nil {
		return MachineUser{}, false
	}
	return MachineUser{ID: w.ID, UserName: w.UserName, Name: w.Machine.Name, AccessTokenType: w.Machine.AccessTokenType}, true
}

// errNotMachine refuses to adopt a human account that happens to share the
// declared user name: issuing it a PAT would hand a person's identity to a
// service.
var errNotMachine = errors.New("Zitadel user with the declared userName is not a machine user")

func (c *Client) FindMachineUserByID(ctx context.Context, organization, userID string) (MachineUser, bool, error) {
	organizationID, err := c.organizationID(ctx, organization)
	if err != nil {
		return MachineUser{}, false, err
	}
	response := struct {
		User machineUserWire `json:"user"`
	}{}
	if err := c.request(ctx, http.MethodGet, "/management/v1/users/"+url.PathEscape(userID), organizationID, nil, &response); err != nil {
		var statusError *statusError
		if errors.As(err, &statusError) && statusError.code == http.StatusNotFound {
			return MachineUser{}, false, nil
		}
		return MachineUser{}, false, err
	}
	user, ok := response.User.user()
	if !ok {
		return MachineUser{}, false, errNotMachine
	}
	return user, true, nil
}

func (c *Client) FindMachineUserByName(ctx context.Context, organization, userName string) (MachineUser, bool, error) {
	organizationID, err := c.organizationID(ctx, organization)
	if err != nil {
		return MachineUser{}, false, err
	}
	request := map[string]any{
		"query":   map[string]any{"limit": 2},
		"queries": []map[string]any{{"userNameQuery": map[string]string{"userName": userName, "method": "TEXT_QUERY_METHOD_EQUALS"}}},
	}
	response := struct {
		Result []machineUserWire `json:"result"`
	}{}
	if err := c.request(ctx, http.MethodPost, "/management/v1/users/_search", organizationID, request, &response); err != nil {
		return MachineUser{}, false, err
	}
	switch len(response.Result) {
	case 0:
		return MachineUser{}, false, nil
	case 1:
		user, ok := response.Result[0].user()
		if !ok {
			return MachineUser{}, false, errNotMachine
		}
		return user, true, nil
	default:
		return MachineUser{}, false, errors.New("multiple Zitadel users match the declared userName")
	}
}

func (c *Client) CreateMachineUser(ctx context.Context, organization, userName, name string) (MachineUser, error) {
	organizationID, err := c.organizationID(ctx, organization)
	if err != nil {
		return MachineUser{}, err
	}
	request := map[string]any{"userName": userName, "name": name, "accessTokenType": AccessTokenTypeJWT}
	response := struct {
		UserID string `json:"userId"`
	}{}
	if err := c.request(ctx, http.MethodPost, "/management/v1/users/machine", organizationID, request, &response); err != nil {
		return MachineUser{}, err
	}
	if response.UserID == "" {
		return MachineUser{}, errors.New("Zitadel machine user response is missing an identifier")
	}
	return MachineUser{ID: response.UserID, UserName: userName, Name: name, AccessTokenType: AccessTokenTypeJWT}, nil
}

// UpdateMachineUser sets the name and JWT access tokens. Callers must
// drift-guard: Zitadel rejects updates that change nothing.
func (c *Client) UpdateMachineUser(ctx context.Context, organization, userID, name string) error {
	organizationID, err := c.organizationID(ctx, organization)
	if err != nil {
		return err
	}
	request := map[string]any{"name": name, "accessTokenType": AccessTokenTypeJWT}
	return c.request(ctx, http.MethodPut, "/management/v1/users/"+url.PathEscape(userID)+"/machine", organizationID, request, nil)
}

// ReplacePersonalAccessTokens revokes every PAT the user holds and issues one
// new token. Callers use it only when no published token exists, so every
// revoked token is one no consumer could be using.
func (c *Client) ReplacePersonalAccessTokens(ctx context.Context, organization, userID string) (string, error) {
	organizationID, err := c.organizationID(ctx, organization)
	if err != nil {
		return "", err
	}
	base := "/management/v1/users/" + url.PathEscape(userID) + "/pats"
	existing := struct {
		Result []struct {
			ID string `json:"id"`
		} `json:"result"`
	}{}
	if err := c.request(ctx, http.MethodPost, base+"/_search", organizationID, map[string]any{"query": map[string]any{"limit": 100}}, &existing); err != nil {
		return "", err
	}
	for _, token := range existing.Result {
		if err := c.request(ctx, http.MethodDelete, base+"/"+url.PathEscape(token.ID), organizationID, nil, nil); err != nil {
			return "", err
		}
	}
	response := struct {
		Token string `json:"token"`
	}{}
	if err := c.request(ctx, http.MethodPost, base, organizationID, map[string]any{}, &response); err != nil {
		return "", err
	}
	if response.Token == "" {
		return "", errors.New("Zitadel returned an empty personal access token")
	}
	return response.Token, nil
}

// ReplaceClientSecret generates a new client secret, which invalidates the
// previous one.
func (c *Client) ReplaceClientSecret(ctx context.Context, organization, userID string) (clientID, secret string, err error) {
	organizationID, err := c.organizationID(ctx, organization)
	if err != nil {
		return "", "", err
	}
	response := struct {
		ClientID     string `json:"clientId"`
		ClientSecret string `json:"clientSecret"`
	}{}
	if err := c.request(ctx, http.MethodPut, "/management/v1/users/"+url.PathEscape(userID)+"/secret", organizationID, map[string]any{}, &response); err != nil {
		return "", "", err
	}
	if response.ClientID == "" || response.ClientSecret == "" {
		return "", "", errors.New("Zitadel returned an incomplete client secret")
	}
	return response.ClientID, response.ClientSecret, nil
}
