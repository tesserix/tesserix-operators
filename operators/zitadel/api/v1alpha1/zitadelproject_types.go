package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var GroupVersion = schema.GroupVersion{Group: "identity.tesserix.app", Version: "v1alpha1"}

type ZitadelProjectSpec struct {
	DisplayName  string `json:"displayName"`
	Organization string `json:"organization"`
	// Access controls who may authenticate to this project's applications.
	// Absent preserves the access policy of an adopted project.
	Access *ZitadelProjectAccess `json:"access,omitempty"`
}

// ZitadelProjectAccess declares the product's audience. Restricted projects
// turn on Zitadel's projectRoleCheck, so only users holding a role grant in
// the project can authenticate; the members list is reconciled to exactly
// those grants.
type ZitadelProjectAccess struct {
	// Mode is public, restricted (exact user grants), or organization (tenant project grants).
	Mode string `json:"mode"`
	// Members are the only users allowed to sign in when Mode is restricted.
	Members []ZitadelProjectMember `json:"members,omitempty"`
}

type ZitadelProjectMember struct {
	Email string `json:"email"`
	// Roles the member is granted; defaults to ["member"].
	Roles []string `json:"roles,omitempty"`
}

type ZitadelProjectStatus struct {
	ProjectID          string             `json:"projectId,omitempty"`
	ObservedGeneration int64              `json:"observedGeneration,omitempty"`
	Conditions         []metav1.Condition `json:"conditions,omitempty"`
}

type ZitadelProject struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              ZitadelProjectSpec   `json:"spec"`
	Status            ZitadelProjectStatus `json:"status,omitempty"`
}

type ZitadelProjectList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ZitadelProject `json:"items"`
}

type ZitadelProjectReference struct {
	Name string `json:"name"`
}

type ZitadelApplicationSpec struct {
	AccessTokenType          string `json:"accessTokenType,omitempty"`
	IDTokenUserinfoAssertion *bool  `json:"idTokenUserinfoAssertion,omitempty"`
	// LoginBaseURI selects a product-hosted Login V2 UI. Nil preserves an adopted client's login UI.
	LoginBaseURI           *string                 `json:"loginBaseUri,omitempty"`
	RefreshToken           *bool                   `json:"refreshToken,omitempty"`
	ProjectRef             ZitadelProjectReference `json:"projectRef"`
	DisplayName            string                  `json:"displayName"`
	ApplicationType        string                  `json:"applicationType"`
	RedirectURIs           []string                `json:"redirectUris"`
	PostLogoutRedirectURIs []string                `json:"postLogoutRedirectUris,omitempty"`
}

type ZitadelApplicationStatus struct {
	AppID              string             `json:"appId,omitempty"`
	ClientID           string             `json:"clientId,omitempty"`
	ObservedGeneration int64              `json:"observedGeneration,omitempty"`
	Conditions         []metav1.Condition `json:"conditions,omitempty"`
}

type ZitadelApplication struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              ZitadelApplicationSpec   `json:"spec"`
	Status            ZitadelApplicationStatus `json:"status,omitempty"`
}

type ZitadelApplicationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ZitadelApplication `json:"items"`
}

func (in *ZitadelProject) DeepCopyObject() runtime.Object {
	return in.DeepCopy()
}

func (in *ZitadelProject) DeepCopy() *ZitadelProject {
	if in == nil {
		return nil
	}
	out := *in
	out.ObjectMeta = *in.ObjectMeta.DeepCopy()
	if in.Spec.Access != nil {
		access := ZitadelProjectAccess{Mode: in.Spec.Access.Mode}
		for _, member := range in.Spec.Access.Members {
			access.Members = append(access.Members, ZitadelProjectMember{Email: member.Email, Roles: append([]string(nil), member.Roles...)})
		}
		out.Spec.Access = &access
	}
	out.Status.Conditions = append([]metav1.Condition(nil), in.Status.Conditions...)
	return &out
}

func (in *ZitadelProjectList) DeepCopyObject() runtime.Object {
	return in.DeepCopy()
}

func (in *ZitadelProjectList) DeepCopy() *ZitadelProjectList {
	if in == nil {
		return nil
	}
	out := *in
	out.ListMeta = in.ListMeta
	out.Items = make([]ZitadelProject, len(in.Items))
	for i := range in.Items {
		out.Items[i] = *in.Items[i].DeepCopy()
	}
	return &out
}

func (in *ZitadelApplication) DeepCopyObject() runtime.Object { return in.DeepCopy() }

func (in *ZitadelApplication) DeepCopy() *ZitadelApplication {
	if in == nil {
		return nil
	}
	out := *in
	out.ObjectMeta = *in.ObjectMeta.DeepCopy()
	if in.Spec.LoginBaseURI != nil {
		value := *in.Spec.LoginBaseURI
		out.Spec.LoginBaseURI = &value
	}
	if in.Spec.IDTokenUserinfoAssertion != nil {
		value := *in.Spec.IDTokenUserinfoAssertion
		out.Spec.IDTokenUserinfoAssertion = &value
	}
	if in.Spec.RefreshToken != nil {
		value := *in.Spec.RefreshToken
		out.Spec.RefreshToken = &value
	}
	out.Spec.RedirectURIs = append([]string(nil), in.Spec.RedirectURIs...)
	out.Spec.PostLogoutRedirectURIs = append([]string(nil), in.Spec.PostLogoutRedirectURIs...)
	out.Status.Conditions = append([]metav1.Condition(nil), in.Status.Conditions...)
	return &out
}

func (in *ZitadelApplicationList) DeepCopyObject() runtime.Object { return in.DeepCopy() }

func (in *ZitadelApplicationList) DeepCopy() *ZitadelApplicationList {
	if in == nil {
		return nil
	}
	out := *in
	out.Items = make([]ZitadelApplication, len(in.Items))
	for i := range in.Items {
		out.Items[i] = *in.Items[i].DeepCopy()
	}
	return &out
}

func AddToScheme(scheme *runtime.Scheme) error {
	scheme.AddKnownTypes(GroupVersion, &ZitadelProject{}, &ZitadelProjectList{}, &ZitadelApplication{}, &ZitadelApplicationList{}, &ZitadelMachineUser{}, &ZitadelMachineUserList{}, &ZitadelOrganization{}, &ZitadelOrganizationList{})
	metav1.AddToGroupVersion(scheme, GroupVersion)
	return nil
}
