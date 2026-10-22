package main

import (
	"context"
	identityv1alpha1 "github.com/tesserix/devai-sandbox-operator/operators/zitadel/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"testing"
	"time"
)

func TestReviewedMachineUserPaths_derives_one_path_per_claim(t *testing.T) {
	paths, err := reviewedMachineUserPaths(" kora/kora-account-deleter, kora/kora-eval ")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"kora-account-deleter": "kora/app/kora-account-deleter", "kora-eval": "kora/app/kora-eval"}
	if len(paths) != len(want) || paths["kora-account-deleter"] != want["kora-account-deleter"] || paths["kora-eval"] != want["kora-eval"] {
		t.Fatalf("paths = %v", paths)
	}
	if empty, err := reviewedMachineUserPaths(""); err != nil || len(empty) != 0 {
		t.Fatalf("empty = %v, %v", empty, err)
	}
	for _, bad := range []string{"kora", "kora/../x", "Kora/deleter", "kora/a,kora/a"} {
		if _, err := reviewedMachineUserPaths(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestPeriodicReconciler_resyncs_existing_claims_and_ignores_absent_claims(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := identityv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	claim := &identityv1alpha1.ZitadelProject{ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "identity"}}
	kube := fake.NewClientBuilder().WithScheme(scheme).WithObjects(claim).Build()
	calls := 0
	r := periodicReconciler(kube, &identityv1alpha1.ZitadelProject{}, func(context.Context, types.NamespacedName) error { calls++; return nil })
	result, err := r.Reconcile(t.Context(), reconcile.Request{NamespacedName: types.NamespacedName{Namespace: "identity", Name: "p"}})
	if err != nil || result.RequeueAfter != 5*time.Minute || calls != 1 {
		t.Fatalf("result=%v calls=%d err=%v", result, calls, err)
	}
	result, err = r.Reconcile(t.Context(), reconcile.Request{NamespacedName: types.NamespacedName{Namespace: "identity", Name: "missing"}})
	if err != nil || result.RequeueAfter != 0 || calls != 1 {
		t.Fatalf("absent claim rescheduled: %v calls=%d err=%v", result, calls, err)
	}
}

func TestReviewedMachineUserPaths_separates_new_environment_paths(t *testing.T) {
	paths, err := reviewedMachineUserPaths("helivanta/prod/helivanta-login-client")
	if err != nil {
		t.Fatal(err)
	}
	if paths["helivanta-login-client"] != "helivanta/prod/app/helivanta-login-client" {
		t.Fatalf("paths=%v", paths)
	}
	for _, bad := range []string{"helivanta/unknown/helivanta-login-client", "helivanta/prod/../client", "helivanta/prod/kora-login-client"} {
		if _, err := reviewedMachineUserPaths(bad); err == nil {
			t.Fatalf("unsafe mapping accepted: %s", bad)
		}
	}
}

func TestConsumerProvidersRequireEnvironmentAndProductPrefix(t *testing.T) {
	for _, input := range []string{"civiclens/civiclens-google-idp", "civiclens/prod/roamie-google-idp", "civiclens/staging/civiclens-google-idp"} {
		if _, err := reviewedConsumerProviderPaths(input); err == nil {
			t.Fatalf("accepted %s", input)
		}
	}
	paths, err := reviewedConsumerProviderPaths("civiclens/prod/civiclens-google-idp-prod,civiclens/prod/civiclens-apple-idp-prod")
	if err != nil || paths["civiclens-apple-idp-prod"] != "civiclens/prod/app/civiclens-apple-idp-prod" {
		t.Fatalf("paths=%v err=%v", paths, err)
	}
}
