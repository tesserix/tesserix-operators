package v1alpha1

// ConsumerOrganization owns an isolated consumer organization for one product.
type ConsumerOrganization struct {
	EmailOTP           *bool              `json:"emailOtp,omitempty"`
	InitialAdminUserID string             `json:"initialAdminUserId"`
	Providers          []ConsumerProvider `json:"providers"`
	PrimaryColor       string             `json:"primaryColor"`
	BackgroundColor    string             `json:"backgroundColor"`
	FontColor          string             `json:"fontColor"`
}

type ConsumerProvider struct {
	Kind       string `json:"kind"`
	SecretName string `json:"secretName"`
}

type ConsumerIdentity struct {
	OrganizationID    string            `json:"organizationId"`
	CredentialDigests map[string]string `json:"credentialDigests,omitempty"`
	ProviderIDs       map[string]string `json:"providerIds"`
}
