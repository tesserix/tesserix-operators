package main

import (
	identity "github.com/tesserix/devai-sandbox-operator/operators/zitadel/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"testing"
)

func TestOrganizationEventsAcceptExplicitRefreshAndIgnoreStatusOnlyWrites(t *testing.T) {
	old := &identity.ZitadelOrganization{ObjectMeta: metav1.ObjectMeta{Generation: 1}}
	refreshed := old.DeepCopy()
	refreshed.Annotations = map[string]string{identity.OrganizationReconcileRequestAnnotation: "018f73cd-b476-7000-8000-000000000001"}
	if !organizationEvents().Update(event.UpdateEvent{ObjectOld: old, ObjectNew: refreshed}) {
		t.Fatal("metadata-only policy refresh did not wake the organization reconciler")
	}
	status := refreshed.DeepCopy()
	status.Status.Activated = true
	if organizationEvents().Update(event.UpdateEvent{ObjectOld: refreshed, ObjectNew: status}) {
		t.Fatal("status-only write caused a reconcile loop")
	}
}
