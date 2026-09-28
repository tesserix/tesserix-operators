package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/tesserix/devai-sandbox-operator/internal/openbaostore"
	analyticsv1alpha1 "github.com/tesserix/devai-sandbox-operator/operators/openpanel/api/v1alpha1"
	"github.com/tesserix/devai-sandbox-operator/operators/openpanel/internal/analyticsonboarding"
	"github.com/tesserix/devai-sandbox-operator/operators/openpanel/internal/openpanelapi"
)

func main() {
	var apiURL string
	var clientIDFile string
	var clientSecretFile string
	var baoAddress, baoRole, baoProducts, baoJWTFile string
	var watchNamespace string
	flag.StringVar(&apiURL, "openpanel-api-url", envOr("OPENPANEL_API_URL", "http://openpanel-api.openpanel.svc.cluster.local:3333"), "OpenPanel management API URL")
	flag.StringVar(&clientIDFile, "openpanel-client-id-file", envOr("OPENPANEL_CLIENT_ID_FILE", "/var/run/openpanel-root/client-id"), "path to the OpenPanel root client id")
	flag.StringVar(&clientSecretFile, "openpanel-client-secret-file", envOr("OPENPANEL_CLIENT_SECRET_FILE", "/var/run/openpanel-root/client-secret"), "path to the OpenPanel root client secret")
	flag.StringVar(&baoAddress, "openbao-address", envOr("OPENBAO_ADDR", "http://openbao.openbao.svc.cluster.local:8200"), "OpenBao address")
	flag.StringVar(&baoRole, "openbao-role", envOr("OPENBAO_ROLE", "analytics-onboarding-writer"), "OpenBao Kubernetes authentication role")
	flag.StringVar(&baoProducts, "openbao-products", envOr("OPENBAO_PRODUCTS", "devai,langfuse"), "reviewed production products allowed to publish client IDs")
	flag.StringVar(&baoJWTFile, "openbao-jwt-file", envOr("OPENBAO_JWT_FILE", "/var/run/secrets/kubernetes.io/serviceaccount/token"), "Kubernetes JWT file")
	// Existing image promotions may precede the argument cutover. These never enable GCP access.
	flag.String("gcp-project", "", "deprecated; ignored")
	flag.String("secret-manager-url", "", "deprecated; ignored")
	flag.String("secret-prefix", "", "deprecated; ignored")
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
	paths, err := reviewedPaths(baoProducts)
	if err != nil {
		panic(err)
	}
	storePaths := make(map[string]string, len(paths))
	for _, path := range paths {
		storePaths[path] = path
	}
	secrets, err := openbaostore.New(baoAddress, baoRole, func() (string, error) { return readSmallFile(baoJWTFile) }, storePaths, nil, &http.Client{Timeout: 10 * time.Second})
	if err != nil {
		panic(err)
	}
	reconciler := analyticsonboarding.NewReconciler(manager.GetClient(), projects, secrets, paths)
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

func reviewedPaths(products string) (map[string]string, error) {
	paths := map[string]string{}
	pattern := regexp.MustCompile(`^[a-z](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
	for _, item := range strings.Split(products, ",") {
		product := strings.TrimSpace(item)
		if !pattern.MatchString(product) {
			return nil, errors.New("invalid reviewed OpenBao product")
		}
		if _, exists := paths[product]; exists {
			return nil, errors.New("duplicate reviewed OpenBao product")
		}
		paths[product] = product + "/app/" + product + "-openpanel-client-id"
	}
	return paths, nil
}
