package main

import (
	"context"
	"flag"
	"net/http"
	"os"
	"time"

	"github.com/zitadel/oidc/v3/pkg/client/profile"

	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	identityv1alpha1 "github.com/tesserix/devai-sandbox-operator/operators/zitadel/api/v1alpha1"
	"github.com/tesserix/devai-sandbox-operator/operators/zitadel/internal/zitadelapi"
	"github.com/tesserix/devai-sandbox-operator/operators/zitadel/internal/zitadelapplication"
	"github.com/tesserix/devai-sandbox-operator/operators/zitadel/internal/zitadelproject"
)

func main() {
	var apiURL string
	var host string
	var machineKeyFile string
	var watchNamespace string
	flag.StringVar(&apiURL, "zitadel-api-url", envOr("ZITADEL_API_URL", "http://zitadel.zitadel.svc.cluster.local:8080"), "Zitadel management API URL")
	flag.StringVar(&host, "zitadel-host", envOr("ZITADEL_HOST", "auth.tesserix.app"), "Zitadel instance host")
	flag.StringVar(&machineKeyFile, "zitadel-machine-key-file", envOr("ZITADEL_MACHINE_KEY_FILE", "/var/run/zitadel-admin/machine-key.json"), "path to the Zitadel machine-key JSON file")
	flag.StringVar(&watchNamespace, "watch-namespace", envOr("WATCH_NAMESPACE", "identity-operator"), "namespace containing Zitadel claims")
	flag.Parse()

	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	_ = identityv1alpha1.AddToScheme(scheme)
	manager, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		HealthProbeBindAddress: ":8081",
		Cache:                  cache.Options{DefaultNamespaces: map[string]cache.Config{watchNamespace: {}}},
	})
	if err != nil {
		panic(err)
	}
	tokenSource, err := machineKeyTokenSource("https://"+host, machineKeyFile)
	if err != nil {
		panic(err)
	}
	projects, err := zitadelapi.NewClient(apiURL, host, tokenSource)
	if err != nil {
		panic(err)
	}
	projectReconciler := zitadelproject.NewReconciler(manager.GetClient(), scheme, projects)
	applicationReconciler := zitadelapplication.NewReconciler(manager.GetClient(), projects)
	if err := manager.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		panic(err)
	}
	if err := manager.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		panic(err)
	}
	if err := ctrl.NewControllerManagedBy(manager).For(&identityv1alpha1.ZitadelProject{}).Complete(reconcile.Func(func(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
		return reconcile.Result{}, projectReconciler.Reconcile(ctx, request.NamespacedName)
	})); err != nil {
		panic(err)
	}
	if err := ctrl.NewControllerManagedBy(manager).For(&identityv1alpha1.ZitadelApplication{}).Complete(reconcile.Func(func(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
		return reconcile.Result{}, applicationReconciler.Reconcile(ctx, request.NamespacedName)
	})); err != nil {
		panic(err)
	}
	if err := manager.Start(ctrl.SetupSignalHandler()); err != nil {
		panic(err)
	}
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func machineKeyTokenSource(issuer, machineKeyFile string) (zitadelapi.TokenSource, error) {
	key, err := os.ReadFile(machineKeyFile)
	if err != nil {
		return nil, err
	}
	source, err := profile.NewJWTProfileTokenSourceFromKeyFileData(
		context.Background(),
		issuer,
		key,
		[]string{"openid", "urn:zitadel:iam:org:project:id:zitadel:aud"},
		profile.WithHTTPClient(&http.Client{Timeout: 10 * time.Second}),
	)
	if err != nil {
		return nil, err
	}
	return func() (string, error) {
		token, err := source.Token()
		if err != nil {
			return "", err
		}
		return token.AccessToken, nil
	}, nil
}
