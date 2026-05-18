package controllers

import (
	"encoding/json"
	"strings"

	brokerv1beta1 "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta1"
	ctrl "sigs.k8s.io/controller-runtime"
)

var qlog = ctrl.Log.WithName("queue_configuration")

var defaultRoutingType string = "MULTICAST"

type ActiveMQArtemisQueueConfiguration struct {
	Name                        *string `json:"name,omitempty"`
	Address                     *string `json:"address,omitempty"`
	RoutingType                 *string `json:"routing-type,omitempty"`
	FilterString                *string `json:"filter-string,omitempty"`
	Durable                     *bool   `json:"durable,omitempty"`
	User                        *string `json:"user,omitempty"`
	MaxConsumers                *int32  `json:"max-consumers,omitempty"`
	Exclusive                   *bool   `json:"exclusive,omitempty"`
	GroupRebalance              *bool   `json:"group-rebalance,omitempty"`
	GroupRebalancePauseDispatch *bool   `json:"group-rebalance-pause-dispatch,omitempty"`
	GroupBuckets                *int32  `json:"group-buckets,omitempty"`
	GroupFirstKey               *string `json:"group-first-key,omitempty"`
	LastValue                   *bool   `json:"last-value,omitempty"`
	LastValueKey                *string `json:"last-value-key,omitempty"`
	NonDestructive              *bool   `json:"non-destructive,omitempty"`
	PurgeOnNoConsumers          *bool   `json:"purge-on-no-consumers,omitempty"`
	Enabled                     *bool   `json:"enabled,omitempty"`
	ConsumersBeforeDispatch     *int32  `json:"consumers-before-dispatch,omitempty"`
	DelayBeforeDispatch         *int64  `json:"delay-before-dispatch,omitempty"`
	ConsumerPriority            *int32  `json:"consumer-priority,omitempty"`
	AutoDelete                  *bool   `json:"auto-delete,omitempty"`
	AutoDeleteDelay             *int64  `json:"auto-delete-delay,omitempty"`
	AutoDeleteMessageCount      *int64  `json:"auto-delete-message-count,omitempty"`
	RingSize                    *int64  `json:"ring-size,omitempty"`
	ConfigurationManaged        *bool   `json:"configuration-managed,omitempty"`
	Temporary                   *bool   `json:"temporary,omitempty"`
	AutoCreateAddress           *bool   `json:"auto-create-address,omitempty"`
	Transient                   *bool   `json:"transient,omitempty"`
	Internal                    *bool   `json:"internal,omitempty"`
	Id                          *int64  `json:"id,omitempty"`
}

// convert QueueConfiguration to json string
func GetQueueConfig(addressRes *brokerv1beta1.ActiveMQArtemisAddress) (string, bool, error) {
	ignoreIfExists := false
	addressSpec := addressRes.Spec
	configSpec := addressRes.Spec.QueueConfiguration

	if configSpec.IgnoreIfExists != nil {
		ignoreIfExists = *configSpec.IgnoreIfExists
	}

	artemisQueueConfig := ActiveMQArtemisQueueConfiguration{}

	artemisQueueConfig.Name = addressSpec.QueueName
	artemisQueueConfig.Address = &addressSpec.AddressName
	if addressSpec.RoutingType == nil {
		artemisQueueConfig.RoutingType = &defaultRoutingType
	} else {
		routingType := strings.ToUpper(*addressSpec.RoutingType)
		artemisQueueConfig.RoutingType = &routingType
	}
	artemisQueueConfig.FilterString = configSpec.FilterString
	artemisQueueConfig.Durable = configSpec.Durable
	artemisQueueConfig.User = configSpec.User
	artemisQueueConfig.MaxConsumers = configSpec.MaxConsumers
	artemisQueueConfig.Exclusive = configSpec.Exclusive
	artemisQueueConfig.GroupRebalance = configSpec.GroupRebalance
	artemisQueueConfig.GroupRebalancePauseDispatch = configSpec.GroupRebalancePauseDispatch
	artemisQueueConfig.GroupBuckets = configSpec.GroupBuckets
	artemisQueueConfig.GroupFirstKey = configSpec.GroupFirstKey
	artemisQueueConfig.LastValue = configSpec.LastValue
	artemisQueueConfig.LastValueKey = configSpec.LastValueKey
	artemisQueueConfig.NonDestructive = configSpec.NonDestructive
	artemisQueueConfig.PurgeOnNoConsumers = configSpec.PurgeOnNoConsumers
	artemisQueueConfig.Enabled = configSpec.Enabled
	artemisQueueConfig.ConsumersBeforeDispatch = configSpec.ConsumersBeforeDispatch
	artemisQueueConfig.DelayBeforeDispatch = configSpec.DelayBeforeDispatch
	artemisQueueConfig.ConsumerPriority = configSpec.ConsumerPriority
	artemisQueueConfig.AutoDelete = configSpec.AutoDelete
	artemisQueueConfig.AutoDeleteDelay = configSpec.AutoDeleteDelay
	artemisQueueConfig.AutoDeleteMessageCount = configSpec.AutoDeleteMessageCount
	artemisQueueConfig.RingSize = configSpec.RingSize
	artemisQueueConfig.ConfigurationManaged = configSpec.ConfigurationManaged
	artemisQueueConfig.Temporary = configSpec.Temporary
	artemisQueueConfig.AutoCreateAddress = configSpec.AutoCreateAddress

	bytes, err := json.Marshal(artemisQueueConfig)
	if err != nil {
		qlog.Error(err, "Error marshalling queue config", "config", artemisQueueConfig)
		return "", false, err
	}
	return string(bytes), ignoreIfExists, nil
}
