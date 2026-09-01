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
	// Absent means public: any signed-in org user gets tokens.
	Access *ZitadelProjectAccess `json:"access,omitempty"`
}

// ZitadelProjectAccess declares the product's audience. Restricted projects
// turn on Zitadel's projectRoleCheck, so only users holding a role grant in
// the project can authenticate; the members list is reconciled to exactly
// those grants.
type ZitadelProjectAccess struct {
	// Mode is "public" or "restricted".
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
	scheme.AddKnownTypes(GroupVersion, &ZitadelProject{}, &ZitadelProjectList{}, &ZitadelApplication{}, &ZitadelApplicationList{})
	metav1.AddToGroupVersion(scheme, GroupVersion)
	return nil
}
