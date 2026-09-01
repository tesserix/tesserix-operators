package zitadelproject

import (
	"context"
	"errors"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	identityv1alpha1 "github.com/tesserix/devai-sandbox-operator/operators/zitadel/api/v1alpha1"
	"github.com/tesserix/devai-sandbox-operator/operators/zitadel/internal/zitadelapi"
)

type Project = zitadelapi.Project
type ProjectInput = zitadelapi.ProjectInput

type Projects interface {
	FindByID(ctx context.Context, organization, id string) (Project, bool, error)
	FindByName(ctx context.Context, organization, name string) (Project, bool, error)
	Create(ctx context.Context, organization string, input ProjectInput) (Project, error)
}

type Reconciler struct {
	client   client.Client
	scheme   *runtime.Scheme
	projects Projects
}

func NewReconciler(client client.Client, scheme *runtime.Scheme, projects Projects) *Reconciler {
	return &Reconciler{client: client, scheme: scheme, projects: projects}
}

func (r *Reconciler) Client() client.Client {
	return r.client
}

func (r *Reconciler) Reconcile(ctx context.Context, key types.NamespacedName) error {
	claim := &identityv1alpha1.ZitadelProject{}
	if err := r.client.Get(ctx, key, claim); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("get ZitadelProject: %w", err)
	}
	if err := validate(claim); err != nil {
		return err
	}

	project, err := r.resolve(ctx, claim)
	if err != nil {
		return err
	}
	if project.ID == "" {
		return errors.New("Zitadel project response has no id")
	}

	claim.Status.ProjectID = project.ID
	claim.Status.ObservedGeneration = claim.Generation
	meta.SetStatusCondition(&claim.Status.Conditions, metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionTrue,
		Reason:             "Reconciled",
		Message:            "Zitadel project is reconciled",
		ObservedGeneration: claim.Generation,
	})
	if err := r.client.Status().Update(ctx, claim); err != nil {
		return fmt.Errorf("update ZitadelProject status: %w", err)
	}
	return nil
}

func (r *Reconciler) resolve(ctx context.Context, claim *identityv1alpha1.ZitadelProject) (Project, error) {
	if claim.Status.ProjectID != "" {
		project, found, err := r.projects.FindByID(ctx, claim.Spec.Organization, claim.Status.ProjectID)
		if err != nil {
			return Project{}, fmt.Errorf("lookup Zitadel project by id: %w", err)
		}
		if !found {
			return Project{}, fmt.Errorf("recorded Zitadel project %q no longer exists", claim.Status.ProjectID)
		}
		return project, nil
	}

	project, found, err := r.projects.FindByName(ctx, claim.Spec.Organization, claim.Spec.DisplayName)
	if err != nil {
		return Project{}, fmt.Errorf("lookup Zitadel project by name: %w", err)
	}
	if found {
		return project, nil
	}
	project, err = r.projects.Create(ctx, claim.Spec.Organization, ProjectInput{DisplayName: claim.Spec.DisplayName})
	if err != nil {
		return Project{}, fmt.Errorf("create Zitadel project: %w", err)
	}
	return project, nil
}

func validate(claim *identityv1alpha1.ZitadelProject) error {
	if claim.Spec.DisplayName == "" {
		return errors.New("spec.displayName is required")
	}
	if claim.Spec.Organization == "" {
		return errors.New("spec.organization is required")
	}
	return nil
}
