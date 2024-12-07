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

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// FluxAppSpec defines the desired state of FluxApp.
type FluxAppSpec struct {
	// Chart defines info about the chart to deploy
	Chart Chart `json:"chart"`
	// TargetNamespace is the namespace to use for the HelmRelease
	// Defaults to the namespace of the FluxApp
	// +optional
	TargetNamespace string `json:"targetNamespace,omitempty"`
}

type Chart struct {
	// Full repository URL of the chart including scheme e.g. oci://ghcr.io/stefanprodan/charts/podinfo
	// +kubebuilder:validation:Pattern=`^oci://.*`
	// +required
	Repository string `json:"repository"`
	// Version of the chart as a semver version or version constraint.
	// Defaults to latest when omitted.
	// +kubebuilder:default:=*
	// +optional
	Version string `json:"version"`
}

// FluxAppStatus defines the observed state of FluxApp.
type FluxAppStatus struct {
	Chart ChartStatus `json:"chart"`
	// Conditions holds the conditions for the FluxApp.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// ChartStatus defines the observed state of the flux image resourfces for a chart
type ChartStatus struct {
	Repository string `json:"repository"`
	Name       string `json:"name"`
	Version    string `json:"version,omitempty"`
}

// GetConditions returns the status conditions of the object.
func (in FluxApp) GetConditions() []metav1.Condition {
	return in.Status.Conditions
}

// SetConditions sets the status conditions on the object.
func (in *FluxApp) SetConditions(conditions []metav1.Condition) {
	in.Status.Conditions = conditions
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=fa
// +kubebuilder:printcolumn:name="Chart",type=string,JSONPath=`.status.chart.name`
// +kubebuilder:printcolumn:name="Version",type=string,JSONPath=`.status.chart.version`
// +kubebuilder:printcolumn:name="Status",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].message`

// FluxApp is the Schema for the fluxapps API.
type FluxApp struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   FluxAppSpec   `json:"spec,omitempty"`
	Status FluxAppStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// FluxAppList contains a list of FluxApp.
type FluxAppList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []FluxApp `json:"items"`
}

func init() {
	SchemeBuilder.Register(&FluxApp{}, &FluxAppList{})
}
