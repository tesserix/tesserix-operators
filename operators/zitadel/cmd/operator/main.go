package main

import (
	"context"
	"errors"
	"flag"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/zitadel/oidc/v3/pkg/client/profile"

	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/tesserix/devai-sandbox-operator/internal/openbaostore"
	identityv1alpha1 "github.com/tesserix/devai-sandbox-operator/operators/zitadel/api/v1alpha1"
	"github.com/tesserix/devai-sandbox-operator/operators/zitadel/internal/zitadelapi"
	"github.com/tesserix/devai-sandbox-operator/operators/zitadel/internal/zitadelapplication"
	"github.com/tesserix/devai-sandbox-operator/operators/zitadel/internal/zitadelmachineuser"
	"github.com/tesserix/devai-sandbox-operator/operators/zitadel/internal/zitadelproject"
)

func main() {
	var apiURL string
	var host string
	var machineKeyFile string
	var watchNamespace string
	var baoAddress, baoRole, baoMachineUsers, baoJWTFile string
	flag.StringVar(&apiURL, "zitadel-api-url", envOr("ZITADEL_API_URL", "http://zitadel.zitadel.svc.cluster.local:8080"), "Zitadel management API URL")
	flag.StringVar(&host, "zitadel-host", envOr("ZITADEL_HOST", "auth.tesserix.app"), "Zitadel instance host")
	flag.StringVar(&machineKeyFile, "zitadel-machine-key-file", envOr("ZITADEL_MACHINE_KEY_FILE", "/var/run/zitadel-admin/machine-key.json"), "path to the Zitadel machine-key JSON file")
	flag.StringVar(&watchNamespace, "watch-namespace", envOr("WATCH_NAMESPACE", "identity-operator"), "namespace containing Zitadel claims")
	flag.StringVar(&baoAddress, "openbao-address", envOr("OPENBAO_ADDR", "http://openbao.openbao.svc.cluster.local:8200"), "OpenBao address")
	flag.StringVar(&baoRole, "openbao-role", envOr("OPENBAO_ROLE", "zitadel-machine-user-writer"), "OpenBao Kubernetes authentication role")
	flag.StringVar(&baoMachineUsers, "openbao-machine-users", envOr("OPENBAO_MACHINE_USERS", ""), "reviewed product/claim pairs whose credentials may be published, e.g. kora/kora-account-deleter; empty disables ZitadelMachineUser")
	flag.StringVar(&baoJWTFile, "openbao-jwt-file", envOr("OPENBAO_JWT_FILE", "/var/run/secrets/kubernetes.io/serviceaccount/token"), "Kubernetes JWT file")
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
	machineUserPaths, err := reviewedMachineUserPaths(baoMachineUsers)
	if err != nil {
		panic(err)
	}
	// Opt-in: with no reviewed paths there is nowhere a credential may land,
	// so the controller is not started and machine-user claims stay unreconciled.
	if len(machineUserPaths) > 0 {
		secrets, err := openbaostore.New(baoAddress, baoRole, func() (string, error) { return readSmallFile(baoJWTFile) }, machineUserPaths, nil, &http.Client{Timeout: 10 * time.Second})
		if err != nil {
			panic(err)
		}
		machineUserReconciler := zitadelmachineuser.NewReconciler(manager.GetClient(), projects, secrets)
		if err := ctrl.NewControllerManagedBy(manager).For(&identityv1alpha1.ZitadelMachineUser{}).Complete(reconcile.Func(func(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
			return reconcile.Result{}, machineUserReconciler.Reconcile(ctx, request.NamespacedName)
		})); err != nil {
			panic(err)
		}
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

var dnsLabel = regexp.MustCompile(`^[a-z](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

// reviewedMachineUserPaths maps each reviewed claim name to
// <product>/app/<claim>. The claim name is the secret name, so a claim can only
// publish to the one path an operator listed for it.
func reviewedMachineUserPaths(pairs string) (map[string]string, error) {
	paths := map[string]string{}
	if strings.TrimSpace(pairs) == "" {
		return paths, nil
	}
	for _, item := range strings.Split(pairs, ",") {
		product, claim, ok := strings.Cut(strings.TrimSpace(item), "/")
		if !ok || !dnsLabel.MatchString(product) || !dnsLabel.MatchString(claim) {
			return nil, errors.New("invalid reviewed machine user; want product/claim")
		}
		if _, exists := paths[claim]; exists {
			return nil, errors.New("duplicate reviewed machine user")
		}
		paths[claim] = product + "/app/" + claim
	}
	return paths, nil
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
