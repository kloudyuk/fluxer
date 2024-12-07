/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/fluxcd/pkg/apis/meta"
	"github.com/fluxcd/pkg/runtime/conditions"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	appsv1 "github.com/kloudyuk/fluxer/api/v1"

	helmv2 "github.com/fluxcd/helm-controller/api/v2"
	imagev1 "github.com/fluxcd/image-reflector-controller/api/v1beta2"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
)

const finalizer = "apps.kloudy.uk/finalizer"

var errRequeue = errors.New("requeue")

// FluxAppReconciler reconciles a FluxApp object
type FluxAppReconciler struct {
	client.Client
	Scheme          *runtime.Scheme
	ResourceManager *ResourceManager
}

// +kubebuilder:rbac:groups=apps.kloudy.uk,resources=fluxapps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps.kloudy.uk,resources=fluxapps/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=apps.kloudy.uk,resources=fluxapps/finalizers,verbs=update

// +kubebuilder:rbac:groups=image.toolkit.fluxcd.io,resources=imagerepositories;imagepolicies,verbs=get;list;watch;create;update;patch;delete;deletecollection
// +kubebuilder:rbac:groups=image.toolkit.fluxcd.io,resources=imagerepositories/status;imagepolicies/status,verbs=get

// +kubebuilder:rbac:groups=helm.toolkit.fluxcd.io,resources=helmreleases,verbs=get;list;watch;create;update;patch;delete;deletecollection
// +kubebuilder:rbac:groups=helm.toolkit.fluxcd.io,resources=helmreleases/status,verbs=get

// +kubebuilder:rbac:groups=source.toolkit.fluxcd.io,resources=helmrepositories,verbs=get;list;watch;create;update;patch;delete;deletecollection
// +kubebuilder:rbac:groups=source.toolkit.fluxcd.io,resources=helmrepositories/status,verbs=get

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the FluxApp object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.19.1/pkg/reconcile
func (r *FluxAppReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {

	// Setup logger
	log := log.FromContext(ctx)

	// Fetch the object
	app := &appsv1.FluxApp{}
	if err := r.Get(ctx, req.NamespacedName, app); err != nil {
		// Ignore NotFound errors
		err = client.IgnoreNotFound(err)
		if err != nil {
			log.Error(err, "unable to fetch FluxApp")
		}
		return ctrl.Result{}, err
	}

	// Handle object deletion
	if !app.ObjectMeta.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(app, finalizer) {
			// Handle clean up logic
			// TODO: Implement clean up logic here
			// Remove finalizer and update the object
			controllerutil.RemoveFinalizer(app, finalizer)
			if err := r.Update(ctx, app); err != nil {
				return ctrl.Result{}, err
			}
		}
		// Stop reconciliation as the object is being deleted
		return ctrl.Result{}, nil
	}

	// Add the finalizer if not present
	if !controllerutil.ContainsFinalizer(app, finalizer) {
		controllerutil.AddFinalizer(app, finalizer)
		if err := r.Update(ctx, app); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Always patch the status before returning
	p := client.MergeFrom(app.DeepCopy())
	defer func() {
		if err := r.Status().Patch(ctx, app, p); err != nil {
			log.Error(err, "unable to update FluxApp status")
		}
	}()

	// Handle the chart ImageRepository object
	if err := handleImageRepository(ctx, r, app); err != nil {
		if errors.Is(err, errRequeue) {
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, err
	}

	// Handle the chart ImagePolicy object
	if err := handleImagePolicy(ctx, r, app); err != nil {
		if errors.Is(err, errRequeue) {
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, err
	}

	// Handle the HelmRepository object
	if err := handleHelmRepository(ctx, r, app); err != nil {
		if errors.Is(err, errRequeue) {
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, err
	}

	// Handle the HelmRelease object
	if err := handleHelmRelease(ctx, r, app); err != nil {
		if errors.Is(err, errRequeue) {
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, err
	}

	// Return success
	return ctrl.Result{}, nil
}

// Handle Flux ImageRepository object
func handleImageRepository(ctx context.Context, r *FluxAppReconciler, app *appsv1.FluxApp) error {
	// Get the ImageRepository managed resource
	mr, err := r.ResourceManager.Get(ctx, app, imagev1.ImageRepositoryKind)
	if err != nil {
		return err
	}
	imageRepo := mr.Object.(*imagev1.ImageRepository)
	// Update the ImageRepository spec
	provider, err := providerFromURL(app.Spec.Chart.Repository)
	if err != nil {
		return err
	}
	parts := strings.Split(app.Spec.Chart.Repository, "://")
	if len(parts) != 2 {
		return fmt.Errorf("invalid chart repository URL: %s", app.Spec.Chart.Repository)
	}
	imageRepo.Spec = imagev1.ImageRepositorySpec{
		Image:    parts[1],
		Interval: metav1.Duration{Duration: 1 * time.Minute},
		Provider: provider,
	}
	// Set the app chart status based on the ImageRepository object
	if imageRepo.Spec.Image != "" {
		app.Status.Chart.Repository = "oci://" + path.Dir(imageRepo.Spec.Image)
		app.Status.Chart.Name = path.Base(imageRepo.Spec.Image)
	}
	// Update the resource
	return r.ResourceManager.Update(ctx, mr)
}

// Handle Flux ImagePolicy object
func handleImagePolicy(ctx context.Context, r *FluxAppReconciler, app *appsv1.FluxApp) error {
	// Get the ImagePolicy managed resource
	mr, err := r.ResourceManager.Get(ctx, app, imagev1.ImagePolicyKind)
	if err != nil {
		return err
	}
	imagePolicy := mr.Object.(*imagev1.ImagePolicy)
	// Update the spec
	imagePolicy.Spec = imagev1.ImagePolicySpec{
		ImageRepositoryRef: meta.NamespacedObjectReference{
			Name:      r.ResourceManager.ImagePolicyName(app),
			Namespace: app.Namespace,
		},
		Policy: imagev1.ImagePolicyChoice{
			SemVer: &imagev1.SemVerPolicy{
				Range: app.Spec.Chart.Version,
			},
		},
	}
	// Add the latest image to the app status
	if imagePolicy.Status.LatestImage != "" {
		parts := strings.Split(imagePolicy.Status.LatestImage, ":")
		if len(parts) != 2 {
			return fmt.Errorf("invalid image reference: %s", imagePolicy.Status.LatestImage)
		}
		app.Status.Chart.Version = parts[1]
	}
	// Update the resource
	return r.ResourceManager.Update(ctx, mr)
}

// Handle Flux HelmRepository object
func handleHelmRepository(ctx context.Context, r *FluxAppReconciler, app *appsv1.FluxApp) error {
	// Get the HelmRepository managed resource
	mr, err := r.ResourceManager.Get(ctx, app, sourcev1.HelmRepositoryKind)
	if err != nil {
		return err
	}
	helmRepository := mr.Object.(*sourcev1.HelmRepository)
	// Update the spec
	provider, err := providerFromURL(app.Status.Chart.Repository)
	if err != nil {
		return err
	}
	helmRepository.Spec = sourcev1.HelmRepositorySpec{
		URL:      app.Status.Chart.Repository,
		Type:     "oci",
		Provider: provider,
	}
	// Update the resource
	return r.ResourceManager.Update(ctx, mr)
}

// Handle Flux HelmRelease object
func handleHelmRelease(ctx context.Context, r *FluxAppReconciler, app *appsv1.FluxApp) error {
	// If we don't have the info needed for the HelmRelease, requeue
	if app.Status.Chart.Repository == "" || app.Status.Chart.Name == "" || app.Status.Chart.Version == "" {
		return errRequeue
	}
	// Get the HelmRelease managed resource
	mr, err := r.ResourceManager.Get(ctx, app, helmv2.HelmReleaseKind)
	if err != nil {
		return err
	}
	helmRelease := mr.Object.(*helmv2.HelmRelease)
	// Update the spec
	targetNS := app.Spec.TargetNamespace
	if targetNS == "" {
		targetNS = app.Namespace
	}
	helmRelease.Spec = helmv2.HelmReleaseSpec{
		Chart: &helmv2.HelmChartTemplate{
			Spec: helmv2.HelmChartTemplateSpec{
				Chart:   app.Status.Chart.Name,
				Version: app.Status.Chart.Version,
				SourceRef: helmv2.CrossNamespaceObjectReference{
					Kind:      "HelmRepository",
					Name:      r.ResourceManager.HelmRepositoryName(app),
					Namespace: app.Namespace,
				},
			},
		},
		Interval:        metav1.Duration{Duration: 1 * time.Minute},
		ReleaseName:     app.Name,
		TargetNamespace: targetNS,
		DriftDetection: &helmv2.DriftDetection{
			Mode: helmv2.DriftDetectionEnabled,
			Ignore: []helmv2.IgnoreRule{
				{
					Paths: []string{"/spec/replicas"},
				},
			},
		},
		Install: &helmv2.Install{
			Replace:         true,
			CRDs:            helmv2.CreateReplace,
			CreateNamespace: true,
		},
		Upgrade: &helmv2.Upgrade{
			CRDs: helmv2.CreateReplace,
		},
	}
	conditions.SetMirror(app, meta.ReadyCondition, helmRelease, conditions.WithFallbackValue(false, meta.ProgressingReason, "HelmRelease is not ready"))
	return r.ResourceManager.Update(ctx, mr)
}

func providerFromURL(s string) (string, error) {
	u, err := url.Parse(s)
	if err != nil {
		return "", err
	}
	var provider string
	switch {
	case strings.Contains(u.Host, "amazonaws.com"):
		provider = "aws"
	case strings.Contains(u.Host, "azurecr.io"):
		provider = "azure"
	case strings.Contains(u.Host, "gcr.io"):
		provider = "gcp"
	default:
		provider = "generic"
	}
	return provider, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *FluxAppReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&appsv1.FluxApp{}).
		Owns(&helmv2.HelmRelease{}).
		Owns(&imagev1.ImagePolicy{}).
		Owns(&imagev1.ImageRepository{}).
		Owns(&sourcev1.HelmRepository{}).
		Named("fluxapp").
		Complete(r)
}
