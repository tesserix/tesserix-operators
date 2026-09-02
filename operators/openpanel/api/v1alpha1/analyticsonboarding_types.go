package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var GroupVersion = schema.GroupVersion{Group: "analytics.tesserix.app", Version: "v1alpha1"}

type AnalyticsOnboardingSpec struct {
	DisplayName string   `json:"displayName"`
	Domain      string   `json:"domain"`
	CORS        []string `json:"cors"`
	Types       []string `json:"types,omitempty"`
}

type AnalyticsOnboardingStatus struct {
	ProjectID          string             `json:"projectId,omitempty"`
	ClientIDSecret     string             `json:"clientIdSecret,omitempty"`
	ObservedGeneration int64              `json:"observedGeneration,omitempty"`
	Conditions         []metav1.Condition `json:"conditions,omitempty"`
}

type AnalyticsOnboarding struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              AnalyticsOnboardingSpec   `json:"spec"`
	Status            AnalyticsOnboardingStatus `json:"status,omitempty"`
}

type AnalyticsOnboardingList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AnalyticsOnboarding `json:"items"`
}

func (in *AnalyticsOnboarding) DeepCopyObject() runtime.Object { return in.DeepCopy() }

func (in *AnalyticsOnboarding) DeepCopy() *AnalyticsOnboarding {
	if in == nil {
		return nil
	}
	out := *in
	out.ObjectMeta = *in.ObjectMeta.DeepCopy()
	out.Spec.CORS = append([]string(nil), in.Spec.CORS...)
	out.Spec.Types = append([]string(nil), in.Spec.Types...)
	out.Status.Conditions = append([]metav1.Condition(nil), in.Status.Conditions...)
	return &out
}

func (in *AnalyticsOnboardingList) DeepCopyObject() runtime.Object { return in.DeepCopy() }

func (in *AnalyticsOnboardingList) DeepCopy() *AnalyticsOnboardingList {
	if in == nil {
		return nil
	}
	out := *in
	out.Items = make([]AnalyticsOnboarding, len(in.Items))
	for i := range in.Items {
		out.Items[i] = *in.Items[i].DeepCopy()
	}
	return &out
}

func AddToScheme(scheme *runtime.Scheme) error {
	scheme.AddKnownTypes(GroupVersion, &AnalyticsOnboarding{}, &AnalyticsOnboardingList{})
	metav1.AddToGroupVersion(scheme, GroupVersion)
	return nil
}
