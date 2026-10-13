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
}

type PendingEmptyAddressStatus struct {
	// App is the identity of the BrokerApp that owns the addresses pending empty
	App string `json:"app"`
	// Messages maps each fully qualified queue name (address::queue) to its
	// pending message count. Entries are removed once the count reaches zero.
	Messages map[string]int64 `json:"messages,omitempty"`
}

type BrokerServiceStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// ObservedGeneration is the most recent generation observed for this BrokerService.
	// It corresponds to the BrokerService's generation, which is updated on mutation by the API Server.
	//+optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Current state of the resource
	// Conditions represent the latest available observations of an object's state
	//+optional
	//+patchMergeKey=type
	//+patchStrategy=merge
	//+operator-sdk:csv:customresourcedefinitions:type=status,displayName="Conditions",xDescriptors="urn:alm:descriptor:io.kubernetes.conditions"
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,2,rep,name=conditions"`

	// BrokerApps that have been applied to the service, each as <namespace>/<name>@<generation>
	//+operator-sdk:csv:customresourcedefinitions:type=status,displayName="Provisioned Applications"
	ProvisionedApps []string `json:"provisionedApps,omitempty"`

	// PendingEmpty lists apps with addresses pending empty before removal.
	// Entries appear when addresses are removed from an app's spec or when an
	// app is being deleted, and remain until all messages have been consumed.
	//+optional
	//+operator-sdk:csv:customresourcedefinitions:type=status,displayName="Pending Empty"
	PendingEmpty []PendingEmptyAddressStatus `json:"pendingEmpty,omitempty"`
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

const (
	AppsProvisionedConditionType           = "AppsProvisioned"
	AppsProvisionedConditionSyncedReason   = "Synced"
	AppsProvisionedConditionWaitingReason  = "WaitingForBroker"
	AppsProvisionedConditionNotReadyReason = "BrokerNotReady"

	ReadyConditionAddressesPendingEmpty      = "AddressesPendingEmpty"
	DeployedConditionPortPoolExhaustedReason = "PortPoolExhausted"

	// AddressesPendingEmptyFinalizer is applied to both BrokerApp and BrokerService
	// resources to ensure pending messages are consumed before resources are removed.
	AddressesPendingEmptyFinalizer = "broker.arkmq.org/addresses-pending-empty"
)
