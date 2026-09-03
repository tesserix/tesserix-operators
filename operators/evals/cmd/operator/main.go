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
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/tesserix/devai-sandbox-operator/internal/secretmanager"
	evalsv1alpha1 "github.com/tesserix/devai-sandbox-operator/operators/evals/api/v1alpha1"
	"github.com/tesserix/devai-sandbox-operator/operators/evals/internal/evalonboarding"
	"github.com/tesserix/devai-sandbox-operator/operators/evals/internal/evalstore"
	"github.com/tesserix/devai-sandbox-operator/operators/evals/internal/langfuseapi"
)

func main() {
	var langfuseURL, publicKeyFile, secretKeyFile string
	var gcpProject, secretManagerURL, secretPrefix string
	var evalsDBURL, evalsDBPasswordFile, watchNamespace string
	flag.StringVar(&langfuseURL, "langfuse-url", envOr("LANGFUSE_URL", "http://langfuse-web.observability.svc.cluster.local:3000"), "Langfuse base URL")
	flag.StringVar(&publicKeyFile, "langfuse-public-key-file", envOr("LANGFUSE_PUBLIC_KEY_FILE", "/var/run/langfuse-org/public-key"), "path to the organization-scoped Langfuse public key")
	flag.StringVar(&secretKeyFile, "langfuse-secret-key-file", envOr("LANGFUSE_SECRET_KEY_FILE", "/var/run/langfuse-org/secret-key"), "path to the organization-scoped Langfuse secret key")
	flag.StringVar(&gcpProject, "gcp-project", envOr("GCP_PROJECT", "tesseracthub-480811"), "GCP project holding the mirrored Langfuse project keys")
	flag.StringVar(&secretManagerURL, "secret-manager-url", envOr("SECRET_MANAGER_URL", "https://secretmanager.googleapis.com"), "Google Secret Manager API URL")
	flag.StringVar(&secretPrefix, "secret-prefix", envOr("SECRET_PREFIX", "prod-"), "derived Secret Manager name prefix")
	flag.StringVar(&evalsDBURL, "evals-db-url", envOr("EVALS_DB_URL", "postgres://grader@global-postgres-pooler-rw.global.svc.cluster.local:5432/evals_db?sslmode=prefer"), "evals database URL without a password")
	flag.StringVar(&evalsDBPasswordFile, "evals-db-password-file", envOr("EVALS_DB_PASSWORD_FILE", "/var/run/evals-db/password"), "path to the evals database password")
	flag.StringVar(&watchNamespace, "watch-namespace", envOr("WATCH_NAMESPACE", "evals-operator"), "namespace containing eval onboarding claims")
	zapOptions := zap.Options{}
	zapOptions.BindFlags(flag.CommandLine)
	flag.Parse()
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&zapOptions)))

	scheme := clientgoscheme.Scheme
	if err := evalsv1alpha1.AddToScheme(scheme); err != nil {
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
	langfuse, err := langfuseapi.NewClient(langfuseURL, &http.Client{Timeout: 10 * time.Second}, fileCredentials(publicKeyFile, secretKeyFile))
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
	datasets, err := evalstore.NewStore(evalsDBURL, func() (string, error) {
		password, err := readSmallFile(evalsDBPasswordFile)
		return strings.TrimSpace(password), err
	})
	if err != nil {
		panic(err)
	}
	reconciler := evalonboarding.NewReconciler(manager.GetClient(), langfuse, secrets, datasets, secretPrefix)
	if err := manager.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		panic(err)
	}
	if err := manager.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		panic(err)
	}
	if err := ctrl.NewControllerManagedBy(manager).For(&evalsv1alpha1.EvalOnboarding{}).Complete(reconcile.Func(func(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
		return reconcile.Result{}, reconciler.Reconcile(ctx, request.NamespacedName)
	})); err != nil {
		panic(err)
	}
	if err := manager.Start(ctrl.SetupSignalHandler()); err != nil {
		panic(err)
	}
}

func fileCredentials(publicKeyPath, secretKeyPath string) langfuseapi.CredentialsSource {
	return func() (string, string, error) {
		publicKey, err := readSmallFile(publicKeyPath)
		if err != nil {
			return "", "", fmt.Errorf("read Langfuse public key: %w", err)
		}
		secretKey, err := readSmallFile(secretKeyPath)
		if err != nil {
			return "", "", fmt.Errorf("read Langfuse secret key: %w", err)
		}
		return strings.TrimSpace(publicKey), strings.TrimSpace(secretKey), nil
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
