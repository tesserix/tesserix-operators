package evalonboarding

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	evalsv1alpha1 "github.com/tesserix/devai-sandbox-operator/operators/evals/api/v1alpha1"
	"github.com/tesserix/devai-sandbox-operator/operators/evals/internal/evalstore"
	"github.com/tesserix/devai-sandbox-operator/operators/evals/internal/langfuseapi"
)

const keyNote = "evals-onboarding-operator"

var (
	datasetNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)
	modalities         = map[string]bool{"agent": true, "retrieval": true, "ocr": true, "transcription": true}
)

type Langfuse interface {
	EnsureProject(ctx context.Context, recordedID, name string, metadata map[string]string) (langfuseapi.Project, error)
	ListAPIKeys(ctx context.Context, projectID string) ([]langfuseapi.APIKey, error)
	CreateAPIKey(ctx context.Context, projectID, note string) (langfuseapi.APIKey, error)
}

type Secrets interface {
	Latest(ctx context.Context, name string) (string, bool, error)
	Ensure(ctx context.Context, name, value string) error
}

type Datasets interface {
	Upsert(ctx context.Context, product string, datasets []evalstore.Dataset) error
}

type Reconciler struct {
	client       client.Client
	langfuse     Langfuse
	secrets      Secrets
	datasets     Datasets
	secretPrefix string
}

func NewReconciler(client client.Client, langfuse Langfuse, secrets Secrets, datasets Datasets, secretPrefix string) *Reconciler {
	return &Reconciler{client: client, langfuse: langfuse, secrets: secrets, datasets: datasets, secretPrefix: secretPrefix}
}

func (r *Reconciler) Reconcile(ctx context.Context, key types.NamespacedName) error {
	claim := &evalsv1alpha1.EvalOnboarding{}
	if err := r.client.Get(ctx, key, claim); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("get EvalOnboarding: %w", err)
	}
	datasets, err := validate(claim)
	if err != nil {
		return r.recordFailure(ctx, claim, err)
	}
	metadata := map[string]string{"managedBy": keyNote, "claim": claim.Name}
	project, err := r.langfuse.EnsureProject(ctx, claim.Status.ProjectID, strings.TrimSpace(claim.Spec.DisplayName), metadata)
	if err != nil {
		return r.recordFailure(ctx, claim, fmt.Errorf("reconcile Langfuse project: %w", err))
	}
	publicName := r.secretPrefix + claim.Name + "-langfuse-public-key"
	secretName := r.secretPrefix + claim.Name + "-langfuse-secret-key"
	if err := r.ensureAPIKey(ctx, project.ID, publicName, secretName); err != nil {
		return r.recordFailure(ctx, claim, fmt.Errorf("reconcile Langfuse API key: %w", err))
	}
	if err := r.datasets.Upsert(ctx, claim.Name, datasets); err != nil {
		return r.recordFailure(ctx, claim, fmt.Errorf("register eval datasets: %w", err))
	}

	claim.Status.ProjectID = project.ID
	claim.Status.PublicKeySecret = publicName
	claim.Status.SecretKeySecret = secretName
	claim.Status.DatasetsRegistered = len(datasets)
	claim.Status.ObservedGeneration = claim.Generation
	meta.SetStatusCondition(&claim.Status.Conditions, metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionTrue,
		Reason:             "Reconciled",
		Message:            "Langfuse project, API key secrets and eval datasets are reconciled",
		ObservedGeneration: claim.Generation,
	})
	if err := r.client.Status().Update(ctx, claim); err != nil {
		return fmt.Errorf("update EvalOnboarding status: %w", err)
	}
	return nil
}

// ensureAPIKey keeps the mirrored pair when Langfuse still lists its public half, and mints a new pair otherwise.
func (r *Reconciler) ensureAPIKey(ctx context.Context, projectID, publicName, secretName string) error {
	mirroredPublic, havePublic, err := r.secrets.Latest(ctx, publicName)
	if err != nil {
		return err
	}
	_, haveSecret, err := r.secrets.Latest(ctx, secretName)
	if err != nil {
		return err
	}
	if havePublic && haveSecret {
		keys, err := r.langfuse.ListAPIKeys(ctx, projectID)
		if err != nil {
			return err
		}
		for _, key := range keys {
			if key.PublicKey == mirroredPublic {
				return nil
			}
		}
	}
	created, err := r.langfuse.CreateAPIKey(ctx, projectID, keyNote)
	if err != nil {
		return err
	}
	if err := r.secrets.Ensure(ctx, secretName, created.SecretKey); err != nil {
		return err
	}
	return r.secrets.Ensure(ctx, publicName, created.PublicKey)
}

func (r *Reconciler) recordFailure(ctx context.Context, claim *evalsv1alpha1.EvalOnboarding, reconcileErr error) error {
	claim.Status.ObservedGeneration = claim.Generation
	meta.SetStatusCondition(&claim.Status.Conditions, metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionFalse,
		Reason:             "ReconcileFailed",
		Message:            reconcileErr.Error(),
		ObservedGeneration: claim.Generation,
	})
	if err := r.client.Status().Update(ctx, claim); err != nil {
		return fmt.Errorf("%w; update EvalOnboarding failure status: %v", reconcileErr, err)
	}
	var permanent interface{ Permanent() bool }
	if errors.As(reconcileErr, &permanent) && permanent.Permanent() {
		return nil
	}
	return reconcileErr
}

func validate(claim *evalsv1alpha1.EvalOnboarding) ([]evalstore.Dataset, error) {
	if strings.TrimSpace(claim.Spec.DisplayName) == "" {
		return nil, permanentError{"spec.displayName is required"}
	}
	datasets := make([]evalstore.Dataset, 0, len(claim.Spec.Datasets))
	seen := map[string]bool{}
	for _, dataset := range claim.Spec.Datasets {
		if !datasetNamePattern.MatchString(dataset.Name) {
			return nil, permanentError{fmt.Sprintf("spec.datasets contains invalid name %q", dataset.Name)}
		}
		if !modalities[dataset.Modality] {
			return nil, permanentError{fmt.Sprintf("spec.datasets[%s] has unsupported modality %q", dataset.Name, dataset.Modality)}
		}
		if seen[dataset.Name] {
			return nil, permanentError{fmt.Sprintf("spec.datasets contains duplicate name %q", dataset.Name)}
		}
		seen[dataset.Name] = true
		datasets = append(datasets, evalstore.Dataset{Name: dataset.Name, Modality: dataset.Modality, Description: strings.TrimSpace(dataset.Description)})
	}
	return datasets, nil
}

type permanentError struct{ message string }

func (e permanentError) Error() string   { return e.message }
func (e permanentError) Permanent() bool { return true }
