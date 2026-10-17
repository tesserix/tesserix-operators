package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/zitadel/oidc/v3/pkg/client/profile"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/tesserix/devai-sandbox-operator/internal/openbaostore"
	identityv1alpha1 "github.com/tesserix/devai-sandbox-operator/operators/zitadel/api/v1alpha1"
	"github.com/tesserix/devai-sandbox-operator/operators/zitadel/internal/onboardingarchive"
	"github.com/tesserix/devai-sandbox-operator/operators/zitadel/internal/zitadelapi"
	"github.com/tesserix/devai-sandbox-operator/operators/zitadel/internal/zitadelapplication"
	"github.com/tesserix/devai-sandbox-operator/operators/zitadel/internal/zitadelmachineuser"
	"github.com/tesserix/devai-sandbox-operator/operators/zitadel/internal/zitadelorganization"
	"github.com/tesserix/devai-sandbox-operator/operators/zitadel/internal/zitadelproject"
)

func main() {
	var apiURL string
	var host string
	var machineKeyFile string
	var watchNamespace string
	var baoAddress, baoRole, baoMachineUsers, baoJWTFile string
	var organizationProducts, archiveBucket, onboardingURL, onboardingJWTFile string
	flag.StringVar(&apiURL, "zitadel-api-url", envOr("ZITADEL_API_URL", "http://zitadel.zitadel.svc.cluster.local:8080"), "Zitadel management API URL")
	flag.StringVar(&host, "zitadel-host", envOr("ZITADEL_HOST", "auth.tesserix.app"), "Zitadel instance host")
	flag.StringVar(&machineKeyFile, "zitadel-machine-key-file", envOr("ZITADEL_MACHINE_KEY_FILE", "/var/run/zitadel-admin/machine-key.json"), "path to the Zitadel machine-key JSON file")
	flag.StringVar(&watchNamespace, "watch-namespace", envOr("WATCH_NAMESPACE", "identity-operator"), "namespace containing Zitadel claims")
	flag.StringVar(&baoAddress, "openbao-address", envOr("OPENBAO_ADDR", "http://openbao.openbao.svc.cluster.local:8200"), "OpenBao address")
	flag.StringVar(&baoRole, "openbao-role", envOr("OPENBAO_ROLE", "zitadel-machine-user-writer"), "OpenBao Kubernetes authentication role")
	flag.StringVar(&baoMachineUsers, "openbao-machine-users", envOr("OPENBAO_MACHINE_USERS", ""), "reviewed product/environment/claim credentials; legacy product/claim paths remain compatible; empty disables ZitadelMachineUser")
	flag.StringVar(&baoJWTFile, "openbao-jwt-file", envOr("OPENBAO_JWT_FILE", "/var/run/secrets/kubernetes.io/serviceaccount/token"), "Kubernetes JWT file")
	flag.StringVar(&organizationProducts, "organization-products", envOr("ORGANIZATION_PRODUCTS", ""), "reviewed product/project-claim hospital bindings; empty disables organization provisioning")
	flag.StringVar(&archiveBucket, "onboarding-archive-bucket", envOr("ONBOARDING_ARCHIVE_BUCKET", ""), "private GCS bucket for held onboarding YAML; required for organizations")
	flag.StringVar(&onboardingURL, "onboarding-api-url", envOr("ONBOARDING_API_URL", ""), "trusted product enrollment API")
	flag.StringVar(&onboardingJWTFile, "onboarding-api-jwt-file", envOr("ONBOARDING_API_JWT_FILE", "/var/run/onboarding/token"), "audience-bound Kubernetes service-account JWT")
	flag.Parse()
	if watchNamespace == "" {
		panic("watch-namespace must not be empty")
	}

	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	_ = identityv1alpha1.AddToScheme(scheme)
	manager, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                  scheme,
		LeaderElection:          true,
		LeaderElectionID:        "zitadel-operator.identity.tesserix.app",
		LeaderElectionNamespace: watchNamespace,
		HealthProbeBindAddress:  ":8081",
		Cache:                   cache.Options{DefaultNamespaces: map[string]cache.Config{watchNamespace: {}}},
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
	if err := ctrl.NewControllerManagedBy(manager).For(&identityv1alpha1.ZitadelProject{}).WithEventFilter(predicate.GenerationChangedPredicate{}).Complete(periodicReconciler(manager.GetClient(), &identityv1alpha1.ZitadelProject{}, projectReconciler.Reconcile)); err != nil {
		panic(err)
	}
	if err := ctrl.NewControllerManagedBy(manager).For(&identityv1alpha1.ZitadelApplication{}).WithEventFilter(predicate.GenerationChangedPredicate{}).Complete(periodicReconciler(manager.GetClient(), &identityv1alpha1.ZitadelApplication{}, applicationReconciler.Reconcile)); err != nil {
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
		if err := ctrl.NewControllerManagedBy(manager).For(&identityv1alpha1.ZitadelMachineUser{}).WithEventFilter(predicate.GenerationChangedPredicate{}).Complete(periodicReconciler(manager.GetClient(), &identityv1alpha1.ZitadelMachineUser{}, machineUserReconciler.Reconcile)); err != nil {
			panic(err)
		}
	}
	productProjects, err := reviewedOrganizationProjects(organizationProducts)
	if err != nil {
		panic(err)
	}
	if len(productProjects) > 0 {
		if archiveBucket == "" {
			panic("organization provisioning requires onboarding-archive-bucket")
		}
		storageClient, err := google.DefaultClient(context.Background(), "https://www.googleapis.com/auth/devstorage.read_write")
		if err != nil {
			panic("GCS workload credentials unavailable")
		}
		storageClient.Timeout = 10 * time.Second
		archive, err := onboardingarchive.New(archiveBucket, "https://storage.googleapis.com", storageClient)
		if err != nil {
			panic(err)
		}
		activation, err := zitadelorganization.NewAPI(onboardingURL, func() (string, error) { return readSmallFile(onboardingJWTFile) })
		if err != nil {
			panic(err)
		}
		organizations := zitadelorganization.NewReconciler(manager.GetClient(), projects, archive, productProjects, activation)
		if err := ctrl.NewControllerManagedBy(manager).For(&identityv1alpha1.ZitadelOrganization{}).WithEventFilter(predicate.GenerationChangedPredicate{}).Complete(periodicReconciler(manager.GetClient(), &identityv1alpha1.ZitadelOrganization{}, organizations.Reconcile)); err != nil {
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
	var config struct {
		Type   string `json:"type"`
		UserID string `json:"userId"`
		KeyID  string `json:"keyId"`
		Key    string `json:"key"`
	}
	if err := json.Unmarshal(key, &config); err != nil {
		return nil, errors.New("invalid Zitadel machine-key JSON")
	}
	if config.Type != "serviceaccount" || config.UserID == "" || config.KeyID == "" || config.Key == "" {
		return nil, errors.New("zitadel machine key must identify a service account, key and user")
	}
	source, err := profile.NewJWTProfileTokenSource(
		context.Background(),
		issuer,
		config.UserID,
		config.KeyID,
		[]byte(config.Key),
		[]string{"openid", "urn:zitadel:iam:org:project:id:zitadel:aud"},
		profile.WithHTTPClient(&http.Client{Timeout: 10 * time.Second}),
	)
	if err != nil {
		return nil, err
	}
	cached := oauth2.ReuseTokenSource(nil, source)
	return func() (string, error) {
		token, err := cached.Token()
		if err != nil {
			return "", err
		}
		return token.AccessToken, nil
	}, nil
}

var dnsLabel = regexp.MustCompile(`^[a-z](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

// reviewedMachineUserPaths maps each reviewed claim name to
// <product>/<environment>/app/<claim>, preserving legacy two-part paths. A claim can only
// publish to the one path an operator listed for it.
func reviewedMachineUserPaths(pairs string) (map[string]string, error) {
	paths := map[string]string{}
	if strings.TrimSpace(pairs) == "" {
		return paths, nil
	}
	for _, item := range strings.Split(pairs, ",") {
		parts := strings.Split(strings.TrimSpace(item), "/")
		if len(parts) != 2 && len(parts) != 3 {
			return nil, errors.New("invalid reviewed machine user; want product/environment/claim")
		}
		product, claim := parts[0], parts[len(parts)-1]
		if !dnsLabel.MatchString(product) || !dnsLabel.MatchString(claim) {
			return nil, errors.New("invalid reviewed machine user identifier")
		}
		path := product + "/app/" + claim
		if len(parts) == 3 {
			environment := parts[1]
			if (environment != "prod" && environment != "dev" && environment != "uat") || !strings.HasPrefix(claim, product+"-") {
				return nil, errors.New("reviewed machine user requires a known environment and product-prefixed claim")
			}
			path = product + "/" + environment + "/app/" + claim
		}
		if _, exists := paths[claim]; exists {
			return nil, errors.New("duplicate reviewed machine user")
		}
		paths[claim] = path
	}
	return paths, nil
}

func readSmallFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() {
		// The file is read-only; closing it cannot change the credential.
		_ = file.Close()
	}()
	content, err := io.ReadAll(io.LimitReader(file, 4097))
	if err != nil {
		return "", err
	}
	if len(content) > 4096 {
		return "", errors.New("credential file exceeds 4096 bytes")
	}
	return string(content), nil
}

func periodicReconciler(kube client.Client, prototype client.Object, reconcileClaim func(context.Context, types.NamespacedName) error) reconcile.Func {
	return func(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		claim := prototype.DeepCopyObject().(client.Object)
		if err := kube.Get(ctx, request.NamespacedName, claim); err != nil {
			return reconcile.Result{}, client.IgnoreNotFound(err)
		}
		if !claim.GetDeletionTimestamp().IsZero() {
			return reconcile.Result{}, nil
		}
		if err := reconcileClaim(ctx, request.NamespacedName); err != nil {
			return reconcile.Result{}, err
		}
		return reconcile.Result{RequeueAfter: 5 * time.Minute}, nil
	}
}

func reviewedOrganizationProjects(pairs string) (map[string]string, error) {
	result := map[string]string{}
	if strings.TrimSpace(pairs) == "" {
		return result, nil
	}
	for _, pair := range strings.Split(pairs, ",") {
		parts := strings.Split(strings.TrimSpace(pair), "/")
		if len(parts) != 2 || !dnsLabel.MatchString(parts[0]) || !dnsLabel.MatchString(parts[1]) || result[parts[0]] != "" {
			return nil, errors.New("invalid or duplicate reviewed organization product/project")
		}
		result[parts[0]] = parts[1]
	}
	return result, nil
}
