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

// FluxAppReconciler reconciles a FluxApp object
type FluxAppReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=apps.kloudy.uk,resources=fluxapps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps.kloudy.uk,resources=fluxapps/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=apps.kloudy.uk,resources=fluxapps/finalizers,verbs=update

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

	// Always update the status
	defer func() {
		if err := r.Status().Update(ctx, app); err != nil {
			log.Error(err, "unable to update FluxApp status")
		}
	}()

	// Handle the chart ImageRepository object
	imageRepo, err := handleImageRepository(ctx, r, app)
	if err != nil {
		return ctrl.Result{}, err
	}
	if imageRepo == nil {
		return ctrl.Result{
			RequeueAfter: 10 * time.Second,
		}, nil
	}

	// Handle the chart ImagePolicy object
	imagePolicy, err := handleImagePolicy(ctx, r, app, imageRepo)
	if err != nil {
		return ctrl.Result{}, err
	}
	if imagePolicy == nil {
		return ctrl.Result{
			RequeueAfter: 10 * time.Second,
		}, nil
	}

	// Handle the HelmRepository object
	helmRepo, err := handleHelmRepository(ctx, r, app)
	if err != nil {
		return ctrl.Result{}, err
	}
	if helmRepo == nil {
		return ctrl.Result{
			RequeueAfter: 10 * time.Second,
		}, nil
	}

	// Handle the HelmRelease object
	_, err = handleHelmRelease(ctx, r, app, helmRepo)
	if err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// Handle Flux ImageRepository object
func handleImageRepository(ctx context.Context, r *FluxAppReconciler, app *appsv1.FluxApp) (*imagev1.ImageRepository, error) {
	imageRepo := &imagev1.ImageRepository{
		TypeMeta: metav1.TypeMeta{
			Kind: imagev1.ImageRepositoryKind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      strings.Join([]string{app.Name, "chart"}, "-"),
			Namespace: app.Namespace,
		},
	}
	exists := true
	if err := r.Get(ctx, client.ObjectKeyFromObject(imageRepo), imageRepo); err != nil {
		if client.IgnoreNotFound(err) != nil {
			return nil, err
		}
		exists = false
	}
	patch := client.MergeFrom(imageRepo.DeepCopy())
	if exists {
		app.Status.Chart.Repository = "oci://" + path.Dir(imageRepo.Spec.Image)
		app.Status.Chart.Name = path.Base(imageRepo.Spec.Image)
	}
	url, err := url.Parse(app.Spec.Chart.Repository)
	if err != nil {
		return nil, err
	}
	provider, err := providerFromURL(app.Spec.Chart.Repository)
	if err != nil {
		return nil, err
	}
	imageRepo.Spec = imagev1.ImageRepositorySpec{
		Image:    strings.TrimPrefix(app.Spec.Chart.Repository, url.Scheme+"://"),
		Interval: metav1.Duration{Duration: 1 * time.Minute},
		Provider: provider,
		Insecure: url.Scheme == "http",
	}
	if err := controllerutil.SetControllerReference(app, imageRepo, r.Scheme); err != nil {
		return nil, err
	}
	if exists {
		err = r.Patch(ctx, imageRepo, patch)
	} else {
		err = r.Create(ctx, imageRepo)
	}
	return imageRepo, client.IgnoreAlreadyExists(err)
}

// Handle Flux ImagePolicy object
func handleImagePolicy(ctx context.Context, r *FluxAppReconciler, app *appsv1.FluxApp, imageRepo *imagev1.ImageRepository) (*imagev1.ImagePolicy, error) {
	imagePolicy := &imagev1.ImagePolicy{
		TypeMeta: metav1.TypeMeta{
			Kind: imagev1.ImagePolicyKind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      strings.Join([]string{app.Name, "chart"}, "-"),
			Namespace: app.Namespace,
		},
	}
	exists := true
	if err := r.Get(ctx, client.ObjectKeyFromObject(imagePolicy), imagePolicy); err != nil {
		if client.IgnoreNotFound(err) != nil {
			return nil, err
		}
		exists = false
	}
	patch := client.MergeFrom(imagePolicy.DeepCopy())
	if exists {
		parts := strings.Split(imagePolicy.Status.LatestImage, ":")
		if len(parts) == 2 {
			app.Status.Chart.Version = parts[1]
		}
	}
	imagePolicy.Spec = imagev1.ImagePolicySpec{
		ImageRepositoryRef: meta.NamespacedObjectReference{
			Name:      imageRepo.Name,
			Namespace: imageRepo.Namespace,
		},
		Policy: imagev1.ImagePolicyChoice{
			SemVer: &imagev1.SemVerPolicy{
				Range: app.Spec.Chart.Version,
			},
		},
	}
	if err := controllerutil.SetControllerReference(app, imagePolicy, r.Scheme); err != nil {
		return nil, err
	}
	var err error
	if exists {
		err = r.Patch(ctx, imagePolicy, patch)
	} else {
		err = r.Create(ctx, imagePolicy)
	}
	return imagePolicy, client.IgnoreAlreadyExists(err)
}

// Handle Flux HelmRepository object
func handleHelmRepository(ctx context.Context, r *FluxAppReconciler, app *appsv1.FluxApp) (*sourcev1.HelmRepository, error) {
	if app.Status.Chart.Repository == "" {
		return nil, nil
	}
	sr := strings.NewReplacer(".", "-", "/", "-")
	helmRepository := &sourcev1.HelmRepository{
		TypeMeta: metav1.TypeMeta{
			Kind: sourcev1.HelmRepositoryKind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      sr.Replace(strings.TrimPrefix(app.Status.Chart.Repository, "oci://")),
			Namespace: app.Namespace,
		},
	}
	exists := true
	if err := r.Get(ctx, client.ObjectKeyFromObject(helmRepository), helmRepository); err != nil {
		if client.IgnoreNotFound(err) != nil {
			return nil, err
		}
		exists = false
	}
	if exists {
		return helmRepository, nil
	}
	provider, err := providerFromURL(app.Status.Chart.Repository)
	if err != nil {
		return nil, err
	}
	helmRepository.Spec = sourcev1.HelmRepositorySpec{
		URL:      app.Status.Chart.Repository,
		Type:     "oci",
		Provider: provider,
	}
	if err := controllerutil.SetControllerReference(app, helmRepository, r.Scheme); err != nil {
		return nil, err
	}
	return helmRepository, client.IgnoreAlreadyExists(r.Create(ctx, helmRepository))
}

// Handle Flux HelmRelease object
func handleHelmRelease(ctx context.Context, r *FluxAppReconciler, app *appsv1.FluxApp, helmRepo *sourcev1.HelmRepository) (*helmv2.HelmRelease, error) {
	if app.Status.Chart.Repository == "" || app.Status.Chart.Name == "" || app.Status.Chart.Version == "" {
		return nil, nil
	}
	helmRelease := &helmv2.HelmRelease{
		TypeMeta: metav1.TypeMeta{
			Kind: helmv2.HelmReleaseKind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      app.Name,
			Namespace: app.Namespace,
		},
	}
	exists := true
	if err := r.Get(ctx, client.ObjectKeyFromObject(helmRelease), helmRelease); err != nil {
		if client.IgnoreNotFound(err) != nil {
			return nil, err
		}
		exists = false
	}
	patch := client.MergeFrom(helmRelease.DeepCopy())
	if exists {
		conditions.SetMirror(app, meta.ReadyCondition, helmRelease)
	}
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
					Name:      helmRepo.Name,
					Namespace: helmRepo.Namespace,
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
	if err := controllerutil.SetControllerReference(app, helmRelease, r.Scheme); err != nil {
		return nil, err
	}
	var err error
	if exists {
		err = r.Patch(ctx, helmRelease, patch)
	} else {
		err = r.Create(ctx, helmRelease)
	}
	return helmRelease, client.IgnoreAlreadyExists(err)
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
