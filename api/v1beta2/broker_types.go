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
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// BrokerSpec defines the desired state of a restricted-mode Broker deployment.
// Fields are flat (no deploymentPlan wrapper). Fields that do not apply to
// restricted mode — InitImage, Clustered, MessageMigration, RequireLogin,
// JolokiaAgentEnabled, ManagementRBACEnabled, JournalType — are intentionally
// absent: restricted brokers always run embedded Java with mTLS Jolokia/Prometheus,
// no init containers, no password auth, and no clustering.
type BrokerSpec struct {
	// The broker container image. Overrides the operator-managed image.
	// Disables automatic upgrades when set.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Image",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	Image string `json:"image,omitempty"`

	// The desired version of the broker. Can be x, or x.y or x.y.z to configure upgrades.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Version",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	Version string `json:"version,omitempty"`

	// Specifies the minimum/maximum compute resources required/allowed for the broker container.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Resource Requirements",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:resourceRequirements"}
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`

	// Optional list of environment variables to apply to the broker container.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Environment Variables"
	Env []corev1.EnvVar `json:"env,omitempty"`

	// If true, use a persistent volume via PVC for journal storage.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Persistence Enabled",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:booleanSwitch"}
	PersistenceEnabled bool `json:"persistenceEnabled,omitempty"`

	// Specifies the storage configuration (used when PersistenceEnabled=true).
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Storage Configurations"
	Storage StorageType `json:"storage,omitempty"`

	// Optional list of key=value properties applied to the broker configuration bean.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Broker Properties"
	BrokerProperties []string `json:"brokerProperties,omitempty"`

	// Specifies the template for various resources that the operator controls.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Resource Templates"
	ResourceTemplates []ResourceTemplate `json:"resourceTemplates,omitempty"`

	// Specifies extra configmap/secret mounts for the broker container.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Extra Mounts"
	ExtraMounts ExtraMountsType `json:"extraMounts,omitempty"`

	// Image pull secrets for the broker container image.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Image Pull Secrets"
	ImagePullSecrets []corev1.LocalObjectReference `json:"imagePullSecrets,omitempty"`

	// Assign labels to broker pods. The keys "Broker" and "application" are reserved.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Labels"
	Labels map[string]string `json:"labels,omitempty"`

	// Custom annotations added to broker pods.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Annotations"
	Annotations map[string]string `json:"annotations,omitempty"`

	// Specifies the node selector for broker pods.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Node Selector",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:selector"}
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// Specifies affinity configuration for broker pods.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Affinity Configurations"
	Affinity AffinityConfig `json:"affinity,omitempty"`

	// Specifies tolerations for broker pods.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Tolerations"
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`

	// Specifies topology spread constraints for broker pods.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Topology Spread Constraints"
	TopologySpreadConstraints []corev1.TopologySpreadConstraint `json:"topologySpreadConstraints,omitempty"`

	// Specifies pod-level security settings (service account, run-as user).
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Pod Security Configurations"
	PodSecurity PodSecurityType `json:"podSecurity,omitempty"`

	// Specifies the Kubernetes pod security context.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Pod Security Context"
	PodSecurityContext *corev1.PodSecurityContext `json:"podSecurityContext,omitempty"`

	// Specifies the container-level security context.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Container Security Context"
	ContainerSecurityContext *corev1.SecurityContext `json:"containerSecurityContext,omitempty"`

	// Specifies the startup probe configuration.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Startup Probe Configurations"
	StartupProbe *corev1.Probe `json:"startupProbe,omitempty"`

	// Specifies the liveness probe configuration.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Liveness Probe Configurations"
	LivenessProbe *corev1.Probe `json:"livenessProbe,omitempty"`

	// Specifies the readiness probe configuration.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Readiness Probe Configurations"
	ReadinessProbe *corev1.Probe `json:"readinessProbe,omitempty"`

	// Whether or not to install the Artemis metrics plugin.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Enable Metrics Plugin",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:booleanSwitch"}
	EnableMetricsPlugin *bool `json:"enableMetricsPlugin,omitempty"`

	// Specifies the pod disruption budget.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Pod Disruption Budget"
	PodDisruptionBudget *policyv1.PodDisruptionBudgetSpec `json:"podDisruptionBudget,omitempty"`

	// Specifies the revision history limit of the StatefulSet.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Revision History Limit"
	RevisionHistoryLimit *int32 `json:"revisionHistoryLimit,omitempty"`

	// Additional volumes attached to broker pods.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Extra Volumes"
	ExtraVolumes []corev1.Volume `json:"extraVolumes,omitempty"`

	// Mount options for ExtraVolumes.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Extra Volume Mounts"
	ExtraVolumeMounts []corev1.VolumeMount `json:"extraVolumeMounts,omitempty"`

	// Extra PVC templates for broker pods.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Extra Volume Claim Templates"
	ExtraVolumeClaimTemplates []VolumeClaimTemplate `json:"extraVolumeClaimTemplates,omitempty"`
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
