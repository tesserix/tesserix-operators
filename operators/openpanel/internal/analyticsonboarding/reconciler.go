package analyticsonboarding

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	analyticsv1alpha1 "github.com/tesserix/devai-sandbox-operator/operators/openpanel/api/v1alpha1"
	"github.com/tesserix/devai-sandbox-operator/operators/openpanel/internal/openpanelapi"
)

type Projects interface {
	EnsureProject(ctx context.Context, recordedID string, input openpanelapi.ProjectInput) (openpanelapi.Result, error)
}

type Secrets interface {
	Ensure(ctx context.Context, name, value string) error
}

type Reconciler struct {
	client       client.Client
	projects     Projects
	secrets      Secrets
	secretPrefix string
}

func NewReconciler(client client.Client, projects Projects, secrets Secrets, secretPrefix string) *Reconciler {
	return &Reconciler{client: client, projects: projects, secrets: secrets, secretPrefix: secretPrefix}
}

func (r *Reconciler) Reconcile(ctx context.Context, key types.NamespacedName) error {
	claim := &analyticsv1alpha1.AnalyticsOnboarding{}
	if err := r.client.Get(ctx, key, claim); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("get AnalyticsOnboarding: %w", err)
	}
	input, err := projectInput(claim)
	if err != nil {
		return r.recordFailure(ctx, claim, err)
	}
	result, err := r.projects.EnsureProject(ctx, claim.Status.ProjectID, input)
	if err != nil {
		return r.recordFailure(ctx, claim, fmt.Errorf("reconcile OpenPanel project: %w", err))
	}
	if result.ProjectID == "" || result.ClientID == "" {
		return r.recordFailure(ctx, claim, errors.New("OpenPanel reconciliation returned an empty project or client id"))
	}
	secretName := r.secretPrefix + claim.Name + "-client-id"
	if err := r.secrets.Ensure(ctx, secretName, result.ClientID); err != nil {
		return r.recordFailure(ctx, claim, fmt.Errorf("reconcile client id in Secret Manager: %w", err))
	}

	claim.Status.ProjectID = result.ProjectID
	claim.Status.ClientIDSecret = secretName
	claim.Status.ObservedGeneration = claim.Generation
	meta.SetStatusCondition(&claim.Status.Conditions, metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionTrue,
		Reason:             "Reconciled",
		Message:            "OpenPanel project and client id secret are reconciled",
		ObservedGeneration: claim.Generation,
	})
	if err := r.client.Status().Update(ctx, claim); err != nil {
		return fmt.Errorf("update AnalyticsOnboarding status: %w", err)
	}
	return nil
}

func (r *Reconciler) recordFailure(ctx context.Context, claim *analyticsv1alpha1.AnalyticsOnboarding, reconcileErr error) error {
	claim.Status.ObservedGeneration = claim.Generation
	meta.SetStatusCondition(&claim.Status.Conditions, metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionFalse,
		Reason:             "ReconcileFailed",
		Message:            reconcileErr.Error(),
		ObservedGeneration: claim.Generation,
	})
	if err := r.client.Status().Update(ctx, claim); err != nil {
		return fmt.Errorf("%w; update AnalyticsOnboarding failure status: %v", reconcileErr, err)
	}
	var permanent interface{ Permanent() bool }
	if errors.As(reconcileErr, &permanent) && permanent.Permanent() {
		return nil
	}
	return reconcileErr
}

func projectInput(claim *analyticsv1alpha1.AnalyticsOnboarding) (openpanelapi.ProjectInput, error) {
	if strings.TrimSpace(claim.Spec.DisplayName) == "" {
		return openpanelapi.ProjectInput{}, permanentError{"spec.displayName is required"}
	}
	domain, err := webURL(claim.Spec.Domain)
	if err != nil {
		return openpanelapi.ProjectInput{}, permanentError{fmt.Sprintf("spec.domain: %v", err)}
	}
	if len(claim.Spec.CORS) == 0 {
		return openpanelapi.ProjectInput{}, permanentError{"spec.cors requires at least one origin"}
	}
	cors := make([]string, 0, len(claim.Spec.CORS))
	seen := map[string]bool{}
	for _, raw := range claim.Spec.CORS {
		origin, err := webURL(raw)
		if err != nil {
			return openpanelapi.ProjectInput{}, permanentError{fmt.Sprintf("spec.cors contains invalid origin %q: %v", raw, err)}
		}
		if seen[origin] {
			return openpanelapi.ProjectInput{}, permanentError{fmt.Sprintf("spec.cors contains duplicate origin %q", origin)}
		}
		seen[origin] = true
		cors = append(cors, origin)
	}
	types := append([]string(nil), claim.Spec.Types...)
	if len(types) == 0 {
		types = []string{"website"}
	}
	seenTypes := map[string]bool{}
	for _, projectType := range types {
		if projectType != "website" && projectType != "app" && projectType != "backend" {
			return openpanelapi.ProjectInput{}, permanentError{fmt.Sprintf("spec.types contains unsupported type %q", projectType)}
		}
		if seenTypes[projectType] {
			return openpanelapi.ProjectInput{}, permanentError{fmt.Sprintf("spec.types contains duplicate type %q", projectType)}
		}
		seenTypes[projectType] = true
	}
	return openpanelapi.ProjectInput{Name: strings.TrimSpace(claim.Spec.DisplayName), Domain: domain, CORS: cors, Types: types}, nil
}

func webURL(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", errors.New("must be an absolute http or https URL")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("must not contain credentials, query, or fragment")
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return "", errors.New("must be an origin without a path")
	}
	return strings.TrimSuffix(parsed.String(), "/"), nil
}

type permanentError struct{ message string }

func (e permanentError) Error() string   { return e.message }
func (e permanentError) Permanent() bool { return true }
