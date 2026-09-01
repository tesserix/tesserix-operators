package zitadelapplication

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

	identityv1alpha1 "github.com/tesserix/devai-sandbox-operator/operators/zitadel/api/v1alpha1"
	"github.com/tesserix/devai-sandbox-operator/operators/zitadel/internal/zitadelapi"
)

type Application = zitadelapi.Application

// OIDCConfig mirrors zitadelapi.OIDCConfig for consumers of this package.
type OIDCConfig = zitadelapi.OIDCConfig
type ApplicationInput = zitadelapi.ApplicationInput

type Applications interface {
	FindApplicationByID(ctx context.Context, organization, projectID, appID string) (Application, bool, error)
	FindApplicationByName(ctx context.Context, organization, projectID, name string) (Application, bool, error)
	CreateApplication(ctx context.Context, organization, projectID string, input ApplicationInput) (Application, error)
	UpdateOIDCConfig(ctx context.Context, organization, projectID, appID string, input ApplicationInput) error
}

type Reconciler struct {
	client client.Client
	apps   Applications
}

func NewReconciler(client client.Client, apps Applications) *Reconciler {
	return &Reconciler{client: client, apps: apps}
}

func (r *Reconciler) Reconcile(ctx context.Context, key types.NamespacedName) error {
	claim := &identityv1alpha1.ZitadelApplication{}
	if err := r.client.Get(ctx, key, claim); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("get ZitadelApplication: %w", err)
	}
	if err := validate(claim); err != nil {
		return err
	}
	project := &identityv1alpha1.ZitadelProject{}
	if err := r.client.Get(ctx, types.NamespacedName{Namespace: claim.Namespace, Name: claim.Spec.ProjectRef.Name}, project); err != nil {
		return fmt.Errorf("get referenced ZitadelProject: %w", err)
	}
	if project.Status.ProjectID == "" {
		return errors.New("referenced ZitadelProject is not ready")
	}
	app, err := r.resolve(ctx, claim, project)
	if err != nil {
		return err
	}
	if app.ID == "" || app.ClientID == "" {
		return errors.New("Zitadel application response is missing an identifier")
	}
	claim.Status.AppID = app.ID
	claim.Status.ClientID = app.ClientID
	claim.Status.ObservedGeneration = claim.Generation
	meta.SetStatusCondition(&claim.Status.Conditions, metav1.Condition{Type: "Ready", Status: metav1.ConditionTrue, Reason: "Reconciled", Message: "Zitadel application is reconciled", ObservedGeneration: claim.Generation})
	if err := r.client.Status().Update(ctx, claim); err != nil {
		return fmt.Errorf("update ZitadelApplication status: %w", err)
	}
	return nil
}

func (r *Reconciler) resolve(ctx context.Context, claim *identityv1alpha1.ZitadelApplication, project *identityv1alpha1.ZitadelProject) (Application, error) {
	input := ApplicationInput{DisplayName: claim.Spec.DisplayName, AppType: claim.Spec.ApplicationType, AuthMethod: "none", ResponseType: "code", GrantType: "authorization_code", RedirectURIs: claim.Spec.RedirectURIs, PostLogoutRedirectURIs: claim.Spec.PostLogoutRedirectURIs}
	if claim.Status.AppID != "" {
		app, found, err := r.apps.FindApplicationByID(ctx, project.Spec.Organization, project.Status.ProjectID, claim.Status.AppID)
		if err != nil {
			return Application{}, fmt.Errorf("lookup Zitadel application by id: %w", err)
		}
		if !found {
			return Application{}, fmt.Errorf("recorded Zitadel application %q no longer exists", claim.Status.AppID)
		}
		return r.ensureConfig(ctx, project, app, input)
	}
	app, found, err := r.apps.FindApplicationByName(ctx, project.Spec.Organization, project.Status.ProjectID, claim.Spec.DisplayName)
	if err != nil {
		return Application{}, fmt.Errorf("lookup Zitadel application by name: %w", err)
	}
	if found {
		return r.ensureConfig(ctx, project, app, input)
	}
	return r.apps.CreateApplication(ctx, project.Spec.Organization, project.Status.ProjectID, input)
}

// ensureConfig heals adopted applications that predate idTokenUserinfoAssertion.
// Without the assertion Zitadel omits email/profile from the id_token, and the
// auth BFF then rejects every sign-in at the user upsert (email is required).
// Guarded on drift because Zitadel rejects a no-change update.
func (r *Reconciler) ensureConfig(ctx context.Context, project *identityv1alpha1.ZitadelProject, app Application, input ApplicationInput) (Application, error) {
	if app.OIDCConfig.IDTokenUserinfoAssertion {
		return app, nil
	}
	if err := r.apps.UpdateOIDCConfig(ctx, project.Spec.Organization, project.Status.ProjectID, app.ID, input); err != nil {
		return Application{}, fmt.Errorf("update Zitadel application OIDC config: %w", err)
	}
	app.OIDCConfig.IDTokenUserinfoAssertion = true
	return app, nil
}

func validate(claim *identityv1alpha1.ZitadelApplication) error {
	if claim.Spec.ProjectRef.Name == "" {
		return errors.New("spec.projectRef.name is required")
	}
	if claim.Spec.DisplayName == "" {
		return errors.New("spec.displayName is required")
	}
	if claim.Spec.ApplicationType != "native" && claim.Spec.ApplicationType != "user_agent" {
		return errors.New("spec.applicationType must be native or user_agent")
	}
	if err := validateURIs(claim.Spec.RedirectURIs, "spec.redirectUris", true, claim.Spec.ApplicationType); err != nil {
		return err
	}
	return validateURIs(claim.Spec.PostLogoutRedirectURIs, "spec.postLogoutRedirectUris", false, claim.Spec.ApplicationType)
}

func validateURIs(values []string, field string, required bool, applicationType string) error {
	if required && len(values) == 0 {
		return fmt.Errorf("%s is required", field)
	}
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != value || value == "" {
			return fmt.Errorf("%s contains an empty or padded URI", field)
		}
		parsed, err := url.ParseRequestURI(value)
		if err != nil || parsed.Scheme == "" {
			return fmt.Errorf("%s contains an invalid URI", field)
		}
		if applicationType == "user_agent" && (parsed.Scheme != "https" || parsed.Host == "") {
			return fmt.Errorf("%s must use an HTTPS URI for a user_agent application", field)
		}
		if _, ok := seen[value]; ok {
			return fmt.Errorf("%s contains a duplicate URI", field)
		}
		seen[value] = struct{}{}
	}
	return nil
}
