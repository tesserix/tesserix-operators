package zitadelproject

import (
	"context"
	"errors"
	"fmt"
	"strings"

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
type UserGrant = zitadelapi.UserGrant

type Projects interface {
	FindByID(ctx context.Context, organization, id string) (Project, bool, error)
	FindByName(ctx context.Context, organization, name string) (Project, bool, error)
	Create(ctx context.Context, organization string, input ProjectInput) (Project, error)
	UpdateProject(ctx context.Context, organization, projectID, name string, roleCheck bool) error
	ListRoles(ctx context.Context, organization, projectID string) ([]string, error)
	AddRole(ctx context.Context, organization, projectID, key string) error
	FindUserByEmail(ctx context.Context, organization, email string) (string, bool, error)
	ListGrants(ctx context.Context, organization, projectID string) ([]UserGrant, error)
	AddGrant(ctx context.Context, organization, userID, projectID string, roles []string) error
	UpdateGrant(ctx context.Context, organization, userID, grantID string, roles []string) error
	RemoveGrant(ctx context.Context, organization, userID, grantID string) error
}

const defaultRole = "member"

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
		return r.recordFailure(ctx, claim, err)
	}

	project, err := r.resolve(ctx, claim)
	if err != nil {
		return r.recordFailure(ctx, claim, err)
	}
	if project.ID == "" {
		return r.recordFailure(ctx, claim, errors.New("Zitadel project response has no id"))
	}
	if err := r.ensureAccess(ctx, claim, project); err != nil {
		return r.recordFailure(ctx, claim, err)
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

func (r *Reconciler) recordFailure(ctx context.Context, claim *identityv1alpha1.ZitadelProject, reconcileErr error) error {
	claim.Status.ObservedGeneration = claim.Generation
	meta.SetStatusCondition(&claim.Status.Conditions, metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionFalse,
		Reason:             "ReconcileFailed",
		Message:            reconcileErr.Error(),
		ObservedGeneration: claim.Generation,
	})
	if err := r.client.Status().Update(ctx, claim); err != nil {
		return fmt.Errorf("%w; update ZitadelProject failure status: %v", reconcileErr, err)
	}
	return reconcileErr
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
	access := claim.Spec.Access
	if access == nil {
		return nil
	}
	switch access.Mode {
	case "public":
		if len(access.Members) > 0 {
			return errors.New("spec.access.members is only valid with mode restricted")
		}
	case "restricted":
		if len(access.Members) == 0 {
			return errors.New("spec.access.mode restricted requires at least one member")
		}
	default:
		return fmt.Errorf("spec.access.mode must be public or restricted, got %q", access.Mode)
	}
	seen := map[string]bool{}
	for _, member := range access.Members {
		email := strings.ToLower(strings.TrimSpace(member.Email))
		if email == "" || !strings.Contains(email, "@") {
			return fmt.Errorf("spec.access.members has invalid email %q", member.Email)
		}
		if seen[email] {
			return fmt.Errorf("spec.access.members lists %q more than once", email)
		}
		seen[email] = true
	}
	return nil
}

// ensureAccess reconciles who may authenticate. Restricted mode turns on
// Zitadel's projectRoleCheck and makes the member list the exact set of
// grants; public mode turns the check off and leaves grants alone.
func (r *Reconciler) ensureAccess(ctx context.Context, claim *identityv1alpha1.ZitadelProject, project Project) error {
	organization := claim.Spec.Organization
	access := claim.Spec.Access
	restricted := access != nil && access.Mode == "restricted"

	if project.ProjectRoleCheck != restricted {
		if err := r.projects.UpdateProject(ctx, organization, project.ID, project.Name, restricted); err != nil {
			return fmt.Errorf("update Zitadel project role check: %w", err)
		}
	}
	if !restricted {
		return nil
	}

	existingRoles, err := r.projects.ListRoles(ctx, organization, project.ID)
	if err != nil {
		return fmt.Errorf("list Zitadel project roles: %w", err)
	}
	haveRole := map[string]bool{}
	for _, key := range existingRoles {
		haveRole[key] = true
	}
	for _, key := range neededRoles(access.Members) {
		if haveRole[key] {
			continue
		}
		if err := r.projects.AddRole(ctx, organization, project.ID, key); err != nil {
			return fmt.Errorf("add Zitadel project role %q: %w", key, err)
		}
	}

	grants, err := r.projects.ListGrants(ctx, organization, project.ID)
	if err != nil {
		return fmt.Errorf("list Zitadel user grants: %w", err)
	}
	grantByUser := map[string]UserGrant{}
	for _, grant := range grants {
		grantByUser[grant.UserID] = grant
	}

	desired := map[string]bool{}
	for _, member := range access.Members {
		email := strings.ToLower(strings.TrimSpace(member.Email))
		userID, found, err := r.projects.FindUserByEmail(ctx, organization, email)
		if err != nil {
			return fmt.Errorf("lookup Zitadel user %q: %w", email, err)
		}
		if !found {
			return fmt.Errorf("Zitadel user %q not found; members must sign up before being granted access", email)
		}
		desired[userID] = true
		roles := memberRoles(member)
		if existing, ok := grantByUser[userID]; ok {
			if !sameRoles(existing.RoleKeys, roles) {
				if err := r.projects.UpdateGrant(ctx, organization, userID, existing.ID, roles); err != nil {
					return fmt.Errorf("update Zitadel grant for %q: %w", email, err)
				}
			}
			continue
		}
		if err := r.projects.AddGrant(ctx, organization, userID, project.ID, roles); err != nil {
			return fmt.Errorf("grant Zitadel access to %q: %w", email, err)
		}
	}

	for userID, grant := range grantByUser {
		if desired[userID] {
			continue
		}
		if err := r.projects.RemoveGrant(ctx, organization, userID, grant.ID); err != nil {
			return fmt.Errorf("remove stale Zitadel grant %q: %w", grant.ID, err)
		}
	}
	return nil
}

func memberRoles(member identityv1alpha1.ZitadelProjectMember) []string {
	if len(member.Roles) == 0 {
		return []string{defaultRole}
	}
	return member.Roles
}

func neededRoles(members []identityv1alpha1.ZitadelProjectMember) []string {
	seen := map[string]bool{}
	var keys []string
	for _, member := range members {
		for _, key := range memberRoles(member) {
			if !seen[key] {
				seen[key] = true
				keys = append(keys, key)
			}
		}
	}
	return keys
}

func sameRoles(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	set := map[string]bool{}
	for _, key := range a {
		set[key] = true
	}
	for _, key := range b {
		if !set[key] {
			return false
		}
	}
	return true
}
