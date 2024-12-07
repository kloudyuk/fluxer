package controller

import (
	"context"
	"fmt"
	"strings"

	helmv2 "github.com/fluxcd/helm-controller/api/v2"
	imagev1 "github.com/fluxcd/image-reflector-controller/api/v1beta2"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	appsv1 "github.com/kloudyuk/fluxer/api/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

type ResourceManager struct {
	c      client.Client
	scheme *runtime.Scheme
}

type managedResource struct {
	client.Object
	patch client.Patch
}

func NewResourceManager(c client.Client, scheme *runtime.Scheme) *ResourceManager {
	return &ResourceManager{c, scheme}
}

func (rm *ResourceManager) Update(ctx context.Context, res *managedResource) error {
	if res.patch == nil {
		return rm.c.Create(ctx, res.Object)
	}
	return rm.c.Patch(ctx, res.Object, res.patch)
}

func (rm *ResourceManager) Get(ctx context.Context, app *appsv1.FluxApp, kind string) (*managedResource, error) {
	// Initialise a ManagedResource object based on the kind
	mr := &managedResource{}
	key := types.NamespacedName{}
	switch kind {
	case imagev1.ImageRepositoryKind:
		mr.Object = &imagev1.ImageRepository{}
		key.Name = rm.ImageRepositoryName(app)
	case imagev1.ImagePolicyKind:
		mr.Object = &imagev1.ImagePolicy{}
		key.Name = rm.ImagePolicyName(app)
	case sourcev1.HelmRepositoryKind:
		mr.Object = &sourcev1.HelmRepository{}
		key.Name = rm.HelmRepositoryName(app)
	case helmv2.HelmReleaseKind:
		mr.Object = &helmv2.HelmRelease{}
		key.Name = rm.HelmReleaseName(app)
	default:
		return nil, fmt.Errorf("unsupported kind: %s", kind)
	}
	key.Namespace = app.Namespace
	// Get the existing object
	if err := rm.c.Get(ctx, key, mr.Object); err != nil {
		// Handle error
		if client.IgnoreNotFound(err) != nil {
			return nil, err
		}
		// Object not found
		// so set the name, namespace & controller ref on our empty object
		mr.SetName(key.Name)
		mr.SetNamespace(key.Namespace)
		if err := controllerutil.SetControllerReference(app, mr, rm.scheme); err != nil {
			return nil, err
		}
	} else {
		// Object found
		// Create a patch from the existing object
		switch o := mr.Object.(type) {
		case *imagev1.ImageRepository:
			mr.patch = client.MergeFrom(o.DeepCopy())
		case *imagev1.ImagePolicy:
			mr.patch = client.MergeFrom(o.DeepCopy())
		case *sourcev1.HelmRepository:
			mr.patch = client.MergeFrom(o.DeepCopy())
		case *helmv2.HelmRelease:
			mr.patch = client.MergeFrom(o.DeepCopy())
		default:
			return nil, fmt.Errorf("unsupported kind: %s", o.GetObjectKind().GroupVersionKind().Kind)
		}
	}
	// Return the managedResource object
	return mr, nil
}

func (rm *ResourceManager) ImageRepositoryName(app *appsv1.FluxApp) string {
	return strings.Join([]string{app.Name, "chart"}, "-")
}

func (rm *ResourceManager) ImagePolicyName(app *appsv1.FluxApp) string {
	return rm.ImageRepositoryName(app)
}

func (rm *ResourceManager) HelmRepositoryName(app *appsv1.FluxApp) string {
	return strings.NewReplacer(".", "-", "/", "-").Replace(strings.TrimPrefix(app.Status.Chart.Repository, "oci://"))
}

func (rm *ResourceManager) HelmReleaseName(app *appsv1.FluxApp) string {
	return app.Name
}
