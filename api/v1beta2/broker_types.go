/*
Copyright 2021.

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
	"github.com/RHsyseng/operator-utils/pkg/olm"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// BrokerSpec defines the desired state of Broker
type BrokerSpec struct {
	// Specifies the deployment plan
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Deployment Plan"
	DeploymentPlan DeploymentPlanType `json:"deploymentPlan,omitempty"`
	// The desired version of the broker. Can be x, or x.y or x.y.z to configure upgrades
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Version",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	Version string `json:"version,omitempty"`
	// Optional list of key=value properties that are applied to the broker configuration bean.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Broker Properties"
	BrokerProperties []string `json:"brokerProperties,omitempty"`
	// Optional list of environment variables to apply to the container(s), not exclusive
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Environment Variables"
	Env []corev1.EnvVar `json:"env,omitempty"`
	// Specifies the template for various resources that the operator controls
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Resource Templates"
	ResourceTemplates []ResourceTemplate `json:"resourceTemplates,omitempty"`
}

// BrokerStatus defines the observed state of Broker
type BrokerStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	//+operator-sdk:csv:customresourcedefinitions:type=status,displayName="Conditions",xDescriptors="urn:alm:descriptor:io.kubernetes.conditions"
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,2,rep,name=conditions"`

	// The current pods
	//+operator-sdk:csv:customresourcedefinitions:type=status,displayName="Pods Status",xDescriptors="urn:alm:descriptor:com.tectonic.ui:podStatuses"
	PodStatus olm.DeploymentStatus `json:"podStatus"`

	//+operator-sdk:csv:customresourcedefinitions:type=status,displayName="Deployment Plan Size"
	DeploymentPlanSize int32 `json:"deploymentPlanSize,omitempty"`

	//+operator-sdk:csv:customresourcedefinitions:type=status,displayName="Auto scale label selector"
	ScaleLabelSelector string `json:"scaleLabelSelector,omitempty"`

	// Current state of external referenced resources
	//+operator-sdk:csv:customresourcedefinitions:type=status,displayName="External Configurations Status"
	ExternalConfigs []ExternalConfigStatus `json:"externalConfigs,omitempty"`

	//+operator-sdk:csv:customresourcedefinitions:type=status,displayName="Version Status"
	Version VersionStatus `json:"version,omitempty"`

	//+operator-sdk:csv:customresourcedefinitions:type=status,displayName="Upgrade Status"
	Upgrade UpgradeStatus `json:"upgrade,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:subresource:scale:specpath=.spec.deploymentPlan.size,statuspath=.status.deploymentPlanSize,selectorpath=.status.scaleLabelSelector
//+kubebuilder:storageversion
//+kubebuilder:resource:path=brokers,shortName=b
//+kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status",description="The state of the resource"
//+kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="The age of the resource"
//+operator-sdk:csv:customresourcedefinitions:resources={{"Service", "v1"}}
//+operator-sdk:csv:customresourcedefinitions:resources={{"Secret", "v1"}}
//+operator-sdk:csv:customresourcedefinitions:resources={{"ConfigMap", "v1"}}
//+operator-sdk:csv:customresourcedefinitions:resources={{"StatefulSet", "apps/v1"}}

// A stateful deployment of one or more brokers
// +operator-sdk:csv:customresourcedefinitions:displayName="Broker"
type Broker struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   BrokerSpec   `json:"spec,omitempty"`
	Status BrokerStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// BrokerList contains a list of Broker
type BrokerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Broker `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Broker{}, &BrokerList{})
}

func (r *Broker) Hub() {
}
