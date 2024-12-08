# fluxer

A Kubernetes controller example

## Description

Fluxer is a pointless and unnecessary controller created purely to showcase various techniques that can be used in a controller.

It includes a minimal `FluxApp` CRD which is used to generate a Flux `HelmRelease` from a public OCI chart repo. This example demonstrates how a CRD can be used to provide a simple interface to manage other resources.

> [!CAUTION]
> This isn't something you'd want to use in a real environment. You should just use the Flux resources directly.

The `FluxApp` resource creates and manages the following Flux resources:

- `ImageRepository` - for scanning the OCI repo for available chart versions
- `ImagePolicy` - selects a version based on a SemVer version or version constraint
- `HelmRepository` - source to retrieve Helm charts from
- `HelmRelease` - installs the chart in the cluster

## Deployment

### Prerequisites

- [Kind](https://kind.sigs.k8s.io/) - `brew install kind`
- [Docker](https://www.docker.com/)
- [Go](https://go.dev/) - `brew install go`
- [Flux CLI](https://fluxcd.io/flux/cmd/) - `brew install fluxcd/tap/flux`

### Install

To deploy the controller to a kind cluster:

```sh
# Create a kind cluster
kind create cluster

# Install the Flux controllers inc. the image automation controllers
make flux

# Build and deploy the CRD & controller
make deploy
```

The controller will be installed to the `fluxer-system` namespace. You can tail the logs to ensure the controller started successfully e.g.

```sh
kubectl -n fluxer-system -l app.kubernetes.io/name=fluxer logs -f
```

## Usage

### Example

```yaml
apiVersion: apps.kloudy.uk/v1
kind: FluxApp
metadata:
  name: example
spec:
  chart:
    repository: oci://ghcr.io/stefanprodan/charts/podinfo
    version: ~> 6
  targetNamespace: default
```

### Spec

`chart.repository` (*required*) - Defines the repository containg the helm chart. This example controller only supports public OCI chart repos.

`chart.version` (*optional*) - The chart version to use. Must be a valid SemVer version or version constraint. If omitted, `*` will be used which gets the latest version.

`targetNamespace` (*optional*) - Sets the `targetNamespace` in the `HelmRelease`. If omitted, the `FluxApp` namespace will be used.

## Controller Design

### Resource Manager

The controller is managing multiple Flux resources in the reconcile loop. For each of these resources we need a way to fetch the Flux resource from the server (if it exists) or create the resource if it doesn't exist. Once we've made any necessary changes to the resource we need to update the resource in the server. To avoid repeating similar logic for each Flux resource, a [ResourceManager](./internal/controller/fluxapp_resource_manager.go#L18-L21) has been implemented that can work with any of the required Flux types by leveraging the [client.Object](./internal/controller/fluxapp_resource_manager.go#L23-L26) interface.

### OwnerReference / ControllerReference

As the fluxer controller is managing Flux resources, we need ensure the fluxer controller is marked as the owner of the Flux resources for the following reasons:

1. Garbage collection - when the `FluxApp` is deleted, the Flux resources should also be removed.
2. Trigger reconcilliation of the `FluxApp` if any of the flux resources are updated.

This is handled by the [ResourceManager](./internal/controller/fluxapp_resource_manager.go#L70-L72).

### Patch vs Update

The fluxer controller is creating resources that will be processed by the Flux controllers. The Flux controllers will make updates to these resources which can lead to conflicts e.g. if the fluxer controller tries to update a resource that has been modified by a Flux controller since it was fetched from the server. To avoid this (and because it's good practice and more efficient), we patch resources rather than updating them. This is also handled by the [ResourceManager](./internal/controller/fluxapp_resource_manager.go#L32-L37).

When the `ResourceManager` fetches an object from the server, if the object exists, [a copy of the original object is stored](./internal/controller/fluxapp_resource_manager.go#L75-L88) to be used as the patch base later.
