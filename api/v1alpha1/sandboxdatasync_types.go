package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var GroupVersion = schema.GroupVersion{Group: "devai.tesserix.io", Version: "v1alpha1"}

type SecretKeyReference struct {
	Name string `json:"name"`
	Key  string `json:"key"`
}

type DatabaseReference struct {
	SecretRef SecretKeyReference `json:"secretRef"`
}

type ColumnRule struct {
	Name      string `json:"name"`
	Transform string `json:"transform"`
}

type TableRule struct {
	Source  string       `json:"source"`
	Target  string       `json:"target"`
	Columns []ColumnRule `json:"columns"`
}

type SandboxDataSyncSpec struct {
	Schedule                   string             `json:"schedule"`
	Source                     DatabaseReference  `json:"source"`
	Target                     DatabaseReference  `json:"target"`
	AnonymizationSaltSecretRef SecretKeyReference `json:"anonymizationSaltSecretRef"`
	Tables                     []TableRule        `json:"tables"`
}

type SandboxDataSyncStatus struct {
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

type SandboxDataSync struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              SandboxDataSyncSpec   `json:"spec"`
	Status            SandboxDataSyncStatus `json:"status,omitempty"`
}

type SandboxDataSyncList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SandboxDataSync `json:"items"`
}

func (in *SandboxDataSync) DeepCopyObject() runtime.Object { return in.DeepCopy() }
func (in *SandboxDataSync) DeepCopy() *SandboxDataSync {
	if in == nil {
		return nil
	}
	out := *in
	out.ObjectMeta = *in.ObjectMeta.DeepCopy()
	out.Spec.Tables = make([]TableRule, len(in.Spec.Tables))
	for i := range in.Spec.Tables {
		out.Spec.Tables[i] = in.Spec.Tables[i]
		out.Spec.Tables[i].Columns = append([]ColumnRule(nil), in.Spec.Tables[i].Columns...)
	}
	return &out
}
func (in *SandboxDataSyncList) DeepCopyObject() runtime.Object { return in.DeepCopy() }
func (in *SandboxDataSyncList) DeepCopy() *SandboxDataSyncList {
	if in == nil {
		return nil
	}
	out := *in
	out.Items = make([]SandboxDataSync, len(in.Items))
	for i := range in.Items {
		out.Items[i] = *in.Items[i].DeepCopy()
	}
	return &out
}

func AddToScheme(scheme *runtime.Scheme) error {
	scheme.AddKnownTypes(GroupVersion, &SandboxDataSync{}, &SandboxDataSyncList{})
	metav1.AddToGroupVersion(scheme, GroupVersion)
	return nil
}
