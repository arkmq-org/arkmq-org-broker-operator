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
	"k8s.io/apimachinery/pkg/runtime"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// BrokerSpec defines the desired state of Broker
type BrokerSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// User name for standard broker user. It is required for connecting to the broker and the web console. If left empty, it will be generated.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Admin User",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	AdminUser string `json:"adminUser,omitempty"`
	// Password for standard broker user. It is required for connecting to the broker and the web console. If left empty, it will be generated.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Admin Password",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:password"}
	AdminPassword string `json:"adminPassword,omitempty"`
	// Specifies the deployment plan
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Deployment Plan"
	DeploymentPlan DeploymentPlanType `json:"deploymentPlan,omitempty"`
	// Specifies the acceptor configuration
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Acceptors"
	Acceptors []AcceptorType `json:"acceptors,omitempty"`
	// Specifies connectors and connector configuration
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Connectors"
	Connectors []ConnectorType `json:"connectors,omitempty"`
	// Specifies the console configuration
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Console Configurations"
	Console ConsoleType `json:"console,omitempty"`
	// The desired version of the broker. Can be x, or x.y or x.y.z to configure upgrades
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Version",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	Version string `json:"version,omitempty"`
	// Specifies the upgrades (deprecated in favour of Version)
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Upgrades"
	Upgrades BrokerUpgrades `json:"upgrades,omitempty"`
	// Specifies the address configurations (deprecated in favour of BrokerProperties)
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Address Configurations"
	AddressSettings AddressSettingsType `json:"addressSettings,omitempty"`
	// Optional list of key=value properties that are applied to the broker configuration bean.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Broker Properties"
	BrokerProperties []string `json:"brokerProperties,omitempty"`
	// Optional list of environment variables to apply to the container(s), not exclusive
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Environment Variables"
	Env []corev1.EnvVar `json:"env,omitempty"`
	// The default ingress domain. It is required when any acceptor, connector or console uses the ingress mode and does not specify an IngressHost.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Ingress Domain",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	IngressDomain string `json:"ingressDomain,omitempty"`
	// Specifies the template for various resources that the operator controls
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Resource Templates"
	ResourceTemplates []ResourceTemplate `json:"resourceTemplates,omitempty"`

	// Restricted deployment, mtls jolokia agent with RBAC
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Restricted"
	Restricted *bool `json:"restricted,omitempty"`
}

type AddressSettingsType struct {
	// How to merge the address settings to broker configuration
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Apply Rule",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	ApplyRule *string `json:"applyRule,omitempty"`
	// Specifies the address settings
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Address Settings"
	AddressSetting []AddressSettingType `json:"addressSetting,omitempty"`
}

type AddressSettingType struct {
	// the address to send dead messages to
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Dead Letter Address",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	DeadLetterAddress *string `json:"deadLetterAddress,omitempty"`
	// whether or not to automatically create the dead-letter-address and/or a corresponding queue on that address when a message found to be undeliverable
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="AutoCreateDeadLetterResources",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:booleanSwitch"}
	AutoCreateDeadLetterResources *bool `json:"autoCreateDeadLetterResources,omitempty"`
	// the prefix to use for auto-created dead letter queues
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Dead Letter Queue Prefix",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	DeadLetterQueuePrefix *string `json:"deadLetterQueuePrefix,omitempty"`
	// the suffix to use for auto-created dead letter queues
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Dead Letter Queue Suffix",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	DeadLetterQueueSuffix *string `json:"deadLetterQueueSuffix,omitempty"`
	// the address to send expired messages to
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Expiry Address",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	ExpiryAddress *string `json:"expiryAddress,omitempty"`
	// whether or not to automatically create the expiry-address and/or a corresponding queue on that address when a message is sent to a matching queue
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Auto Create Expiry Resources",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:booleanSwitch"}
	AutoCreateExpiryResources *bool `json:"autoCreateExpiryResources,omitempty"`
	// the prefix to use for auto-created expiry queues
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Expiry Queue Prefix",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	ExpiryQueuePrefix *string `json:"expiryQueuePrefix,omitempty"`
	// the suffix to use for auto-created expiry queues
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Expiry Queue Suffix",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	ExpiryQueueSuffix *string `json:"expiryQueueSuffix,omitempty"`
	// Overrides the expiration time for messages using the default value for expiration time. "-1" disables this setting.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Expiry Delay",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:number"}
	ExpiryDelay *int32 `json:"expiryDelay,omitempty"`
	// Overrides the expiration time for messages using a lower value. "-1" disables this setting.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Min Expiry Delay",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:number"}
	MinExpiryDelay *int32 `json:"minExpiryDelay,omitempty"`
	// Overrides the expiration time for messages using a higher value. "-1" disables this setting.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Max Expiry Delay",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:number"}
	MaxExpiryDelay *int32 `json:"maxExpiryDelay,omitempty"`
	// the time (in ms) to wait before redelivering a cancelled message.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Redelivery Delay",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:number"}
	RedeliveryDelay *int32 `json:"redeliveryDelay,omitempty"`

	// dropping these two fields due to historicatl incorrect conversion from *float32 to *string
	// without conversion support. Existing CR's with these set cannot be reconciled
	//
	// multiplier to apply to the redelivery-delay
	//RedeliveryDelayMultiplier *string
	// factor by which to modify the redelivery delay slightly to avoid collisions
	//RedeliveryCollisionAvoidanceFactor *string

	// Maximum value for the redelivery-delay
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Max Redelivery Delay",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:number"}
	MaxRedeliveryDelay *int32 `json:"maxRedeliveryDelay,omitempty"`
	// how many times to attempt to deliver a message before sending to dead letter address
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Max Delivery Attempts",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:number"}
	MaxDeliveryAttempts *int32 `json:"maxDeliveryAttempts,omitempty"`
	// the maximum size in bytes for an address. -1 means no limits. This is used in PAGING, BLOCK and FAIL policies. Supports byte notation like K, Mb, GB, etc.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Max Size Bytes",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	MaxSizeBytes *string `json:"maxSizeBytes,omitempty"`
	// used with the address full BLOCK policy, the maximum size in bytes an address can reach before messages start getting rejected. Works in combination with max-size-bytes for AMQP protocol only.  Default = -1 (no limit).
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Max Size Bytes Reject Threshold",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:number"}
	MaxSizeBytesRejectThreshold *int32 `json:"maxSizeBytesRejectThreshold,omitempty"`
	// The page size in bytes to use for an address. Supports byte notation like K, Mb, GB, etc.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Page Size Bytes",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	PageSizeBytes *string `json:"pageSizeBytes,omitempty"`
	// Number of paging files to cache in memory to avoid IO during paging navigation
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Page Max Cache Size",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:number"}
	PageMaxCacheSize *int32 `json:"pageMaxCacheSize,omitempty"`
	// what happens when an address where maxSizeBytes is specified becomes full
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Address Full Policy",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	AddressFullPolicy *string `json:"addressFullPolicy,omitempty"`
	// how many days to keep message counter history for this address
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Message Counter History Day Limit",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:number"}
	MessageCounterHistoryDayLimit *int32 `json:"messageCounterHistoryDayLimit,omitempty"`
	// This is deprecated please use default-last-value-queue instead.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Last Value Queue",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:booleanSwitch"}
	LastValueQueue *bool `json:"lastValueQueue,omitempty"`
	// whether to treat the queues under the address as a last value queues by default
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Default Last Value Queue",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:booleanSwitch"}
	DefaultLastValueQueue *bool `json:"defaultLastValueQueue,omitempty"`
	// the property to use as the key for a last value queue by default
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Default Last Value Key",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	DefaultLastValueKey *string `json:"defaultLastValueKey,omitempty"`
	// whether the queue should be non-destructive by default
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Default Non Destructive",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:booleanSwitch"}
	DefaultNonDestructive *bool `json:"defaultNonDestructive,omitempty"`
	// whether to treat the queues under the address as exclusive queues by default
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Default Exclusive Queue",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:booleanSwitch"}
	DefaultExclusiveQueue *bool `json:"defaultExclusiveQueue,omitempty"`
	// whether to rebalance groups when a consumer is added
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Default Group Rebalance",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:booleanSwitch"}
	DefaultGroupRebalance *bool `json:"defaultGroupRebalance,omitempty"`
	// whether to pause dispatch when rebalancing groups
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Default Group Rebalance Pause Dispatch",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:booleanSwitch"}
	DefaultGroupRebalancePauseDispatch *bool `json:"defaultGroupRebalancePauseDispatch,omitempty"`
	// number of buckets to use for grouping, -1 (default) is unlimited and uses the raw group, 0 disables message groups.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Default Group Buckets",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:number"}
	DefaultGroupBuckets *int32 `json:"defaultGroupBuckets,omitempty"`
	// key used to mark a message is first in a group for a consumer
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Default Group First Key",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	DefaultGroupFirstKey *string `json:"defaultGroupFirstKey,omitempty"`
	// the default number of consumers needed before dispatch can start for queues under the address.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Default Consumers Before Dispatch",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:number"}
	DefaultConsumersBeforeDispatch *int32 `json:"defaultConsumersBeforeDispatch,omitempty"`
	// the default delay (in milliseconds) to wait before dispatching if number of consumers before dispatch is not met for queues under the address.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Default Delay Before Dispatch",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:number"}
	DefaultDelayBeforeDispatch *int32 `json:"defaultDelayBeforeDispatch,omitempty"`
	// how long (in ms) to wait after the last consumer is closed on a queue before redistributing messages.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Redistribution Delay",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:number"}
	RedistributionDelay *int32 `json:"redistributionDelay,omitempty"`
	// if there are no queues matching this address, whether to forward message to DLA (if it exists for this address)
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Send To DLA On No Route",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:booleanSwitch"}
	SendToDlaOnNoRoute *bool `json:"sendToDlaOnNoRoute,omitempty"`
	// The minimum rate of message consumption allowed before a consumer is considered "slow." Measured in messages-per-second.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Slow Consumer Threshold",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:number"}
	SlowConsumerThreshold *int32 `json:"slowConsumerThreshold,omitempty"`
	// what happens when a slow consumer is identified
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Slow Consumer Policy",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	SlowConsumerPolicy *string `json:"slowConsumerPolicy,omitempty"`
	// How often to check for slow consumers on a particular queue. Measured in seconds.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Slow Consumer Check Period",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:number"}
	SlowConsumerCheckPeriod *int32 `json:"slowConsumerCheckPeriod,omitempty"`
	// DEPRECATED. whether or not to automatically create JMS queues when a producer sends or a consumer connects to a queue
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Auto Create Jms Queues",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:booleanSwitch"}
	AutoCreateJmsQueues *bool `json:"autoCreateJmsQueues,omitempty"`
	// DEPRECATED. whether or not to delete auto-created JMS queues when the queue has 0 consumers and 0 messages
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Auto Delete Jms Queues",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:booleanSwitch"}
	AutoDeleteJmsQueues *bool `json:"autoDeleteJmsQueues,omitempty"`
	// DEPRECATED. whether or not to automatically create JMS topics when a producer sends or a consumer subscribes to a topic
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Auto Create Jms Topics",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:booleanSwitch"}
	AutoCreateJmsTopics *bool `json:"autoCreateJmsTopics,omitempty"`
	// DEPRECATED. whether or not to delete auto-created JMS topics when the last subscription is closed
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Auto Delete Jms Topics",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:booleanSwitch"}
	AutoDeleteJmsTopics *bool `json:"autoDeleteJmsTopics,omitempty"`
	// whether or not to automatically create a queue when a client sends a message to or attempts to consume a message from a queue
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Auto Create Queues",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:booleanSwitch"}
	AutoCreateQueues *bool `json:"autoCreateQueues,omitempty"`
	// whether or not to delete auto-created queues when the queue has 0 consumers and 0 messages
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Auto Delete Queues",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:booleanSwitch"}
	AutoDeleteQueues *bool `json:"autoDeleteQueues,omitempty"`
	// whether or not to delete created queues when the queue has 0 consumers and 0 messages
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Auto Delete Created Queues",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:booleanSwitch"}
	AutoDeleteCreatedQueues *bool `json:"autoDeleteCreatedQueues,omitempty"`
	// how long to wait (in milliseconds) before deleting auto-created queues after the queue has 0 consumers.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Auto Delete Queues Delay",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:number"}
	AutoDeleteQueuesDelay *int32 `json:"autoDeleteQueuesDelay,omitempty"`
	// the message count the queue must be at or below before it can be evaluated to be auto deleted, 0 waits until empty queue (default) and -1 disables this check.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Auto Delete Queues Message Count",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:number"}
	AutoDeleteQueuesMessageCount *int32 `json:"autoDeleteQueuesMessageCount,omitempty"`
	//What to do when a queue is no longer in broker.xml.  OFF = will do nothing queues will remain, FORCE = delete queues even if messages remaining.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Config Delete Queues",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	ConfigDeleteQueues *string `json:"configDeleteQueues,omitempty"`
	// whether or not to automatically create addresses when a client sends a message to or attempts to consume a message from a queue mapped to an address that doesnt exist
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Auto Create Addresses",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:booleanSwitch"}
	AutoCreateAddresses *bool `json:"autoCreateAddresses,omitempty"`
	// whether or not to delete auto-created addresses when it no longer has any queues
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Auto Delete Addresses",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:booleanSwitch"}
	AutoDeleteAddresses *bool `json:"autoDeleteAddresses,omitempty"`
	// how long to wait (in milliseconds) before deleting auto-created addresses after they no longer have any queues
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Auto Delete Addresses Delay",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:number"}
	AutoDeleteAddressesDelay *int32 `json:"autoDeleteAddressesDelay,omitempty"`
	// What to do when an address is no longer in broker.xml.  OFF = will do nothing addresses will remain, FORCE = delete address and its queues even if messages remaining.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Config Delete Addresses",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	ConfigDeleteAddresses *string `json:"configDeleteAddresses,omitempty"`
	// how many message a management resource can browse
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Management Browse Page Size",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:number"}
	ManagementBrowsePageSize *int32 `json:"managementBrowsePageSize,omitempty"`
	// purge the contents of the queue once there are no consumers
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Default Purge On No Consumers",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:booleanSwitch"}
	DefaultPurgeOnNoConsumers *bool `json:"defaultPurgeOnNoConsumers,omitempty"`
	// the maximum number of consumers allowed on this queue at any one time
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Default Max Consumers",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:number"}
	DefaultMaxConsumers *int32 `json:"defaultMaxConsumers,omitempty"`
	// the routing-type used on auto-created queues
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Default Queue Routing Type",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	DefaultQueueRoutingType *string `json:"defaultQueueRoutingType,omitempty"`
	// the routing-type used on auto-created addresses
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Default Address Routing Type",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	DefaultAddressRoutingType *string `json:"defaultAddressRoutingType,omitempty"`
	// the default window size for a consumer
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Default Consumer Window Size",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:number"}
	DefaultConsumerWindowSize *int32 `json:"defaultConsumerWindowSize,omitempty"`
	// the default ring-size value for any matching queue which doesnt have ring-size explicitly defined
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Default Ring Size",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:number"}
	DefaultRingSize *int32 `json:"defaultRingSize,omitempty"`
	// the number of messages to preserve for future queues created on the matching address
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Retroactive Message Count",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:number"}
	RetroactiveMessageCount *int32 `json:"retroactiveMessageCount,omitempty"`
	// whether or not to enable metrics for metrics plugins on the matching address
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Enable Metrics",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:booleanSwitch"}
	EnableMetrics *bool `json:"enableMetrics,omitempty"`
	// pattern for matching settings against addresses; can use wildards
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Match",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	Match string `json:"match,omitempty"`
	// max size of the message returned from management API, default 256
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Max Management Message Size Limit",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:number"}
	ManagementMessageAttributeSizeLimit *int32 `json:"managementMessageAttributeSizeLimit,omitempty"`
	// Unit used in specifying slow consumer threshold, default is MESSAGE_PER_SECOND
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Slow Consumer Threshold Measurement Unit",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	SlowConsumerThresholdMeasurementUnit *string `json:"slowConsumerThresholdMeasurementUnit,omitempty"`
	// Whether or not set the timestamp of arrival on messages. default false
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Enable Ingress Timestamp",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:booleanSwitch"}
	EnableIngressTimestamp *bool `json:"enableIngressTimestamp,omitempty"`
	// What to do when a divert is no longer in broker.xml.  OFF = will do nothing and divert will remain(default), FORCE = delete divert
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Config Delete Diverts",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	ConfigDeleteDiverts *string `json:"configDeleteDiverts,omitempty"`
	// the maximum number of messages allowed on the address (default -1).  This is used in PAGING, BLOCK and FAIL policies. It does not support notations and it is a simple number of messages allowed.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Max Size Messages",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:number"}
	MaxSizeMessages *int64 `json:"maxSizeMessages,omitempty"`
}

type DeploymentPlanType struct {
	//The image used for the broker, all upgrades are disabled. Needs a corresponding initImage
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Image",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	Image string `json:"image,omitempty"`
	// The init container image used to configure broker, all upgrades are disabled. Needs a corresponding image
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Init Image",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	InitImage string `json:"initImage,omitempty"`
	//The image pull secrets name used to retrieve secrets for image pulls.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Image Pull Secrets"
	ImagePullSecrets []corev1.LocalObjectReference `json:"imagePullSecrets,omitempty"`
	// The number of broker pods to deploy
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Size",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:podCount"}
	Size *int32 `json:"size,omitempty"`
	// If true require user password login credentials for broker protocol ports
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Require Login",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:booleanSwitch"}
	RequireLogin bool `json:"requireLogin,omitempty"`
	// If true use persistent volume via persistent volume claim for journal storage
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Persistence Enabled",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:booleanSwitch"}
	PersistenceEnabled bool `json:"persistenceEnabled,omitempty"`
	// If aio use ASYNCIO, if nio use NIO for journal IO
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Journal Type",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	JournalType string `json:"journalType,omitempty"`
	//If true migrate messages on scaledown
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Message Migration",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:booleanSwitch"}
	MessageMigration *bool `json:"messageMigration,omitempty"`
	// Specifies the minimum/maximum amount of compute resources required/allowed
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Resource Requirements",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:resourceRequirements"}
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`
	// Specifies the storage configurations
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Storage Configurations"
	Storage StorageType `json:"storage,omitempty"`
	// Specifies the topology spread constraints
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Topology Spread Constraints"
	TopologySpreadConstraints []corev1.TopologySpreadConstraint `json:"topologySpreadConstraints,omitempty"`
	// If true enable the Jolokia JVM Agent
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Jolokia Agent Enabled",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:booleanSwitch"}
	JolokiaAgentEnabled bool `json:"jolokiaAgentEnabled,omitempty"`
	// If true enable the management role based access control
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Management RBAC Enabled",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:booleanSwitch"}
	ManagementRBACEnabled bool `json:"managementRBACEnabled,omitempty"`
	// Specifies extra mounts
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Extra Mounts"
	ExtraMounts ExtraMountsType `json:"extraMounts,omitempty"`
	// Whether broker is clustered
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Clustered",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:booleanSwitch"}
	Clustered *bool `json:"clustered,omitempty"`
	// Specifies the pod security configurations
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Pod Security Configurations"
	PodSecurity PodSecurityType `json:"podSecurity,omitempty"`
	// Specifies the startup probe configuration
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Startup Probe Configurations"
	StartupProbe *corev1.Probe `json:"startupProbe,omitempty"`
	// Specifies the liveness probe configuration
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Liveness Probe Configurations"
	LivenessProbe *corev1.Probe `json:"livenessProbe,omitempty"`
	// Specifies the readiness probe configuration
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Readiness Probe Configurations"
	ReadinessProbe *corev1.Probe `json:"readinessProbe,omitempty"`
	// Whether or not to install the artemis metrics plugin
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Enable Metrics Plugin",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:booleanSwitch"}
	EnableMetricsPlugin *bool `json:"enableMetricsPlugin,omitempty"`
	// Specifies the tolerations
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Tolerations"
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`
	// Assign labels to broker pods, the keys `Broker` and `application` are not allowed
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Labels"
	Labels map[string]string `json:"labels,omitempty"`
	// Specifies the node selector
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Node Selector",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:selector"}
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`
	// Specifies affinity configuration
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Affinity Configurations"
	Affinity AffinityConfig `json:"affinity,omitempty"`
	// Specifies the pod security context
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Pod Security Context"
	PodSecurityContext *corev1.PodSecurityContext `json:"podSecurityContext,omitempty"`
	// Custom annotations to be added to broker pods
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Annotations"
	Annotations map[string]string `json:"annotations,omitempty"`
	// Specifies the pod disruption budget
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Pod Disruption Budget"
	PodDisruptionBudget *policyv1.PodDisruptionBudgetSpec `json:"podDisruptionBudget,omitempty"`
	// Specifies the Revision History Limit of the statefulset
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Revision History Limit"
	RevisionHistoryLimit *int32 `json:"revisionHistoryLimit,omitempty"`
	// Specifies the container security context
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Container Security Context"
	ContainerSecurityContext *corev1.SecurityContext `json:"containerSecurityContext,omitempty"`
	// Specifies additional Volumes to be attached to the broker pods
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Extra Volumes"
	ExtraVolumes []corev1.Volume `json:"extraVolumes,omitempty"`
	// Specifies mount options for extraVolumes
	// +optional
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Extra Mount Options"
	ExtraVolumeMounts []corev1.VolumeMount `json:"extraVolumeMounts,omitempty"`
	// Specifies Extra Volume Claims Templates for the broker pods
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Extra Volume Claims Templates"
	ExtraVolumeClaimTemplates []VolumeClaimTemplate `json:"extraVolumeClaimTemplates,omitempty"`
}
type VolumeClaimTemplate struct {
	// Specifies the desired metadata of a volume claim
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Metadata"
	ObjectMeta `json:"metadata,omitempty"`

	// Specifies the desired characteristics of a volume claim
	// More info: https://kubernetes.io/docs/concepts/storage/persistent-volumes#persistentvolumeclaims
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Spec"
	Spec corev1.PersistentVolumeClaimSpec `json:"spec,omitempty"`
}

type ObjectMeta struct {
	// Name must be unique within a namespace. Is required when creating resources, although
	// some resources may allow a client to request the generation of an appropriate name
	// automatically. Name is primarily intended for creation idempotence and configuration
	// definition.
	// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/names#names
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Name",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	Name string `json:"name,omitempty"`

	// Annotations is an unstructured key value map stored with a resource that may be
	// set by external tools to store and retrieve arbitrary metadata. They are not
	// queryable and should be preserved when modifying objects.
	// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/annotations
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Annotations"
	Annotations map[string]string `json:"annotations,omitempty"`

	// Map of string keys and values that can be used to organize and categorize
	// (scope and select) objects. May match selectors of replication controllers
	// and services.
	// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/labels
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Labels"
	Labels map[string]string `json:"labels,omitempty"`
}

type ResourceTemplate struct {
	// Select which resources to match, an empty selector will match all resources
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Selector"
	Selector *ResourceSelector `json:"selector,omitempty"`

	// Custom annotations
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Annotations"
	Annotations map[string]string `json:"annotations,omitempty"`

	// Custom labels
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Labels"
	Labels map[string]string `json:"labels,omitempty"`

	// Custom attributes applied as strategic merge patch by the operator.
	// +kubebuilder:pruning:PreserveUnknownFields
	// +kubebuilder:validation:Schemaless
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Patch"
	Patch runtime.RawExtension `json:"patch,omitempty"`
}

// match criteria to restrict the selection of resources
type ResourceSelector struct {
	// APIGroup is the group version string of resources to match
	APIGroup *string `json:"apiGroup,omitempty"`
	// Kind is the type of resources to match
	Kind *string `json:"kind,omitempty"`
	// Name is the pattern matcher for the Name of resources to match
	Name *string `json:"name,omitempty"`
}

// Affinity is a group of affinity scheduling rules.
type AffinityConfig struct {
	// Describes node affinity scheduling rules for the pod.
	// +optional
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Node Affinity",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:nodeAffinity"}
	NodeAffinity *corev1.NodeAffinity `json:"nodeAffinity,omitempty" protobuf:"bytes,1,opt,name=nodeAffinity"`
	// Describes pod affinity scheduling rules (e.g. co-locate this pod in the same node, zone, etc. as some other pod(s)).
	// +optional
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Pod Affinity",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:podAffinity"}
	PodAffinity *corev1.PodAffinity `json:"podAffinity,omitempty" protobuf:"bytes,2,opt,name=podAffinity"`
	// Describes pod anti-affinity scheduling rules (e.g. avoid putting this pod in the same node, zone, etc. as some other pod(s)).
	// +optional
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Pod Anti Affinity",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:podAntiAffinity"}
	PodAntiAffinity *corev1.PodAntiAffinity `json:"podAntiAffinity,omitempty" protobuf:"bytes,3,opt,name=podAntiAffinity"`
}

type PodSecurityType struct {
	// ServiceAccount Name of the pod
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Service Account Name",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	ServiceAccountName *string `json:"serviceAccountName,omitempty"`
	// runAsUser as defined in PodSecurityContext for the pod
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Run As User",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:number"}
	RunAsUser *int64 `json:"runAsUser,omitempty"`
}

type ExtraMountsType struct {
	// Specifies ConfigMap names
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="ConfigMap Names"
	ConfigMaps []string `json:"configMaps,omitempty"`
	// Specifies Secret names
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Secret Names"
	Secrets []string `json:"secrets,omitempty"`
}

type StorageType struct {
	// The storage size
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Size",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	Size string `json:"size,omitempty"`
	// The storageClassName to be used in PVC
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Storage Class Name",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	StorageClassName string `json:"storageClassName,omitempty"`
}

// +kubebuilder:validation:Enum=ingress;route
type ExposeMode string

var ExposeModes = struct {
	Ingress ExposeMode
	Route   ExposeMode
}{
	Ingress: "ingress",
	Route:   "route",
}

type AcceptorType struct {
	// The acceptor name
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Name",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	Name string `json:"name"`
	// Port number
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Port",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:number"}
	Port int32 `json:"port,omitempty"`
	// The protocols to enable for this acceptor
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Protocols",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	Protocols string `json:"protocols,omitempty"`
	// Whether or not to enable SSL on this port
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="SSL Enabled",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:booleanSwitch"}
	SSLEnabled bool `json:"sslEnabled,omitempty"`
	// Name of the secret to use for ssl information
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="SSL Secret",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	SSLSecret string `json:"sslSecret,omitempty"`
	// Comma separated list of cipher suites used for SSL communication.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Enabled Cipher Suites",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	EnabledCipherSuites string `json:"enabledCipherSuites,omitempty"`
	// Comma separated list of protocols used for SSL communication.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Enabled Protocols",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	EnabledProtocols string `json:"enabledProtocols,omitempty"`
	// Tells a client connecting to this acceptor that 2-way SSL is required. This property takes precedence over wantClientAuth.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Need Client Auth",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:booleanSwitch"}
	NeedClientAuth bool `json:"needClientAuth,omitempty"`
	// Tells a client connecting to this acceptor that 2-way SSL is requested but not required. Overridden by needClientAuth.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Want Client Auth",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:booleanSwitch"}
	WantClientAuth bool `json:"wantClientAuth,omitempty"`
	// The CN of the connecting client's SSL certificate will be compared to its hostname to verify they match. This is useful only for 2-way SSL.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Verify Host",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:booleanSwitch"}
	VerifyHost bool `json:"verifyHost,omitempty"`
	// Used to change the SSL Provider between JDK and OPENSSL. The default is JDK.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="SSL Provider",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	SSLProvider string `json:"sslProvider,omitempty"`
	// A regular expression used to match the server_name extension on incoming SSL connections. If the name doesn't match then the connection to the acceptor will be rejected.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="SNI Host",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	SNIHost string `json:"sniHost,omitempty"`
	// Whether or not to expose this acceptor
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Expose",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:booleanSwitch"}
	Expose bool `json:"expose,omitempty"`
	// Mode to expose the acceptor. Currently the supported modes are `route` and `ingress`. It is ignored when the field `Expose` is false. Default is `route` on OpenShift and `ingress` on Kubernetes. \n\n* `route` mode uses OpenShift Routes to expose the acceptor.\n* `ingress` mode uses Kubernetes Nginx Ingress to expose the acceptor with TLS passthrough.\n"
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Expose Mode",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	ExposeMode *ExposeMode `json:"exposeMode,omitempty"`
	// To indicate which kind of routing type to use.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Anycast Prefix",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	AnycastPrefix string `json:"anycastPrefix,omitempty"`
	// To indicate which kind of routing type to use
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Multicast Prefix",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	MulticastPrefix string `json:"multicastPrefix,omitempty"`
	// Max number of connections allowed to make
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Connections Allowed",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:number"}
	ConnectionsAllowed int `json:"connectionsAllowed,omitempty"`
	// AMQP Minimum Large Message Size
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="AMQP Min Large Message Size",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:number"}
	AMQPMinLargeMessageSize int `json:"amqpMinLargeMessageSize,omitempty"`
	// For openwire protocol if advisory topics are enabled, default false
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Support Advisory",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:booleanSwitch"}
	SupportAdvisory *bool `json:"supportAdvisory,omitempty"`
	// If prevents advisory addresses/queues to be registered to management service, default false
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Suppress Internal Management Objects",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:booleanSwitch"}
	SuppressInternalManagementObjects *bool `json:"suppressInternalManagementObjects,omitempty"`
	// Whether to let the acceptor to bind to all interfaces
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Bind To All Interfaces",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:booleanSwitch"}
	BindToAllInterfaces *bool `json:"bindToAllInterfaces,omitempty"`
	// Provider used for the keystore; "SUN", "SunJCE", etc. Default is null
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="KeyStore Provider",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	KeyStoreProvider string `json:"keyStoreProvider,omitempty"`
	// Type of truststore being used; "JKS", "JCEKS", "PKCS12", etc. Default in broker is "JKS"
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="TrustStore Type",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	TrustStoreType string `json:"trustStoreType,omitempty"`
	// Provider used for the truststore; "SUN", "SunJCE", etc. Default in broker is null
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="TrustStore Provider",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	TrustStoreProvider string `json:"trustStoreProvider,omitempty"`
	// Host for Ingress and Route resources of the acceptor. It supports the following variables: $(CR_NAME), $(CR_NAMESPACE), $(BROKER_ORDINAL), $(ITEM_NAME), $(RES_NAME) and $(INGRESS_DOMAIN). It is required for the acceptors exposed with the ingress mode when the ingress domain is not specified.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Ingress Host",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	IngressHost string `json:"ingressHost,omitempty"`
	// The name of the truststore secret.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Trust Secret",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	TrustSecret *string `json:"trustSecret,omitempty"`
}

type ConnectorType struct {
	// The name of the connector
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Name",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	Name string `json:"name"`
	// The type either tcp or vm
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Type",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	Type string `json:"type,omitempty"`
	// Hostname or IP to connect to
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Host",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	Host string `json:"host"`
	// Port number
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Port",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:number"}
	Port int32 `json:"port"`
	//  Whether or not to enable SSL on this port
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="SSL Enabled",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:booleanSwitch"}
	SSLEnabled bool `json:"sslEnabled,omitempty"`
	// Name of the secret to use for ssl information
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="SSL Secret",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	SSLSecret string `json:"sslSecret,omitempty"`
	// Comma separated list of cipher suites used for SSL communication.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Enabled Cipher Suites",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	EnabledCipherSuites string `json:"enabledCipherSuites,omitempty"`
	// Comma separated list of protocols used for SSL communication.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Enabled Protocols",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	EnabledProtocols string `json:"enabledProtocols,omitempty"`
	// Tells a client connecting to this connector that 2-way SSL is required. This property takes precedence over wantClientAuth.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Need Client Auth",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:booleanSwitch"}
	NeedClientAuth bool `json:"needClientAuth,omitempty"`
	// Tells a client connecting to this connector that 2-way SSL is requested but not required. Overridden by needClientAuth.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Want Client Auth",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:booleanSwitch"}
	WantClientAuth bool `json:"wantClientAuth,omitempty"`
	// The CN of the connecting client's SSL certificate will be compared to its hostname to verify they match. This is useful only for 2-way SSL.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Verify Host",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:booleanSwitch"}
	VerifyHost bool `json:"verifyHost,omitempty"`
	// Used to change the SSL Provider between JDK and OPENSSL. The default is JDK.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="SSL Provider",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	SSLProvider string `json:"sslProvider,omitempty"`
	// A regular expression used to match the server_name extension on incoming SSL connections. If the name doesn't match then the connection to the acceptor will be rejected.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="SNI Host",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	SNIHost string `json:"sniHost,omitempty"`
	// Whether or not to expose this connector
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Expose",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:booleanSwitch"}
	Expose bool `json:"expose,omitempty"`
	// Mode to expose the connector. Currently the supported modes are `route` and `ingress`. It is ignored when the field `Expose` is false. Default is `route` on OpenShift and `ingress` on Kubernetes. \n\n* `route` mode uses OpenShift Routes to expose the connector.\n* `ingress` mode uses Kubernetes Nginx Ingress to expose the connector with TLS passthrough.\n"
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Expose Mode",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	ExposeMode *ExposeMode `json:"exposeMode,omitempty"`
	// Provider used for the keystore; "SUN", "SunJCE", etc. Default is null
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="KeyStore Provider",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	KeyStoreProvider string `json:"keyStoreProvider,omitempty"`
	// Type of truststore being used; "JKS", "JCEKS", "PKCS12", etc. Default in broker is "JKS"
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="TrustStore Type",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	TrustStoreType string `json:"trustStoreType,omitempty"`
	// Provider used for the truststore; "SUN", "SunJCE", etc. Default in broker is null
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="TrustStore Provider",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	TrustStoreProvider string `json:"trustStoreProvider,omitempty"`
	// Host for Ingress and Route resources of the acceptor. It supports the following variables: $(CR_NAME), $(CR_NAMESPACE), $(BROKER_ORDINAL), $(ITEM_NAME), $(RES_NAME) and $(INGRESS_DOMAIN). It is required for the connectors exposed with the ingress mode when the ingress domain is not specified.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Ingress Host",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	IngressHost string `json:"ingressHost,omitempty"`
	// The name of the truststore secret.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Trust Secret",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	TrustSecret *string `json:"trustSecret,omitempty"`
}

type ConsoleType struct {
	// The name of the console. Default is wconsj.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Name",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	Name string `json:"name,omitempty"`
	// Whether or not to expose this port
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Expose",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:booleanSwitch"}
	Expose bool `json:"expose,omitempty"`
	// Mode to expose the console. Currently the supported modes are `route` and `ingress`. It is ignored when the field `Expose` is false. Default is `route` on OpenShift and `ingress` on Kubernetes. \n\n* `route` mode uses OpenShift Routes to expose the console.\n* `ingress` mode uses Kubernetes Nginx Ingress to expose the console with TLS passthrough.\n"
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Expose Mode",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	ExposeMode *ExposeMode `json:"exposeMode,omitempty"`
	// Whether or not to enable SSL on this port
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="SSL Enabled",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:booleanSwitch"}
	SSLEnabled bool `json:"sslEnabled,omitempty"`
	// Name of the secret to use for ssl information
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="SSL Secret",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	SSLSecret string `json:"sslSecret,omitempty"`
	// If the embedded server requires client authentication
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Use Client Auth",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:booleanSwitch"}
	UseClientAuth bool `json:"useClientAuth,omitempty"`
	// Host for Ingress and Route resources of the acceptor. It supports the following variables: $(CR_NAME), $(CR_NAMESPACE), $(BROKER_ORDINAL), $(ITEM_NAME), $(RES_NAME) and $(INGRESS_DOMAIN). It is required for the console exposed with the ingress mode when the ingress domain is not specified.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Ingress Host",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	IngressHost string `json:"ingressHost,omitempty"`
	// The name of the truststore secret.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Trust Secret",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text"}
	TrustSecret *string `json:"trustSecret,omitempty"`
}

// Broker App product upgrade flags, this is deprecated in v1beta1, specifying the Version is sufficient
type BrokerUpgrades struct {
	// Set true to enable automatic micro version product upgrades, it is disabled by default.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Enable Upgrades",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:ui:booleanSwitch"}
	Enabled bool `json:"enabled"`
	// Set true to enable automatic minor product version upgrades, it is disabled by default. Requires spec.upgrades.enabled to be true.
	//+operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Include minor version upgrades",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:fieldDependency:upgrades.enabled:true","urn:alm:descriptor:com.tectonic.ui:ui:booleanSwitch"}
	Minor bool `json:"minor"`
}

// BrokerStatus defines the observed state of Broker
type BrokerStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Current state of the resource
	// Conditions represent the latest available observations of an object's state
	//+optional
	//+patchMergeKey=type
	//+patchStrategy=merge
	//+operator-sdk:csv:customresourcedefinitions:type=status,displayName="Conditions",xDescriptors="urn:alm:descriptor:io.kubernetes.conditions"
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,2,rep,name=conditions"`

	// The current pods
	//+operator-sdk:csv:customresourcedefinitions:type=status,displayName="Pods Status",xDescriptors="urn:alm:descriptor:com.tectonic.ui:podStatuses"
	PodStatus olm.DeploymentStatus `json:"podStatus"`
	//Deployments olm.DeploymentStatus `json:"podStatus"`
	//we probably use Deployments as operatorHub shows invalid field podStatus
	//see 3scale https://github.com/3scale/3scale-operator/blob/8abbabd926616b98db0e7e736e68e5ceba90ed9d/apis/apps/v1alpha1/apimanager_types.go#L87

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

type VersionStatus struct {

	//+operator-sdk:csv:customresourcedefinitions:type=status,displayName="BrokerVersion",xDescriptors="urn:alm:descriptor:text"
	BrokerVersion string `json:"brokerVersion,omitempty"`

	//+operator-sdk:csv:customresourcedefinitions:type=status,displayName="Image URI",xDescriptors="urn:alm:descriptor:org.w3:link"
	Image string `json:"image,omitempty"`
	//+operator-sdk:csv:customresourcedefinitions:type=status,displayName="InitImage URI",xDescriptors="urn:alm:descriptor:org.w3:link"
	InitImage string `json:"initImage,omitempty"`
}

type UpgradeStatus struct {
	//+operator-sdk:csv:customresourcedefinitions:type=status,displayName="SecurityUpdates",xDescriptors="urn:alm:descriptor:text"
	SecurityUpdates bool `json:"securityUpdates"` // false if image != "" && init image != ""

	//+operator-sdk:csv:customresourcedefinitions:type=status,displayName="MajorUpdates",xDescriptors="urn:alm:descriptor:text"
	MajorUpdates bool `json:"majorUpdates"` // true for empty version, false if version = x
	//+operator-sdk:csv:customresourcedefinitions:type=status,displayName="MinorUpdates",xDescriptors="urn:alm:descriptor:text"
	MinorUpdates bool `json:"minorUpdates"` // false if version = x.y
	//+operator-sdk:csv:customresourcedefinitions:type=status,displayName="PatchUpdates",xDescriptors="urn:alm:descriptor:text"
	PatchUpdates bool `json:"patchUpdates"` // false if version = x.y.z
}

type ExternalConfigStatus struct {
	//+operator-sdk:csv:customresourcedefinitions:type=status,displayName="Name",xDescriptors="urn:alm:descriptor:text"
	Name string `json:"name"`
	//+operator-sdk:csv:customresourcedefinitions:type=status,displayName="Resource Version",xDescriptors="urn:alm:descriptor:text"
	ResourceVersion string `json:"resourceVersion"`
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

const (
	DeployedConditionType                         = "Deployed"
	DeployedConditionReadyReason                  = "AllPodsReady"
	DeployedConditionNotReadyReason               = "PodsNotReady"
	DeployedConditionZeroSizeReason               = "ZeroSizeDeployment"
	DeployedConditionValidationFailedReason       = "ValidationFailed"
	DeployedConditionCrudKindErrorReason          = "ResourceError"
	DeployedConditionNoMatchingServiceReason      = "NoMatchingService"
	DeployedConditionNoServiceCapacityReason      = "NoServiceCapacity"
	DeployedConditionPortConflictReason           = "PortConflict"
	DeployedConditionMatchedServiceNotFoundReason = "MatchedServiceNotFound"
	DeployedConditionProvisioningPendingReason    = "ProvisioningPending"
	DeployedConditionProvisionedReason            = "Provisioned"
	DeployedConditionSelectorEvaluationError      = "AppSelectorEvaluationError"
	DeployedConditionPortDiscoveryPendingReason   = "PortDiscoveryPending"

	AppsProvisionedConditionType                    = "AppsProvisioned"
	AppsProvisionedConditionSyncedReason            = "Synced"
	AppsProvisionedConditionWaitingReason           = "WaitingForBroker"
	AppsProvisionedConditionNotReadyReason          = "BrokerNotReady"
	AppsProvisionedConditionAddressClashReason      = "AddressClash"
	AppsProvisionedConditionInvalidAddressRefReason = "InvalidAddressRef"

	ValidConditionType                   = "Valid"
	ValidConditionSuccessReason          = "ValidationSucceded"
	ValidConditionFailureReason          = "ValidationFailed"
	ValidConditionUnknownReason          = "NonFatalValidationFailure"
	ValidConditionMissingResourcesReason = "MissingDependentResources"
	ValidConditionInvalidVersionReason   = "SpecVersionInvalid"
	ValidConditionInvalidResourceName    = "InvalidResourceName"
	ValidConditionAddressTypeError       = "AddressTypeError"
	ValidConditionAddressRefError        = "AddressRefError"
	ValidConditionAddressClashError      = "AddressClash"
	ValidConditionSpecSelectorError      = "SpecSelectorError"

	ValidConditionPDBNonNilSelectorReason            = "PodDisruptionBudgetNonNilSelector"
	ValidConditionFailedReservedLabelReason          = "ReservedLabelReference"
	ValidConditionFailedExtraMountReason             = "InvalidExtraMount"
	ValidConditionFailedDuplicateAcceptorPort        = "DuplicateAcceptorPort"
	ValidConditionFailedInvalidExposeMode            = "InvalidExposeMode"
	ValidConditionFailedInvalidIngressSettings       = "InvalidIngressSettings"
	ValidConditionInvalidCertSecretReason            = "InvalidCertSecret"
	ValidConditionFailedDuplicateBrokerPropertiesKey = "DuplicateBrokerPropertiesKey"
	ValidConditionInvalidInternalVarUsage            = "InvalidInternalVarUsage"

	ReadyConditionType      = "Ready"
	ReadyConditionReason    = "ResourceReady"
	NotReadyConditionReason = "WaitingForAllConditions"

	ConfigAppliedConditionType     = "BrokerPropertiesApplied"
	JaasConfigAppliedConditionType = "JaasPropertiesApplied"

	ConfigAppliedConditionSynchedReason          = "Applied"
	ConfigAppliedConditionSynchedWithErrorReason = "AppliedWithError"

	ConfigAppliedConditionUnknownReason                   = "UnableToRetrieveStatus"
	ConfigAppliedConditionOutOfSyncReason                 = "OutOfSync"
	ConfigAppliedConditionNoJolokiaClientsAvailableReason = "NoJolokiaClientsAvailable"

	BrokerVersionAlignedConditionType           = "BrokerVersionAligned"
	BrokerVersionAlignedConditionMatchReason    = "VersionMatch"
	BrokerVersionAlignedConditionMismatchReason = "VersionMismatch"

	ScaleDownPendingConditionType                       = "ScalingDown"
	ScaleDownPendingConditionPendingConfigAppliedReason = "PendingConfigApplied"
	ScaleDownPendingConditionPendingEmptyReason         = "PendingEmpty" // no messages
	ScaleDownPendingConditionPendingDeleteReason        = "PendingDelete"

	ReconcileBlockedType   = "ReconcileBlocked"
	ReconcileBlockedReason = "AnnotationPresent"
)
