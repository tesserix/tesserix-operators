package v1alpha1_test

import (
	identity "github.com/tesserix/devai-sandbox-operator/operators/zitadel/api/v1alpha1"
	"strings"
	"testing"
)

func TestInitialAdminProfileRejectsAmbiguousModesAndUnsafeContact(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*identity.ZitadelOrganizationSpec)
	}{
		{"adoption", func(s *identity.ZitadelOrganizationSpec) { s.OrganizationID = "existing" }},
		{"preserve_policy", func(s *identity.ZitadelOrganizationSpec) { s.PreserveLoginPolicy = true }},
		{"borrowed_subject", func(s *identity.ZitadelOrganizationSpec) { s.InitialAdminUserID = "foreign" }},
		{"missing_given_name", func(s *identity.ZitadelOrganizationSpec) { s.InitialAdmin.GivenName = "" }},
		{"missing_family_name", func(s *identity.ZitadelOrganizationSpec) { s.InitialAdmin.FamilyName = "" }},
		{"long_name", func(s *identity.ZitadelOrganizationSpec) { s.InitialAdmin.GivenName = strings.Repeat("a", 201) }},
		{"control_name", func(s *identity.ZitadelOrganizationSpec) { s.InitialAdmin.GivenName = "a\nb" }},
		{"padded_name", func(s *identity.ZitadelOrganizationSpec) { s.InitialAdmin.FamilyName = " Owner" }},
		{"invalid_utf8", func(s *identity.ZitadelOrganizationSpec) { s.InitialAdmin.GivenName = string([]byte{255}) }},
		{"email_display_name", func(s *identity.ZitadelOrganizationSpec) { s.InitialAdmin.Email = "Owner <owner@example.test>" }},
		{"invalid_email", func(s *identity.ZitadelOrganizationSpec) { s.InitialAdmin.Email = "invalid" }},
		{"long_email", func(s *identity.ZitadelOrganizationSpec) {
			s.InitialAdmin.Email = strings.Repeat("a", 190) + "@example.test"
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := firstOwnerSpec()
			tc.change(&s)
			if s.ValidateInitialAdmin() == nil {
				t.Fatal("invalid first-owner input accepted")
			}
		})
	}
	s := firstOwnerSpec()
	s.InitialAdmin.GivenName = strings.Repeat("अ", 200)
	if err := s.ValidateInitialAdmin(); err != nil {
		t.Fatalf("native Unicode boundary rejected: %v", err)
	}
	s.InitialAdmin = nil
	s.InitialAdminUserID = "existing-owner"
	if err := s.ValidateInitialAdmin(); err != nil {
		t.Fatal("legacy owner mode rejected")
	}
}

func TestOrganizationDeepCopyDoesNotAliasInitialAdminProfile(t *testing.T) {
	original := &identity.ZitadelOrganization{Spec: firstOwnerSpec()}
	copied := original.DeepCopy()
	copied.Spec.InitialAdmin.Email = "changed@example.test"
	if original.Spec.InitialAdmin.Email != "owner@example.test" {
		t.Fatal("profile was shared across copies")
	}
}

func firstOwnerSpec() identity.ZitadelOrganizationSpec {
	return identity.ZitadelOrganizationSpec{TenantID: "11111111-1111-4111-8111-111111111111", InitialAdminUserID: "owner-11111111-1111-4111-8111-111111111111", InitialAdmin: &identity.OrganizationInitialAdmin{GivenName: "Test", FamilyName: "Owner", Email: "owner@example.test"}}
}
