package sandboxsync

import (
	"context"
	"encoding/json"
	"fmt"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	resourceapi "k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	devai "github.com/tesserix/devai-sandbox-operator/api/v1alpha1"
)

type Reconciler struct {
	client client.Client
	scheme *runtime.Scheme
	image  string
}

func NewReconciler(client client.Client, scheme *runtime.Scheme, image string) *Reconciler {
	return &Reconciler{client: client, scheme: scheme, image: image}
}

func (r *Reconciler) Reconcile(ctx context.Context, key types.NamespacedName) error {
	claim := &devai.SandboxDataSync{}
	if err := r.client.Get(ctx, key, claim); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("get sandbox data sync: %w", err)
	}
	if err := validate(claim.Spec); err != nil {
		return err
	}
	policy, err := json.Marshal(claim.Spec.Tables)
	if err != nil {
		return fmt.Errorf("marshal sync policy: %w", err)
	}
	name := "sandbox-sync-" + claim.Name
	config := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: claim.Namespace}}
	_, err = controllerutil.CreateOrUpdate(ctx, r.client, config, func() error {
		config.Data = map[string]string{"policy.json": string(policy)}
		return controllerutil.SetControllerReference(claim, config, r.scheme)
	})
	if err != nil {
		return fmt.Errorf("reconcile sync policy configmap: %w", err)
	}
	cron := &batchv1.CronJob{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: claim.Namespace}}
	_, err = controllerutil.CreateOrUpdate(ctx, r.client, cron, func() error {
		cron.Spec = cronJobSpec(claim, r.image, name)
		return controllerutil.SetControllerReference(claim, cron, r.scheme)
	})
	if err != nil {
		return fmt.Errorf("reconcile sync cronjob: %w", err)
	}
	return nil
}

func cronJobSpec(claim *devai.SandboxDataSync, image, configName string) batchv1.CronJobSpec {
	backoff := int32(1)
	history := int32(3)
	deadline := int64(3600)
	readOnly := true
	nonRoot := true
	uid := int64(65532)
	return batchv1.CronJobSpec{Schedule: claim.Spec.Schedule, ConcurrencyPolicy: batchv1.ForbidConcurrent, SuccessfulJobsHistoryLimit: &history, FailedJobsHistoryLimit: &history, JobTemplate: batchv1.JobTemplateSpec{Spec: batchv1.JobSpec{BackoffLimit: &backoff, ActiveDeadlineSeconds: &deadline, Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{RestartPolicy: corev1.RestartPolicyNever, AutomountServiceAccountToken: boolPtr(false), SecurityContext: &corev1.PodSecurityContext{RunAsNonRoot: &nonRoot, RunAsUser: &uid}, Containers: []corev1.Container{{Name: "sync", Image: image, Args: []string{"--policy=/config/policy.json"}, SecurityContext: &corev1.SecurityContext{ReadOnlyRootFilesystem: &readOnly, AllowPrivilegeEscalation: boolPtr(false), Capabilities: &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}}}, Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource("250m"), corev1.ResourceMemory: resource("512Mi")}, Limits: corev1.ResourceList{corev1.ResourceMemory: resource("2Gi")}}, Env: []corev1.EnvVar{secretEnv("SOURCE_DATABASE_URL", claim.Spec.Source.SecretRef), secretEnv("TARGET_DATABASE_URL", claim.Spec.Target.SecretRef), secretEnv("ANONYMIZATION_SALT", claim.Spec.AnonymizationSaltSecretRef)}, VolumeMounts: []corev1.VolumeMount{{Name: "policy", MountPath: "/config", ReadOnly: true}}}}, Volumes: []corev1.Volume{{Name: "policy", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: configName}}}}}}}}}}
}

func secretEnv(name string, ref devai.SecretKeyReference) corev1.EnvVar {
	return corev1.EnvVar{Name: name, ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: ref.Name}, Key: ref.Key}}}
}
func boolPtr(value bool) *bool                   { return &value }
func resource(value string) resourceapi.Quantity { return resourceapi.MustParse(value) }

func validate(spec devai.SandboxDataSyncSpec) error {
	if spec.Schedule == "" {
		return fmt.Errorf("schedule is required")
	}
	if spec.Source.SecretRef.Name == "" || spec.Target.SecretRef.Name == "" || spec.AnonymizationSaltSecretRef.Name == "" {
		return fmt.Errorf("source, target, and anonymization salt secret references are required")
	}
	if len(spec.Tables) == 0 {
		return fmt.Errorf("at least one table rule is required")
	}
	allowed := map[string]bool{"email": true, "name": true, "hash": true, "redact": true, "preserve": true}
	for _, table := range spec.Tables {
		if table.Source == "" || table.Target == "" || len(table.Columns) == 0 {
			return fmt.Errorf("each table rule requires source, target, and columns")
		}
		for _, column := range table.Columns {
			if column.Name == "" || !allowed[column.Transform] {
				return fmt.Errorf("column %q has unsupported transform %q", column.Name, column.Transform)
			}
		}
	}
	return nil
}
