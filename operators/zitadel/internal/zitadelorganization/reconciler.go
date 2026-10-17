package zitadelorganization

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strings"

	"github.com/google/uuid"
	identity "github.com/tesserix/devai-sandbox-operator/operators/zitadel/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
)

var sourceID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,200}$`)

type Identity interface {
	EnsureOrganization(context.Context, identity.ZitadelOrganizationSpec, string, string) (string, error)
}
type Activation interface {
	Complete(context.Context, identity.ZitadelOrganizationSpec, string, string) error
}

type Archive interface {
	Put(context.Context, string, []byte) (string, error)
}

type Reconciler struct {
	kube       client.Client
	identity   Identity
	archive    Archive
	projects   map[string]string
	activation Activation
}

func NewReconciler(kube client.Client, remote Identity, archive Archive, projects map[string]string, activation Activation) *Reconciler {
	return &Reconciler{kube: kube, identity: remote, archive: archive, projects: projects, activation: activation}
}

func (r *Reconciler) Reconcile(ctx context.Context, key types.NamespacedName) (reconcileErr error) {
	claim := &identity.ZitadelOrganization{}
	if err := r.kube.Get(ctx, key, claim); err != nil {
		return client.IgnoreNotFound(err)
	}
	if !claim.DeletionTimestamp.IsZero() {
		return nil
	}
	before := claim.DeepCopy()
	defer func() {
		claim.Status.ObservedGeneration = claim.Generation
		if reconcileErr != nil {
			meta.SetStatusCondition(&claim.Status.Conditions, metav1.Condition{Type: "Ready", Status: metav1.ConditionFalse, Reason: "ReconcileFailed", Message: "identity onboarding pending; inspect operator logs", ObservedGeneration: claim.Generation})
		}
		if !reflect.DeepEqual(before.Status, claim.Status) {
			if e := r.kube.Status().Update(ctx, claim); e != nil {
				reconcileErr = errors.Join(reconcileErr, fmt.Errorf("update organization status: %w", e))
			}
		}
	}()
	projectName, ok := r.projects[claim.Spec.Product]
	tenant, err := uuid.Parse(claim.Spec.TenantID)
	if !sourceID.MatchString(claim.Spec.InitialAdminUserID) || !ok || err != nil || tenant == uuid.Nil || tenant.String() != claim.Spec.TenantID || claim.Name != claim.Spec.Product+"-"+claim.Spec.TenantID || strings.TrimSpace(claim.Spec.DisplayName) == "" || len(claim.Spec.DisplayName) > 200 {
		return errors.New("invalid or unreviewed product/tenant organization binding")
	}
	if claim.Spec.PreserveLoginPolicy && claim.Spec.OrganizationID == "" {
		return errors.New("preserveLoginPolicy requires explicit adoption")
	}
	project := &identity.ZitadelProject{}
	if err := r.kube.Get(ctx, types.NamespacedName{Namespace: key.Namespace, Name: projectName}, project); err != nil {
		return fmt.Errorf("get parent project: %w", err)
	}
	if project.Status.ProjectID == "" || project.Status.ObservedGeneration != project.Generation || !meta.IsStatusConditionTrue(project.Status.Conditions, "Ready") {
		return errors.New("parent project is not current and ready")
	}
	if r.archive == nil {
		return errors.New("organization archive is required")
	}
	// Only the typed spec enters the archive: annotations may contain arbitrary customer data.
	intent := struct {
		APIVersion string                           `json:"apiVersion"`
		Kind       string                           `json:"kind"`
		Spec       identity.ZitadelOrganizationSpec `json:"spec"`
	}{identity.GroupVersion.String(), "ZitadelOrganization", claim.Spec}
	raw, err := yaml.Marshal(intent)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(raw)
	prefix := claim.Spec.Product + "/prod/tenants/" + claim.Spec.TenantID + "/revisions/" + hex.EncodeToString(sum[:])
	alreadyArchived := claim.Status.ArchiveObject == prefix+"/identity.yaml" && claim.Status.ArchiveGeneration != "" && claim.Status.ObservedGeneration == claim.Generation
	if !alreadyArchived {
		if _, err := r.archive.Put(ctx, prefix+"/intent.yaml", raw); err != nil {
			return fmt.Errorf("archive organization intent: %w", err)
		}
	}
	orgID, err := r.identity.EnsureOrganization(ctx, claim.Spec, project.Spec.Organization, project.Status.ProjectID)
	if err != nil {
		return fmt.Errorf("ensure organization: %w", err)
	}
	if orgID == "" || (claim.Status.OrganizationID != "" && claim.Status.OrganizationID != orgID) {
		return errors.New("organization identity changed")
	}
	if r.activation == nil {
		return errors.New("product activation API is required")
	}
	if err := r.activation.Complete(ctx, claim.Spec, orgID, project.Status.ProjectID); err != nil {
		return fmt.Errorf("activate product tenant: %w", err)
	}
	claim.Status.Activated = true
	receipt := struct {
		Spec           identity.ZitadelOrganizationSpec `json:"spec"`
		OrganizationID string                           `json:"organizationId"`
		ProjectID      string                           `json:"projectId"`
	}{claim.Spec, orgID, project.Status.ProjectID}
	raw, err = yaml.Marshal(receipt)
	if err != nil {
		return err
	}
	object := prefix + "/identity.yaml"
	generation := claim.Status.ArchiveGeneration
	if !alreadyArchived {
		generation, err = r.archive.Put(ctx, object, raw)
		if err != nil {
			return fmt.Errorf("archive organization receipt: %w", err)
		}
	}
	claim.Status.OrganizationID = orgID
	claim.Status.ProjectID = project.Status.ProjectID
	claim.Status.ArchiveObject = object
	claim.Status.ArchiveGeneration = generation
	meta.SetStatusCondition(&claim.Status.Conditions, metav1.Condition{Type: "Ready", Status: metav1.ConditionTrue, Reason: "IdentityReconciled", Message: "Identity, product enrollment, initial membership and archive reconciled", ObservedGeneration: claim.Generation})
	return nil
}
