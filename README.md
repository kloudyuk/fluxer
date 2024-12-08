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

# Install the Flux controllers
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
