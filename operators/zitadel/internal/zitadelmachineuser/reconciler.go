package zitadelmachineuser

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"regexp"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	identityv1alpha1 "github.com/tesserix/devai-sandbox-operator/operators/zitadel/api/v1alpha1"
	"github.com/tesserix/devai-sandbox-operator/operators/zitadel/internal/zitadelapi"
)

const (
	CredentialPAT          = "personalAccessToken"
	CredentialClientSecret = "clientSecret"
)

type MachineUser = zitadelapi.MachineUser

type Users interface {
	FindMachineUserByID(ctx context.Context, organization, userID string) (MachineUser, bool, error)
	FindMachineUserByName(ctx context.Context, organization, userName string) (MachineUser, bool, error)
	CreateMachineUser(ctx context.Context, organization, userName, name string) (MachineUser, error)
	UpdateMachineUser(ctx context.Context, organization, userID, name string) error
	ReplacePersonalAccessTokens(ctx context.Context, organization, userID string) (string, error)
	ReplaceClientSecret(ctx context.Context, organization, userID string) (clientID, secret string, err error)
}

// Secrets is the reviewed OpenBao store. It resolves a claim name to the one
// path an operator reviewed for it and refuses any other name, so a claim can
// never choose where its credential lands.
type Secrets interface {
	Latest(ctx context.Context, name string) (string, bool, error)
	Ensure(ctx context.Context, name, value string) error
}

type Reconciler struct {
	client  client.Client
	users   Users
	secrets Secrets
}

func NewReconciler(client client.Client, users Users, secrets Secrets) *Reconciler {
	return &Reconciler{client: client, users: users, secrets: secrets}
}

// Reconcile makes the machine user exist with JWT access tokens and makes sure
// its credential is published.
//
// Zitadel returns a PAT or client secret only once, at creation, so a failed
// handoff cannot be recovered by reading it back. OpenBao is therefore the
// source of truth: while the claim's path holds a value, nothing is issued.
// Missing previously published values require explicit recovery. A failed first
// handoff is retried by revoking orphan credentials before reissuing. A crash
// between issue and publish leaves a credential
// nobody holds, and the next reconcile revokes it. A failed OpenBao READ never
// counts as empty, or an outage would rotate a credential consumers are using.
func (r *Reconciler) Reconcile(ctx context.Context, key types.NamespacedName) (reconcileErr error) {
	claim := &identityv1alpha1.ZitadelMachineUser{}
	if err := r.client.Get(ctx, key, claim); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("get ZitadelMachineUser: %w", err)
	}
	if !claim.DeletionTimestamp.IsZero() {
		return nil
	}
	previous := claim.DeepCopy()
	defer func() {
		if reconcileErr != nil {
			claim.Status.ObservedGeneration = claim.Generation
			meta.SetStatusCondition(&claim.Status.Conditions, metav1.Condition{Type: "Ready", Status: metav1.ConditionFalse, Reason: "ReconcileFailed", Message: reconcileErr.Error(), ObservedGeneration: claim.Generation})
		}
		if !reflect.DeepEqual(previous.Status, claim.Status) {
			if err := r.client.Status().Update(ctx, claim); err != nil {
				reconcileErr = errors.Join(reconcileErr, fmt.Errorf("update ZitadelMachineUser status: %w", err))
			}
		}
	}()
	if err := validate(claim); err != nil {
		return err
	}
	_, published, err := r.secrets.Latest(ctx, claim.Name)
	if err != nil {
		return fmt.Errorf("read published credential: %w", err)
	}
	if !published && claim.Status.UserID != "" {
		return errors.New("previously published credential is missing; restore OpenBao or perform an explicitly approved rotation")
	}
	user, err := r.resolve(ctx, claim)
	if err != nil {
		return err
	}
	if user.AccessTokenType != zitadelapi.AccessTokenTypeJWT || user.Name != claim.Spec.DisplayName {
		if err := r.users.UpdateMachineUser(ctx, claim.Spec.Organization, user.ID, claim.Spec.DisplayName); err != nil {
			return fmt.Errorf("update Zitadel machine user: %w", err)
		}
	}
	clientID := claim.Status.ClientID
	if !published {
		var value string
		switch claim.Spec.Credential {
		case CredentialPAT:
			value, err = r.users.ReplacePersonalAccessTokens(ctx, claim.Spec.Organization, user.ID)
		case CredentialClientSecret:
			clientID, value, err = r.users.ReplaceClientSecret(ctx, claim.Spec.Organization, user.ID)
		}
		if err != nil {
			return fmt.Errorf("issue machine user credential: %w", err)
		}
		if err := r.secrets.Ensure(ctx, claim.Name, value); err != nil {
			return fmt.Errorf("publish machine user credential: %w", err)
		}
	}
	if claim.Spec.Credential == CredentialClientSecret && clientID == "" {
		// Zitadel's client ID for a machine secret is the user name.
		clientID = user.UserName
	}
	claim.Status.UserID = user.ID
	claim.Status.ClientID = clientID
	claim.Status.ObservedGeneration = claim.Generation
	meta.SetStatusCondition(&claim.Status.Conditions, metav1.Condition{Type: "Ready", Status: metav1.ConditionTrue, Reason: "Reconciled", Message: "Zitadel machine user is reconciled and its credential is published", ObservedGeneration: claim.Generation})
	return nil
}

func (r *Reconciler) resolve(ctx context.Context, claim *identityv1alpha1.ZitadelMachineUser) (MachineUser, error) {
	if claim.Status.UserID != "" {
		user, found, err := r.users.FindMachineUserByID(ctx, claim.Spec.Organization, claim.Status.UserID)
		if err != nil {
			return MachineUser{}, fmt.Errorf("lookup Zitadel machine user by id: %w", err)
		}
		if !found {
			return MachineUser{}, fmt.Errorf("recorded Zitadel machine user %q no longer exists", claim.Status.UserID)
		}
		return user, nil
	}
	user, found, err := r.users.FindMachineUserByName(ctx, claim.Spec.Organization, claim.Spec.UserName)
	if err != nil {
		return MachineUser{}, fmt.Errorf("lookup Zitadel machine user by name: %w", err)
	}
	if found {
		return user, nil
	}
	return r.users.CreateMachineUser(ctx, claim.Spec.Organization, claim.Spec.UserName, claim.Spec.DisplayName)
}

var userNamePattern = regexp.MustCompile(`^[a-z](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

func validate(claim *identityv1alpha1.ZitadelMachineUser) error {
	if claim.Spec.Organization == "" {
		return errors.New("spec.organization is required")
	}
	if !userNamePattern.MatchString(claim.Spec.UserName) {
		return errors.New("spec.userName must be a lowercase DNS label")
	}
	if claim.Spec.UserName != claim.Name {
		return errors.New("spec.userName must match the reviewed claim name")
	}
	if claim.Spec.DisplayName == "" {
		return errors.New("spec.displayName is required")
	}
	if claim.Spec.Credential != CredentialPAT && claim.Spec.Credential != CredentialClientSecret {
		return errors.New("spec.credential must be personalAccessToken or clientSecret")
	}
	return nil
}
