package main

import (
	"context"
	"flag"
	"os"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	devai "github.com/tesserix/devai-sandbox-operator/api/v1alpha1"
	"github.com/tesserix/devai-sandbox-operator/internal/sandboxsync"
)

func main() {
	var image string
	flag.StringVar(&image, "sync-image", os.Getenv("SYNC_IMAGE"), "sync worker image")
	flag.Parse()
	if image == "" {
		image = "ghcr.io/tesserix/devai-sandbox-sync:v0.1.0"
	}
	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	_ = devai.AddToScheme(scheme)
	manager, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{Scheme: scheme, HealthProbeBindAddress: ":8081"})
	if err != nil {
		panic(err)
	}
	reconciler := sandboxsync.NewReconciler(manager.GetClient(), scheme, image)
	if err := manager.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		panic(err)
	}
	if err := manager.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		panic(err)
	}
	if err := ctrl.NewControllerManagedBy(manager).For(&devai.SandboxDataSync{}).Owns(&batchv1.CronJob{}).Owns(&corev1.ConfigMap{}).Complete(reconcile.Func(func(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
		return reconcile.Result{}, reconciler.Reconcile(ctx, request.NamespacedName)
	})); err != nil {
		panic(err)
	}
	if err := manager.Start(ctrl.SetupSignalHandler()); err != nil {
		panic(err)
	}
}
