/*
Copyright 2026.

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

package v1beta2

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

type BrokerServiceSpec struct {

	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Resources"
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`

	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Environment"
	Env []corev1.EnvVar `json:"env,omitempty"`

	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Broker image"
	Image *string `json:"image,omitempty"`

	// AppSelectorExpression is a CEL expression that determines which BrokerApps
	// can deploy to this service.
	//
	// The expression has access to the following variables:
	// - app: The BrokerApp object being evaluated (map with metadata, spec, etc.)
	// - service: The BrokerService object (map with metadata, spec, etc.)
	// - appNamespace: The Namespace object where the app resides (map with metadata, etc.)
	// - serviceNamespace: The Namespace object where the service resides (map with metadata, etc.)
	//
	// Empty or nil (default): Uses "app.metadata.namespace == service.metadata.namespace" (same namespace only).
	//
	// The expression must evaluate to a boolean. Matching is checked at binding time
	// and continuously during reconciliation. Apps that no longer match are automatically
	// unbound and must find an alternative service.
	//
	//+optional
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="App Selector Expression"
	AppSelectorExpression string `json:"appSelectorExpression,omitempty"`

	// PKI configures the chain-of-trust certificate properties.
	// When omitted, sensible defaults are used (10-year CA, 90-day leaf certs).
	//+optional
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="PKI Configuration"
	PKI *PKISpec `json:"pki,omitempty"`
}

// RejectedApp represents a BrokerApp that was rejected during provisioning validation
type RejectedApp struct {
	// Name of the rejected app
	Name string `json:"name"`
	// Namespace of the rejected app
	Namespace string `json:"namespace"`
	// Reason why the app was rejected
	Reason string `json:"reason"`
}

type BrokerServiceStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Current state of the resource
	// Conditions represent the latest available observations of an object's state
	//+optional
	//+patchMergeKey=type
	//+patchStrategy=merge
	//+operator-sdk:csv:customresourcedefinitions:type=status,displayName="Conditions",xDescriptors="urn:alm:descriptor:io.kubernetes.conditions"
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,2,rep,name=conditions"`

	// List of BrokerApp identities that have been applied to the service
	//+operator-sdk:csv:customresourcedefinitions:type=status,displayName="Provisioned Applications"
	ProvisionedApps []string `json:"provisionedApps,omitempty"`

	// List of BrokerApps that were rejected during provisioning validation
	//+optional
	//+operator-sdk:csv:customresourcedefinitions:type=status,displayName="Rejected Applications"
	RejectedApps []RejectedApp `json:"rejectedApps,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:storageversion
//+kubebuilder:subresource:status
//+kubebuilder:resource:path=brokerservices,shortName=bsvc
//+operator-sdk:csv:customresourcedefinitions:resources={{"Secret", "v1"}}
//+operator-sdk:csv:customresourcedefinitions:resources={{"Service", "v1"}}
//+operator-sdk:csv:customresourcedefinitions:resources={{"Broker", "v1beta2"}}

// Provides a broker service
// +operator-sdk:csv:customresourcedefinitions:displayName="Broker Service"
type BrokerService struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   BrokerServiceSpec   `json:"spec,omitempty"`
	Status BrokerServiceStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

type BrokerServiceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []BrokerService `json:"items"`
}

func init() {
	SchemeBuilder.Register(&BrokerService{}, &BrokerServiceList{})
}
