package zitadelapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
)

func (c *Client) ensureConsumerEmailOTP(ctx context.Context, orgID string, enabled *bool) error {
	if enabled == nil {
		return nil
	}
	const key = "consumer_email_otp"
	path := "/management/v1/metadata/" + key
	var current struct {
		Metadata struct {
			Key   string `json:"key"`
			Value string `json:"value"`
		} `json:"metadata"`
	}
	err := c.request(ctx, http.MethodGet, path, orgID, nil, &current)
	if err != nil {
		var status *statusError
		if !errors.As(err, &status) || status.code != http.StatusNotFound {
			return err
		}
	}
	value, err := json.Marshal(struct {
		Version    int  `json:"version"`
		Enabled    bool `json:"enabled"`
		SMSEnabled bool `json:"smsEnabled"`
	}{Version: 1, Enabled: *enabled})
	if err != nil {
		return err
	}
	encoded := base64.StdEncoding.EncodeToString(value)
	if current.Metadata.Key == key && current.Metadata.Value == encoded {
		return nil
	}
	return c.request(ctx, http.MethodPost, path, orgID, map[string]string{"value": encoded}, nil)
}
