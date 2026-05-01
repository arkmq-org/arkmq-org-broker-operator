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

type BrokerAppSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="ServiceSelector"
	ServiceSelector *metav1.LabelSelector `json:"selector,omitempty"`

	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Addresses"
	// Addresses owned by this app (lifecycle tied to this app)
	// Other apps can reference these via appNamespace/appName in their capabilities
	Addresses []AddressType `json:"addresses,omitempty"`

	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Messaging Capabilities"
	Capabilities []AppCapabilityType `json:"capabilities,omitempty"`

	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Resources"
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`
}

// AddressType defines a messaging address
type AddressType struct {
	// Address is the address identifier (unique within a broker service)
	Address string `json:"address"`
}

// AddressRef references an address for use in capabilities
type AddressRef struct {
	// Address is the address identifier (required)
	// Can be simple name ("orders") or FQQN ("events::queue1" for SubscriberOf)
	Address string `json:"address"`

	// AppNamespace of owning app - for cross-app references  (optional)
	AppNamespace string `json:"appNamespace,omitempty"`

	// AppName of owning app - for cross-app references  (optional)
	AppName string `json:"appName,omitempty"`
}

type AppCapabilityType struct {
	ProducerOf []AddressRef `json:"producerOf,omitempty"`

	ConsumerOf []AddressRef `json:"consumerOf,omitempty"`

	SubscriberOf []AddressRef `json:"subscriberOf,omitempty"`
}

// BrokerServiceBindingStatus captures the binding details between a BrokerApp and its provisioned BrokerService
type BrokerServiceBindingStatus struct {
	// Name of the BrokerService this app is bound to
	Name string `json:"name"`

	// Namespace of the BrokerService this app is bound to
	Namespace string `json:"namespace"`

	// Secret is the name of the binding secret containing connection details
	Secret string `json:"secret"`

	// AssignedPort is the port allocated from the service's pool for this app's acceptor
	AssignedPort int32 `json:"assignedPort"`
}

// Key returns the field indexer key for this service binding (namespace:name format)
func (s *BrokerServiceBindingStatus) Key() string {
	return s.Namespace + ":" + s.Name
}

type BrokerAppStatus struct {

	// Current state of the resource
	// Conditions represent the latest available observations of an object's state
	//+optional
	//+patchMergeKey=type
	//+patchStrategy=merge
	//+operator-sdk:csv:customresourcedefinitions:type=status,displayName="Conditions",xDescriptors="urn:alm:descriptor:io.kubernetes.conditions"
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,2,rep,name=conditions"`

	// Service references the BrokerService this app is bound to and its binding secret
	//+optional
	Service *BrokerServiceBindingStatus `json:"service,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:storageversion
//+kubebuilder:subresource:status
//+kubebuilder:resource:path=brokerapps,shortName=bapp
//+operator-sdk:csv:customresourcedefinitions:resources={{"Secret", "v1"}}

// Describes the messaging requirements of an application
// +operator-sdk:csv:customresourapplicationcedefinitions:displayName="Messaging Application"
type BrokerApp struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   BrokerAppSpec   `json:"spec,omitempty"`
	Status BrokerAppStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

type BrokerAppList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []BrokerApp `json:"items"`
}

func init() {
	SchemeBuilder.Register(&BrokerApp{}, &BrokerAppList{})
}
