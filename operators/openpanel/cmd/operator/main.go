package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"golang.org/x/oauth2/google"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	analyticsv1alpha1 "github.com/tesserix/devai-sandbox-operator/operators/openpanel/api/v1alpha1"
	"github.com/tesserix/devai-sandbox-operator/operators/openpanel/internal/analyticsonboarding"
	"github.com/tesserix/devai-sandbox-operator/operators/openpanel/internal/openpanelapi"
	"github.com/tesserix/devai-sandbox-operator/operators/openpanel/internal/secretmanager"
)

func main() {
	var apiURL string
	var clientIDFile string
	var clientSecretFile string
	var gcpProject string
	var secretManagerURL string
	var secretPrefix string
	var watchNamespace string
	flag.StringVar(&apiURL, "openpanel-api-url", envOr("OPENPANEL_API_URL", "http://openpanel-api.openpanel.svc.cluster.local:3333"), "OpenPanel management API URL")
	flag.StringVar(&clientIDFile, "openpanel-client-id-file", envOr("OPENPANEL_CLIENT_ID_FILE", "/var/run/openpanel-root/client-id"), "path to the OpenPanel root client id")
	flag.StringVar(&clientSecretFile, "openpanel-client-secret-file", envOr("OPENPANEL_CLIENT_SECRET_FILE", "/var/run/openpanel-root/client-secret"), "path to the OpenPanel root client secret")
	flag.StringVar(&gcpProject, "gcp-project", envOr("GCP_PROJECT", "tesseracthub-480811"), "GCP project containing analytics client id secrets")
	flag.StringVar(&secretManagerURL, "secret-manager-url", envOr("SECRET_MANAGER_URL", "https://secretmanager.googleapis.com"), "Google Secret Manager API URL")
	flag.StringVar(&secretPrefix, "secret-prefix", envOr("SECRET_PREFIX", "prod-openpanel-"), "derived Secret Manager name prefix")
	flag.StringVar(&watchNamespace, "watch-namespace", envOr("WATCH_NAMESPACE", "analytics-operator"), "namespace containing analytics onboarding claims")
	flag.Parse()

	scheme := clientgoscheme.Scheme
	if err := analyticsv1alpha1.AddToScheme(scheme); err != nil {
		panic(err)
	}
	manager, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		HealthProbeBindAddress: ":8081",
		Cache:                  cache.Options{DefaultNamespaces: map[string]cache.Config{watchNamespace: {}}},
	})
	if err != nil {
		panic(err)
	}
	openPanelHTTP := &http.Client{Timeout: 10 * time.Second}
	projects, err := openpanelapi.NewClient(apiURL, openPanelHTTP, fileCredentials(clientIDFile, clientSecretFile))
	if err != nil {
		panic(err)
	}
	secretHTTP, err := google.DefaultClient(context.Background(), "https://www.googleapis.com/auth/cloud-platform")
	if err != nil {
		panic(fmt.Errorf("create authenticated Secret Manager client: %w", err))
	}
	secretHTTP.Timeout = 10 * time.Second
	secrets, err := secretmanager.NewStore(secretManagerURL, gcpProject, secretHTTP)
	if err != nil {
		panic(err)
	}
	reconciler := analyticsonboarding.NewReconciler(manager.GetClient(), projects, secrets, secretPrefix)
	if err := manager.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		panic(err)
	}
	if err := manager.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		panic(err)
	}
	if err := ctrl.NewControllerManagedBy(manager).For(&analyticsv1alpha1.AnalyticsOnboarding{}).Complete(reconcile.Func(func(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
		return reconcile.Result{}, reconciler.Reconcile(ctx, request.NamespacedName)
	})); err != nil {
		panic(err)
	}
	if err := manager.Start(ctrl.SetupSignalHandler()); err != nil {
		panic(err)
	}
}

func fileCredentials(clientIDPath, clientSecretPath string) openpanelapi.CredentialsSource {
	return func() (string, string, error) {
		clientID, err := readSmallFile(clientIDPath)
		if err != nil {
			return "", "", fmt.Errorf("read OpenPanel client id: %w", err)
		}
		clientSecret, err := readSmallFile(clientSecretPath)
		if err != nil {
			return "", "", fmt.Errorf("read OpenPanel client secret: %w", err)
		}
		return strings.TrimSpace(clientID), strings.TrimSpace(clientSecret), nil
	}
}

func readSmallFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, 4097))
	if err != nil {
		return "", err
	}
	if len(content) > 4096 {
		return "", errors.New("credential file exceeds 4096 bytes")
	}
	return string(content), nil
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
