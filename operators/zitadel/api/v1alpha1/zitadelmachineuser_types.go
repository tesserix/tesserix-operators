package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// ZitadelMachineUserSpec declares a service account and the one credential it
// gets. The credential is published only to the OpenBao path the operator
// derives for the claim, never to the claim or its status.
type ZitadelMachineUserSpec struct {
	Organization string `json:"organization"`
	// UserName is the Zitadel login name, and the client ID for
	// clientSecret credentials.
	UserName    string `json:"userName"`
	DisplayName string `json:"displayName"`
	// Credential is "personalAccessToken" or "clientSecret".
	Credential string `json:"credential"`
}

type ZitadelMachineUserStatus struct {
	UserID string `json:"userId,omitempty"`
	// ClientID is set for clientSecret credentials. It is an identifier, not a
	// secret; consumers pair it with the published secret.
	ClientID           string             `json:"clientId,omitempty"`
	ObservedGeneration int64              `json:"observedGeneration,omitempty"`
	Conditions         []metav1.Condition `json:"conditions,omitempty"`
}

type ZitadelMachineUser struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              ZitadelMachineUserSpec   `json:"spec"`
	Status            ZitadelMachineUserStatus `json:"status,omitempty"`
}

type ZitadelMachineUserList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ZitadelMachineUser `json:"items"`
}

func (in *ZitadelMachineUser) DeepCopyObject() runtime.Object { return in.DeepCopy() }

func (in *ZitadelMachineUser) DeepCopy() *ZitadelMachineUser {
	if in == nil {
		return nil
	}
	out := *in
	out.ObjectMeta = *in.ObjectMeta.DeepCopy()
	out.Status.Conditions = append([]metav1.Condition(nil), in.Status.Conditions...)
	return &out
}

func (in *ZitadelMachineUserList) DeepCopyObject() runtime.Object { return in.DeepCopy() }

func (in *ZitadelMachineUserList) DeepCopy() *ZitadelMachineUserList {
	if in == nil {
		return nil
	}
	out := *in
	out.Items = make([]ZitadelMachineUser, len(in.Items))
	for i := range in.Items {
		out.Items[i] = *in.Items[i].DeepCopy()
	}
	return &out
}
