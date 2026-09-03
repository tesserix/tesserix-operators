package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var GroupVersion = schema.GroupVersion{Group: "evals.tesserix.app", Version: "v1alpha1"}

type Dataset struct {
	Name        string `json:"name"`
	Modality    string `json:"modality"`
	Description string `json:"description,omitempty"`
}

type EvalOnboardingSpec struct {
	DisplayName string    `json:"displayName"`
	Datasets    []Dataset `json:"datasets,omitempty"`
}

type EvalOnboardingStatus struct {
	ProjectID          string             `json:"projectId,omitempty"`
	PublicKeySecret    string             `json:"publicKeySecret,omitempty"`
	SecretKeySecret    string             `json:"secretKeySecret,omitempty"`
	DatasetsRegistered int                `json:"datasetsRegistered,omitempty"`
	ObservedGeneration int64              `json:"observedGeneration,omitempty"`
	Conditions         []metav1.Condition `json:"conditions,omitempty"`
}

type EvalOnboarding struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              EvalOnboardingSpec   `json:"spec"`
	Status            EvalOnboardingStatus `json:"status,omitempty"`
}

type EvalOnboardingList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []EvalOnboarding `json:"items"`
}

func (in *EvalOnboarding) DeepCopyObject() runtime.Object { return in.DeepCopy() }

func (in *EvalOnboarding) DeepCopy() *EvalOnboarding {
	if in == nil {
		return nil
	}
	out := *in
	out.ObjectMeta = *in.ObjectMeta.DeepCopy()
	out.Spec.Datasets = append([]Dataset(nil), in.Spec.Datasets...)
	out.Status.Conditions = append([]metav1.Condition(nil), in.Status.Conditions...)
	return &out
}

func (in *EvalOnboardingList) DeepCopyObject() runtime.Object { return in.DeepCopy() }

func (in *EvalOnboardingList) DeepCopy() *EvalOnboardingList {
	if in == nil {
		return nil
	}
	out := *in
	out.Items = make([]EvalOnboarding, len(in.Items))
	for i := range in.Items {
		out.Items[i] = *in.Items[i].DeepCopy()
	}
	return &out
}

func AddToScheme(scheme *runtime.Scheme) error {
	scheme.AddKnownTypes(GroupVersion, &EvalOnboarding{}, &EvalOnboardingList{})
	metav1.AddToGroupVersion(scheme, GroupVersion)
	return nil
}
