package v1alpha1

import (
	"errors"
	"net/mail"
	"strings"
	"unicode"
	"unicode/utf8"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// ZitadelOrganizationSpec binds a private customer tenant to one product.
type ZitadelOrganizationSpec struct {
	InitialAdminUserID string                    `json:"initialAdminUserId"`
	InitialAdmin       *OrganizationInitialAdmin `json:"initialAdmin,omitempty"`
	Product            string                    `json:"product"`
	TenantID           string                    `json:"tenantId"`
	DisplayName        string                    `json:"displayName"`
	OrganizationID     string                    `json:"organizationId,omitempty"`
	// PreserveLoginPolicy is an adoption-only escape hatch for a staged MFA cutover.
	PreserveLoginPolicy bool `json:"preserveLoginPolicy,omitempty"`
}

// OrganizationInitialAdmin is private first-owner input, never an archive payload.
type OrganizationInitialAdmin struct {
	GivenName  string `json:"givenName"`
	FamilyName string `json:"familyName"`
	Email      string `json:"email"`
}

// ValidateInitialAdmin rejects ambiguous first-owner and adoption modes.
func (s ZitadelOrganizationSpec) ValidateInitialAdmin() error {
	if s.InitialAdmin == nil {
		return nil
	}
	if s.OrganizationID != "" || s.PreserveLoginPolicy || s.InitialAdminUserID != "owner-"+s.TenantID {
		return errors.New("first owner requires a new organization and tenant-bound subject")
	}
	for _, value := range []string{s.InitialAdmin.GivenName, s.InitialAdmin.FamilyName, s.InitialAdmin.Email} {
		if !utf8.ValidString(value) || strings.TrimSpace(value) != value || value == "" || utf8.RuneCountInString(value) > 200 || strings.ContainsFunc(value, unicode.IsControl) {
			return errors.New("invalid first owner profile")
		}
	}
	address, err := mail.ParseAddress(s.InitialAdmin.Email)
	if err != nil || address.Address != s.InitialAdmin.Email || address.Name != "" {
		return errors.New("invalid first owner email")
	}
	return nil
}

type ZitadelOrganizationStatus struct {
	Activated          bool               `json:"activated,omitempty"`
	OrganizationID     string             `json:"organizationId,omitempty"`
	ProjectID          string             `json:"projectId,omitempty"`
	ArchiveObject      string             `json:"archiveObject,omitempty"`
	ArchiveGeneration  string             `json:"archiveGeneration,omitempty"`
	ObservedGeneration int64              `json:"observedGeneration,omitempty"`
	Conditions         []metav1.Condition `json:"conditions,omitempty"`
}

type ZitadelOrganization struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              ZitadelOrganizationSpec   `json:"spec"`
	Status            ZitadelOrganizationStatus `json:"status,omitempty"`
}

type ZitadelOrganizationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ZitadelOrganization `json:"items"`
}

func (in *ZitadelOrganization) DeepCopy() *ZitadelOrganization {
	if in == nil {
		return nil
	}
	out := *in
	out.ObjectMeta = *in.ObjectMeta.DeepCopy()
	if in.Spec.InitialAdmin != nil {
		profile := *in.Spec.InitialAdmin
		out.Spec.InitialAdmin = &profile
	}
	out.Status.Conditions = append([]metav1.Condition(nil), in.Status.Conditions...)
	return &out
}
func (in *ZitadelOrganization) DeepCopyObject() runtime.Object { return in.DeepCopy() }
func (in *ZitadelOrganizationList) DeepCopyObject() runtime.Object {
	if in == nil {
		return nil
	}
	out := *in
	out.Items = make([]ZitadelOrganization, len(in.Items))
	for i := range in.Items {
		out.Items[i] = *in.Items[i].DeepCopy()
	}
	return &out
}
