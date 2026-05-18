package config

import (
	brokerv1beta1 "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta1"
	brokerv2alpha3 "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v2alpha3"
	brokerv2alpha4 "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v2alpha4"
	brokerv2alpha5 "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v2alpha5"
	ctrl "sigs.k8s.io/controller-runtime"
)

// Todo:  should be a better way to do this.
func IsEqualV1Beta1(currentAddressSetting []brokerv1beta1.AddressSettingType, newAddressSetting []brokerv1beta1.AddressSettingType) bool {
	log := ctrl.Log.WithName("util_config")
	log.V(1).Info("Comparing addressSettings...", "current: ", currentAddressSetting, "new: ", newAddressSetting)

	for _, curSetting := range currentAddressSetting {

		var newSetting *brokerv1beta1.AddressSettingType = nil
		for _, setting := range newAddressSetting {
			if setting.Match == curSetting.Match {
				newSetting = &setting
				break
			}
		}
		if newSetting == nil {
			return false
		}
		//compare the whole struct
		if newSetting.DeadLetterAddress == nil {
			if curSetting.DeadLetterAddress != nil {
				return false
			}
		} else if curSetting.DeadLetterAddress != nil {
			if *curSetting.DeadLetterAddress != *newSetting.DeadLetterAddress {
				return false
			}
		} else {
			return false
		}
		if newSetting.AutoCreateDeadLetterResources == nil {
			if curSetting.AutoCreateDeadLetterResources != nil {
				return false
			}
		} else if curSetting.AutoCreateDeadLetterResources != nil {
			if *curSetting.AutoCreateDeadLetterResources != *newSetting.AutoCreateDeadLetterResources {
				return false
			}
		} else {
			return false
		}
		if newSetting.DeadLetterQueuePrefix == nil {
			if curSetting.DeadLetterQueuePrefix != nil {
				return false
			}
		} else if curSetting.DeadLetterQueuePrefix != nil {
			if *curSetting.DeadLetterQueuePrefix != *newSetting.DeadLetterQueuePrefix {
				return false
			}
		} else {
			return false
		}
		if newSetting.DeadLetterQueueSuffix == nil {
			if curSetting.DeadLetterQueueSuffix != nil {
				return false
			}
		} else if curSetting.DeadLetterQueueSuffix != nil {
			if *curSetting.DeadLetterQueueSuffix != *newSetting.DeadLetterQueueSuffix {
				return false
			}
		} else {
			return false
		}
		if newSetting.ExpiryAddress == nil {
			if curSetting.ExpiryAddress != nil {
				return false
			}
		} else if curSetting.ExpiryAddress != nil {
			if *curSetting.ExpiryAddress != *newSetting.ExpiryAddress {
				return false
			}
		} else {
			return false
		}
		if newSetting.AutoCreateExpiryResources == nil {
			if curSetting.AutoCreateExpiryResources != nil {
				return false
			}
		} else if curSetting.AutoCreateExpiryResources != nil {
			if *curSetting.AutoCreateExpiryResources != *newSetting.AutoCreateExpiryResources {
				return false
			}
		} else {
			return false
		}
		if newSetting.ExpiryQueuePrefix == nil {
			if curSetting.ExpiryQueuePrefix != nil {
				return false
			}
		} else if curSetting.ExpiryQueuePrefix != nil {
			if *curSetting.ExpiryQueuePrefix != *newSetting.ExpiryQueuePrefix {
				return false
			}
		} else {
			return false
		}
		if newSetting.ExpiryQueueSuffix == nil {
			if curSetting.ExpiryQueueSuffix != nil {
				return false
			}
		} else if curSetting.ExpiryQueueSuffix != nil {
			if *curSetting.ExpiryQueueSuffix != *newSetting.ExpiryQueueSuffix {
				return false
			}
		} else {
			return false
		}
		if newSetting.ExpiryDelay == nil {
			if curSetting.ExpiryDelay != nil {
				return false
			}
		} else if curSetting.ExpiryDelay != nil {
			if *curSetting.ExpiryDelay != *newSetting.ExpiryDelay {
				return false
			}
		} else {
			return false
		}
		if newSetting.MinExpiryDelay == nil {
			if curSetting.MinExpiryDelay != nil {
				return false
			}
		} else if curSetting.MinExpiryDelay != nil {
			if *curSetting.MinExpiryDelay != *newSetting.MinExpiryDelay {
				return false
			}
		} else {
			return false
		}
		if newSetting.MaxExpiryDelay == nil {
			if curSetting.MaxExpiryDelay != nil {
				return false
			}
		} else if curSetting.MaxExpiryDelay != nil {
			if *curSetting.MaxExpiryDelay != *newSetting.MaxExpiryDelay {
				return false
			}
		} else {
			return false
		}
		if newSetting.RedeliveryDelay == nil {
			if curSetting.RedeliveryDelay != nil {
				return false
			}
		} else if curSetting.RedeliveryDelay != nil {
			if *curSetting.RedeliveryDelay != *newSetting.RedeliveryDelay {
				return false
			}
		} else {
			return false
		}
		if newSetting.MaxRedeliveryDelay == nil {
			if curSetting.MaxRedeliveryDelay != nil {
				return false
			}
		} else if curSetting.MaxRedeliveryDelay != nil {
			if *curSetting.MaxRedeliveryDelay != *newSetting.MaxRedeliveryDelay {
				return false
			}
		} else {
			return false
		}
		if newSetting.MaxDeliveryAttempts == nil {
			if curSetting.MaxDeliveryAttempts != nil {
				return false
			}
		} else if curSetting.MaxDeliveryAttempts != nil {
			if *curSetting.MaxDeliveryAttempts != *newSetting.MaxDeliveryAttempts {
				return false
			}
		} else {
			return false
		}
		if newSetting.MaxSizeBytes == nil {
			if curSetting.MaxSizeBytes != nil {
				return false
			}
		} else if curSetting.MaxSizeBytes != nil {
			if *curSetting.MaxSizeBytes != *newSetting.MaxSizeBytes {
				return false
			}
		} else {
			return false
		}
		if newSetting.MaxSizeBytesRejectThreshold == nil {
			if curSetting.MaxSizeBytesRejectThreshold != nil {
				return false
			}
		} else if curSetting.MaxSizeBytesRejectThreshold != nil {
			if *curSetting.MaxSizeBytesRejectThreshold != *newSetting.MaxSizeBytesRejectThreshold {
				return false
			}
		} else {
			return false
		}
		if newSetting.PageSizeBytes == nil {
			if curSetting.PageSizeBytes != nil {
				return false
			}
		} else if curSetting.PageSizeBytes != nil {
			if *curSetting.PageSizeBytes != *newSetting.PageSizeBytes {
				return false
			}
		} else {
			return false
		}
		if newSetting.PageMaxCacheSize == nil {
			if curSetting.PageMaxCacheSize != nil {
				return false
			}
		} else if curSetting.PageMaxCacheSize != nil {
			if *curSetting.PageMaxCacheSize != *newSetting.PageMaxCacheSize {
				return false
			}
		} else {
			return false
		}
		if newSetting.AddressFullPolicy == nil {
			if curSetting.AddressFullPolicy != nil {
				return false
			}
		} else if curSetting.AddressFullPolicy != nil {
			if *curSetting.AddressFullPolicy != *newSetting.AddressFullPolicy {
				return false
			}
		} else {
			return false
		}
		if newSetting.MessageCounterHistoryDayLimit == nil {
			if curSetting.MessageCounterHistoryDayLimit != nil {
				return false
			}
		} else if curSetting.MessageCounterHistoryDayLimit != nil {
			if *curSetting.MessageCounterHistoryDayLimit != *newSetting.MessageCounterHistoryDayLimit {
				return false
			}
		} else {
			return false
		}
		if newSetting.LastValueQueue == nil {
			if curSetting.LastValueQueue != nil {
				return false
			}
		} else if curSetting.LastValueQueue != nil {
			if *curSetting.LastValueQueue != *newSetting.LastValueQueue {
				return false
			}
		} else {
			return false
		}
		if newSetting.DefaultLastValueQueue == nil {
			if curSetting.DefaultLastValueQueue != nil {
				return false
			}
		} else if curSetting.DefaultLastValueQueue != nil {
			if *curSetting.DefaultLastValueQueue != *newSetting.DefaultLastValueQueue {
				return false
			}
		} else {
			return false
		}
		if newSetting.DefaultLastValueKey == nil {
			if curSetting.DefaultLastValueKey != nil {
				return false
			}
		} else if curSetting.DefaultLastValueKey != nil {
			if *curSetting.DefaultLastValueKey != *newSetting.DefaultLastValueKey {
				return false
			}
		} else {
			return false
		}
		if newSetting.DefaultNonDestructive == nil {
			if curSetting.DefaultNonDestructive != nil {
				return false
			}
		} else if curSetting.DefaultNonDestructive != nil {
			if *curSetting.DefaultNonDestructive != *newSetting.DefaultNonDestructive {
				return false
			}
		} else {
			return false
		}
		if newSetting.DefaultExclusiveQueue == nil {
			if curSetting.DefaultExclusiveQueue != nil {
				return false
			}
		} else if curSetting.DefaultExclusiveQueue != nil {
			if *curSetting.DefaultExclusiveQueue != *newSetting.DefaultExclusiveQueue {
				return false
			}
		} else {
			return false
		}
		if newSetting.DefaultGroupRebalance == nil {
			if curSetting.DefaultGroupRebalance != nil {
				return false
			}
		} else if curSetting.DefaultGroupRebalance != nil {
			if *curSetting.DefaultGroupRebalance != *newSetting.DefaultGroupRebalance {
				return false
			}
		} else {
			return false
		}
		if newSetting.DefaultGroupRebalancePauseDispatch == nil {
			if curSetting.DefaultGroupRebalancePauseDispatch != nil {
				return false
			}
		} else if curSetting.DefaultGroupRebalancePauseDispatch != nil {
			if *curSetting.DefaultGroupRebalancePauseDispatch != *newSetting.DefaultGroupRebalancePauseDispatch {
				return false
			}
		} else {
			return false
		}
		if newSetting.DefaultGroupBuckets == nil {
			if curSetting.DefaultGroupBuckets != nil {
				return false
			}
		} else if curSetting.DefaultGroupBuckets != nil {
			if *curSetting.DefaultGroupBuckets != *newSetting.DefaultGroupBuckets {
				return false
			}
		} else {
			return false
		}
		if newSetting.DefaultGroupFirstKey == nil {
			if curSetting.DefaultGroupFirstKey != nil {
				return false
			}
		} else if curSetting.DefaultGroupFirstKey != nil {
			if *curSetting.DefaultGroupFirstKey != *newSetting.DefaultGroupFirstKey {
				return false
			}
		} else {
			return false
		}
		if newSetting.DefaultConsumersBeforeDispatch == nil {
			if curSetting.DefaultConsumersBeforeDispatch != nil {
				return false
			}
		} else if curSetting.DefaultConsumersBeforeDispatch != nil {
			if *curSetting.DefaultConsumersBeforeDispatch != *newSetting.DefaultConsumersBeforeDispatch {
				return false
			}
		} else {
			return false
		}
		if newSetting.DefaultDelayBeforeDispatch == nil {
			if curSetting.DefaultDelayBeforeDispatch != nil {
				return false
			}
		} else if curSetting.DefaultDelayBeforeDispatch != nil {
			if *curSetting.DefaultDelayBeforeDispatch != *newSetting.DefaultDelayBeforeDispatch {
				return false
			}
		} else {
			return false
		}
		if newSetting.RedistributionDelay == nil {
			if curSetting.RedistributionDelay != nil {
				return false
			}
		} else if curSetting.RedistributionDelay != nil {
			if *curSetting.RedistributionDelay != *newSetting.RedistributionDelay {
				return false
			}
		} else {
			return false
		}
		if newSetting.SendToDlaOnNoRoute == nil {
			if curSetting.SendToDlaOnNoRoute != nil {
				return false
			}
		} else if curSetting.SendToDlaOnNoRoute != nil {
			if *curSetting.SendToDlaOnNoRoute != *newSetting.SendToDlaOnNoRoute {
				return false
			}
		} else {
			return false
		}
		if newSetting.SlowConsumerThreshold == nil {
			if curSetting.SlowConsumerThreshold != nil {
				return false
			}
		} else if curSetting.SlowConsumerThreshold != nil {
			if *curSetting.SlowConsumerThreshold != *newSetting.SlowConsumerThreshold {
				return false
			}
		} else {
			return false
		}
		if newSetting.SlowConsumerPolicy == nil {
			if curSetting.SlowConsumerPolicy != nil {
				return false
			}
		} else if curSetting.SlowConsumerPolicy != nil {
			if *curSetting.SlowConsumerPolicy != *newSetting.SlowConsumerPolicy {
				return false
			}
		} else {
			return false
		}
		if newSetting.SlowConsumerCheckPeriod == nil {
			if curSetting.SlowConsumerCheckPeriod != nil {
				return false
			}
		} else if curSetting.SlowConsumerCheckPeriod != nil {
			if *curSetting.SlowConsumerCheckPeriod != *newSetting.SlowConsumerCheckPeriod {
				return false
			}
		} else {
			return false
		}
		if newSetting.AutoCreateJmsQueues == nil {
			if curSetting.AutoCreateJmsQueues != nil {
				return false
			}
		} else if curSetting.AutoCreateJmsQueues != nil {
			if *curSetting.AutoCreateJmsQueues != *newSetting.AutoCreateJmsQueues {
				return false
			}
		} else {
			return false
		}
		if newSetting.AutoDeleteJmsQueues == nil {
			if curSetting.AutoDeleteJmsQueues != nil {
				return false
			}
		} else if curSetting.AutoDeleteJmsQueues != nil {
			if *curSetting.AutoDeleteJmsQueues != *newSetting.AutoDeleteJmsQueues {
				return false
			}
		} else {
			return false
		}
		if newSetting.AutoCreateJmsTopics == nil {
			if curSetting.AutoCreateJmsTopics != nil {
				return false
			}
		} else if curSetting.AutoCreateJmsTopics != nil {
			if *curSetting.AutoCreateJmsTopics != *newSetting.AutoCreateJmsTopics {
				return false
			}
		} else {
			return false
		}
		if newSetting.AutoDeleteJmsTopics == nil {
			if curSetting.AutoDeleteJmsTopics != nil {
				return false
			}
		} else if curSetting.AutoDeleteJmsTopics != nil {
			if *curSetting.AutoDeleteJmsTopics != *newSetting.AutoDeleteJmsTopics {
				return false
			}
		} else {
			return false
		}
		if newSetting.AutoCreateQueues == nil {
			if curSetting.AutoCreateQueues != nil {
				return false
			}
		} else if curSetting.AutoCreateQueues != nil {
			if *curSetting.AutoCreateQueues != *newSetting.AutoCreateQueues {
				return false
			}
		} else {
			return false
		}
		if newSetting.AutoDeleteQueues == nil {
			if curSetting.AutoDeleteQueues != nil {
				return false
			}
		} else if curSetting.AutoDeleteQueues != nil {
			if *curSetting.AutoDeleteQueues != *newSetting.AutoDeleteQueues {
				return false
			}
		} else {
			return false
		}
		if newSetting.AutoDeleteCreatedQueues == nil {
			if curSetting.AutoDeleteCreatedQueues != nil {
				return false
			}
		} else if curSetting.AutoDeleteCreatedQueues != nil {
			if *curSetting.AutoDeleteCreatedQueues != *newSetting.AutoDeleteCreatedQueues {
				return false
			}
		} else {
			return false
		}
		if newSetting.AutoDeleteQueuesDelay == nil {
			if curSetting.AutoDeleteQueuesDelay != nil {
				return false
			}
		} else if curSetting.AutoDeleteQueuesDelay != nil {
			if *curSetting.AutoDeleteQueuesDelay != *newSetting.AutoDeleteQueuesDelay {
				return false
			}
		} else {
			return false
		}
		if newSetting.AutoDeleteQueuesMessageCount == nil {
			if curSetting.AutoDeleteQueuesMessageCount != nil {
				return false
			}
		} else if curSetting.AutoDeleteQueuesMessageCount != nil {
			if *curSetting.AutoDeleteQueuesMessageCount != *newSetting.AutoDeleteQueuesMessageCount {
				return false
			}
		} else {
			return false
		}
		if newSetting.ConfigDeleteQueues == nil {
			if curSetting.ConfigDeleteQueues != nil {
				return false
			}
		} else if curSetting.ConfigDeleteQueues != nil {
			if *curSetting.ConfigDeleteQueues != *newSetting.ConfigDeleteQueues {
				return false
			}
		} else {
			return false
		}
		if newSetting.AutoCreateAddresses == nil {
			if curSetting.AutoCreateAddresses != nil {
				return false
			}
		} else if curSetting.AutoCreateAddresses != nil {
			if *curSetting.AutoCreateAddresses != *newSetting.AutoCreateAddresses {
				return false
			}
		} else {
			return false
		}
		if newSetting.AutoDeleteAddresses == nil {
			if curSetting.AutoDeleteAddresses != nil {
				return false
			}
		} else if curSetting.AutoDeleteAddresses != nil {
			if *curSetting.AutoDeleteAddresses != *newSetting.AutoDeleteAddresses {
				return false
			}
		} else {
			return false
		}
		if newSetting.AutoDeleteAddressesDelay == nil {
			if curSetting.AutoDeleteAddressesDelay != nil {
				return false
			}
		} else if curSetting.AutoDeleteAddressesDelay != nil {
			if *curSetting.AutoDeleteAddressesDelay != *newSetting.AutoDeleteAddressesDelay {
				return false
			}
		} else {
			return false
		}
		if newSetting.ConfigDeleteAddresses == nil {
			if curSetting.ConfigDeleteAddresses != nil {
				return false
			}
		} else if curSetting.ConfigDeleteAddresses != nil {
			if *curSetting.ConfigDeleteAddresses != *newSetting.ConfigDeleteAddresses {
				return false
			}
		} else {
			return false
		}
		if newSetting.ManagementBrowsePageSize == nil {
			if curSetting.ManagementBrowsePageSize != nil {
				return false
			}
		} else if curSetting.ManagementBrowsePageSize != nil {
			if *curSetting.ManagementBrowsePageSize != *newSetting.ManagementBrowsePageSize {
				return false
			}
		} else {
			return false
		}
		if newSetting.DefaultPurgeOnNoConsumers == nil {
			if curSetting.DefaultPurgeOnNoConsumers != nil {
				return false
			}
		} else if curSetting.DefaultPurgeOnNoConsumers != nil {
			if *curSetting.DefaultPurgeOnNoConsumers != *newSetting.DefaultPurgeOnNoConsumers {
				return false
			}
		} else {
			return false
		}
		if newSetting.DefaultMaxConsumers == nil {
			if curSetting.DefaultMaxConsumers != nil {
				return false
			}
		} else if curSetting.DefaultMaxConsumers != nil {
			if *curSetting.DefaultMaxConsumers != *newSetting.DefaultMaxConsumers {
				return false
			}
		} else {
			return false
		}
		if newSetting.DefaultQueueRoutingType == nil {
			if curSetting.DefaultQueueRoutingType != nil {
				return false
			}
		} else if curSetting.DefaultQueueRoutingType != nil {
			if *curSetting.DefaultQueueRoutingType != *newSetting.DefaultQueueRoutingType {
				return false
			}
		} else {
			return false
		}
		if newSetting.DefaultAddressRoutingType == nil {
			if curSetting.DefaultAddressRoutingType != nil {
				return false
			}
		} else if curSetting.DefaultAddressRoutingType != nil {
			if *curSetting.DefaultAddressRoutingType != *newSetting.DefaultAddressRoutingType {
				return false
			}
		} else {
			return false
		}
		if newSetting.DefaultConsumerWindowSize == nil {
			if curSetting.DefaultConsumerWindowSize != nil {
				return false
			}
		} else if curSetting.DefaultConsumerWindowSize != nil {
			if *curSetting.DefaultConsumerWindowSize != *newSetting.DefaultConsumerWindowSize {
				return false
			}
		} else {
			return false
		}
		if newSetting.DefaultRingSize == nil {
			if curSetting.DefaultRingSize != nil {
				return false
			}
		} else if curSetting.DefaultRingSize != nil {
			if *curSetting.DefaultRingSize != *newSetting.DefaultRingSize {
				return false
			}
		} else {
			return false
		}
		if newSetting.RetroactiveMessageCount == nil {
			if curSetting.RetroactiveMessageCount != nil {
				return false
			}
		} else if curSetting.RetroactiveMessageCount != nil {
			if *curSetting.RetroactiveMessageCount != *newSetting.RetroactiveMessageCount {
				return false
			}
		} else {
			return false
		}
		if newSetting.EnableMetrics == nil {
			if curSetting.EnableMetrics != nil {
				return false
			}
		} else if curSetting.EnableMetrics != nil {
			if *curSetting.EnableMetrics != *newSetting.EnableMetrics {
				return false
			}
		} else {
			return false
		}
		if newSetting.ManagementMessageAttributeSizeLimit == nil {
			if curSetting.ManagementMessageAttributeSizeLimit != nil {
				return false
			}
		} else if curSetting.ManagementMessageAttributeSizeLimit != nil {
			if *curSetting.ManagementMessageAttributeSizeLimit != *newSetting.ManagementMessageAttributeSizeLimit {
				return false
			}
		} else {
			return false
		}
		if newSetting.SlowConsumerThresholdMeasurementUnit == nil {
			if curSetting.SlowConsumerThresholdMeasurementUnit != nil {
				return false
			}
		} else if curSetting.SlowConsumerThresholdMeasurementUnit != nil {
			if *curSetting.SlowConsumerThresholdMeasurementUnit != *newSetting.SlowConsumerThresholdMeasurementUnit {
				return false
			}
		} else {
			return false
		}
		if newSetting.EnableIngressTimestamp == nil {
			if curSetting.EnableIngressTimestamp != nil {
				return false
			}
		} else if curSetting.EnableIngressTimestamp != nil {
			if *curSetting.EnableIngressTimestamp != *newSetting.EnableIngressTimestamp {
				return false
			}
		} else {
			return false
		}
		if newSetting.ConfigDeleteDiverts == nil {
			if curSetting.ConfigDeleteDiverts != nil {
				return false
			}
		} else if curSetting.ConfigDeleteDiverts != nil {
			if *curSetting.ConfigDeleteDiverts != *newSetting.ConfigDeleteDiverts {
				return false
			}
		} else {
			return false
		}
		if newSetting.MaxSizeMessages == nil {
			if curSetting.MaxSizeMessages != nil {
				return false
			}
		} else if curSetting.MaxSizeMessages != nil {
			if *curSetting.MaxSizeMessages != *newSetting.MaxSizeMessages {
				return false
			}
		} else {
			return false
		}
	}
	return true
}

// assuming the lengths of 2 array are equal.
func IsEqualV2Alpha5(currentAddressSetting []brokerv2alpha5.AddressSettingType, newAddressSetting []brokerv2alpha5.AddressSettingType) bool {
	log := ctrl.Log.WithName("util_config")
	log.V(1).Info("Comparing addressSettings...", "current: ", currentAddressSetting, "new: ", newAddressSetting)

	for _, curSetting := range currentAddressSetting {

		var newSetting *brokerv2alpha5.AddressSettingType = nil
		for _, setting := range newAddressSetting {
			if setting.Match == curSetting.Match {
				newSetting = &setting
				break
			}
		}
		if newSetting == nil {
			return false
		}
		//compare the whole struct
		if newSetting.DeadLetterAddress == nil {
			if curSetting.DeadLetterAddress != nil {
				return false
			}
		} else if curSetting.DeadLetterAddress != nil {
			if *curSetting.DeadLetterAddress != *newSetting.DeadLetterAddress {
				return false
			}
		} else {
			return false
		}
		if newSetting.AutoCreateDeadLetterResources == nil {
			if curSetting.AutoCreateDeadLetterResources != nil {
				return false
			}
		} else if curSetting.AutoCreateDeadLetterResources != nil {
			if *curSetting.AutoCreateDeadLetterResources != *newSetting.AutoCreateDeadLetterResources {
				return false
			}
		} else {
			return false
		}
		if newSetting.DeadLetterQueuePrefix == nil {
			if curSetting.DeadLetterQueuePrefix != nil {
				return false
			}
		} else if curSetting.DeadLetterQueuePrefix != nil {
			if *curSetting.DeadLetterQueuePrefix != *newSetting.DeadLetterQueuePrefix {
				return false
			}
		} else {
			return false
		}
		if newSetting.DeadLetterQueueSuffix == nil {
			if curSetting.DeadLetterQueueSuffix != nil {
				return false
			}
		} else if curSetting.DeadLetterQueueSuffix != nil {
			if *curSetting.DeadLetterQueueSuffix != *newSetting.DeadLetterQueueSuffix {
				return false
			}
		} else {
			return false
		}
		if newSetting.ExpiryAddress == nil {
			if curSetting.ExpiryAddress != nil {
				return false
			}
		} else if curSetting.ExpiryAddress != nil {
			if *curSetting.ExpiryAddress != *newSetting.ExpiryAddress {
				return false
			}
		} else {
			return false
		}
		if newSetting.AutoCreateExpiryResources == nil {
			if curSetting.AutoCreateExpiryResources != nil {
				return false
			}
		} else if curSetting.AutoCreateExpiryResources != nil {
			if *curSetting.AutoCreateExpiryResources != *newSetting.AutoCreateExpiryResources {
				return false
			}
		} else {
			return false
		}
		if newSetting.ExpiryQueuePrefix == nil {
			if curSetting.ExpiryQueuePrefix != nil {
				return false
			}
		} else if curSetting.ExpiryQueuePrefix != nil {
			if *curSetting.ExpiryQueuePrefix != *newSetting.ExpiryQueuePrefix {
				return false
			}
		} else {
			return false
		}
		if newSetting.ExpiryQueueSuffix == nil {
			if curSetting.ExpiryQueueSuffix != nil {
				return false
			}
		} else if curSetting.ExpiryQueueSuffix != nil {
			if *curSetting.ExpiryQueueSuffix != *newSetting.ExpiryQueueSuffix {
				return false
			}
		} else {
			return false
		}
		if newSetting.ExpiryDelay == nil {
			if curSetting.ExpiryDelay != nil {
				return false
			}
		} else if curSetting.ExpiryDelay != nil {
			if *curSetting.ExpiryDelay != *newSetting.ExpiryDelay {
				return false
			}
		} else {
			return false
		}
		if newSetting.MinExpiryDelay == nil {
			if curSetting.MinExpiryDelay != nil {
				return false
			}
		} else if curSetting.MinExpiryDelay != nil {
			if *curSetting.MinExpiryDelay != *newSetting.MinExpiryDelay {
				return false
			}
		} else {
			return false
		}
		if newSetting.MaxExpiryDelay == nil {
			if curSetting.MaxExpiryDelay != nil {
				return false
			}
		} else if curSetting.MaxExpiryDelay != nil {
			if *curSetting.MaxExpiryDelay != *newSetting.MaxExpiryDelay {
				return false
			}
		} else {
			return false
		}
		if newSetting.RedeliveryDelay == nil {
			if curSetting.RedeliveryDelay != nil {
				return false
			}
		} else if curSetting.RedeliveryDelay != nil {
			if *curSetting.RedeliveryDelay != *newSetting.RedeliveryDelay {
				return false
			}
		} else {
			return false
		}
		if newSetting.RedeliveryDelayMultiplier == nil {
			if curSetting.RedeliveryDelayMultiplier != nil {
				return false
			}
		} else if curSetting.RedeliveryDelayMultiplier != nil {
			if *curSetting.RedeliveryDelayMultiplier != *newSetting.RedeliveryDelayMultiplier {
				return false
			}
		} else {
			return false
		}
		if newSetting.RedeliveryCollisionAvoidanceFactor == nil {
			if curSetting.RedeliveryCollisionAvoidanceFactor != nil {
				return false
			}
		} else if curSetting.RedeliveryCollisionAvoidanceFactor != nil {
			if *curSetting.RedeliveryCollisionAvoidanceFactor != *newSetting.RedeliveryCollisionAvoidanceFactor {
				return false
			}
		} else {
			return false
		}
		if newSetting.MaxRedeliveryDelay == nil {
			if curSetting.MaxRedeliveryDelay != nil {
				return false
			}
		} else if curSetting.MaxRedeliveryDelay != nil {
			if *curSetting.MaxRedeliveryDelay != *newSetting.MaxRedeliveryDelay {
				return false
			}
		} else {
			return false
		}
		if newSetting.MaxDeliveryAttempts == nil {
			if curSetting.MaxDeliveryAttempts != nil {
				return false
			}
		} else if curSetting.MaxDeliveryAttempts != nil {
			if *curSetting.MaxDeliveryAttempts != *newSetting.MaxDeliveryAttempts {
				return false
			}
		} else {
			return false
		}
		if newSetting.MaxSizeBytes == nil {
			if curSetting.MaxSizeBytes != nil {
				return false
			}
		} else if curSetting.MaxSizeBytes != nil {
			if *curSetting.MaxSizeBytes != *newSetting.MaxSizeBytes {
				return false
			}
		} else {
			return false
		}
		if newSetting.MaxSizeBytesRejectThreshold == nil {
			if curSetting.MaxSizeBytesRejectThreshold != nil {
				return false
			}
		} else if curSetting.MaxSizeBytesRejectThreshold != nil {
			if *curSetting.MaxSizeBytesRejectThreshold != *newSetting.MaxSizeBytesRejectThreshold {
				return false
			}
		} else {
			return false
		}
		if newSetting.PageSizeBytes == nil {
			if curSetting.PageSizeBytes != nil {
				return false
			}
		} else if curSetting.PageSizeBytes != nil {
			if *curSetting.PageSizeBytes != *newSetting.PageSizeBytes {
				return false
			}
		} else {
			return false
		}
		if newSetting.PageMaxCacheSize == nil {
			if curSetting.PageMaxCacheSize != nil {
				return false
			}
		} else if curSetting.PageMaxCacheSize != nil {
			if *curSetting.PageMaxCacheSize != *newSetting.PageMaxCacheSize {
				return false
			}
		} else {
			return false
		}
		if newSetting.AddressFullPolicy == nil {
			if curSetting.AddressFullPolicy != nil {
				return false
			}
		} else if curSetting.AddressFullPolicy != nil {
			if *curSetting.AddressFullPolicy != *newSetting.AddressFullPolicy {
				return false
			}
		} else {
			return false
		}
		if newSetting.MessageCounterHistoryDayLimit == nil {
			if curSetting.MessageCounterHistoryDayLimit != nil {
				return false
			}
		} else if curSetting.MessageCounterHistoryDayLimit != nil {
			if *curSetting.MessageCounterHistoryDayLimit != *newSetting.MessageCounterHistoryDayLimit {
				return false
			}
		} else {
			return false
		}
		if newSetting.LastValueQueue == nil {
			if curSetting.LastValueQueue != nil {
				return false
			}
		} else if curSetting.LastValueQueue != nil {
			if *curSetting.LastValueQueue != *newSetting.LastValueQueue {
				return false
			}
		} else {
			return false
		}
		if newSetting.DefaultLastValueQueue == nil {
			if curSetting.DefaultLastValueQueue != nil {
				return false
			}
		} else if curSetting.DefaultLastValueQueue != nil {
			if *curSetting.DefaultLastValueQueue != *newSetting.DefaultLastValueQueue {
				return false
			}
		} else {
			return false
		}
		if newSetting.DefaultLastValueKey == nil {
			if curSetting.DefaultLastValueKey != nil {
				return false
			}
		} else if curSetting.DefaultLastValueKey != nil {
			if *curSetting.DefaultLastValueKey != *newSetting.DefaultLastValueKey {
				return false
			}
		} else {
			return false
		}
		if newSetting.DefaultNonDestructive == nil {
			if curSetting.DefaultNonDestructive != nil {
				return false
			}
		} else if curSetting.DefaultNonDestructive != nil {
			if *curSetting.DefaultNonDestructive != *newSetting.DefaultNonDestructive {
				return false
			}
		} else {
			return false
		}
		if newSetting.DefaultExclusiveQueue == nil {
			if curSetting.DefaultExclusiveQueue != nil {
				return false
			}
		} else if curSetting.DefaultExclusiveQueue != nil {
			if *curSetting.DefaultExclusiveQueue != *newSetting.DefaultExclusiveQueue {
				return false
			}
		} else {
			return false
		}
		if newSetting.DefaultGroupRebalance == nil {
			if curSetting.DefaultGroupRebalance != nil {
				return false
			}
		} else if curSetting.DefaultGroupRebalance != nil {
			if *curSetting.DefaultGroupRebalance != *newSetting.DefaultGroupRebalance {
				return false
			}
		} else {
			return false
		}
		if newSetting.DefaultGroupRebalancePauseDispatch == nil {
			if curSetting.DefaultGroupRebalancePauseDispatch != nil {
				return false
			}
		} else if curSetting.DefaultGroupRebalancePauseDispatch != nil {
			if *curSetting.DefaultGroupRebalancePauseDispatch != *newSetting.DefaultGroupRebalancePauseDispatch {
				return false
			}
		} else {
			return false
		}
		if newSetting.DefaultGroupBuckets == nil {
			if curSetting.DefaultGroupBuckets != nil {
				return false
			}
		} else if curSetting.DefaultGroupBuckets != nil {
			if *curSetting.DefaultGroupBuckets != *newSetting.DefaultGroupBuckets {
				return false
			}
		} else {
			return false
		}
		if newSetting.DefaultGroupFirstKey == nil {
			if curSetting.DefaultGroupFirstKey != nil {
				return false
			}
		} else if curSetting.DefaultGroupFirstKey != nil {
			if *curSetting.DefaultGroupFirstKey != *newSetting.DefaultGroupFirstKey {
				return false
			}
		} else {
			return false
		}
		if newSetting.DefaultConsumersBeforeDispatch == nil {
			if curSetting.DefaultConsumersBeforeDispatch != nil {
				return false
			}
		} else if curSetting.DefaultConsumersBeforeDispatch != nil {
			if *curSetting.DefaultConsumersBeforeDispatch != *newSetting.DefaultConsumersBeforeDispatch {
				return false
			}
		} else {
			return false
		}
		if newSetting.DefaultDelayBeforeDispatch == nil {
			if curSetting.DefaultDelayBeforeDispatch != nil {
				return false
			}
		} else if curSetting.DefaultDelayBeforeDispatch != nil {
			if *curSetting.DefaultDelayBeforeDispatch != *newSetting.DefaultDelayBeforeDispatch {
				return false
			}
		} else {
			return false
		}
		if newSetting.RedistributionDelay == nil {
			if curSetting.RedistributionDelay != nil {
				return false
			}
		} else if curSetting.RedistributionDelay != nil {
			if *curSetting.RedistributionDelay != *newSetting.RedistributionDelay {
				return false
			}
		} else {
			return false
		}
		if newSetting.SendToDlaOnNoRoute == nil {
			if curSetting.SendToDlaOnNoRoute != nil {
				return false
			}
		} else if curSetting.SendToDlaOnNoRoute != nil {
			if *curSetting.SendToDlaOnNoRoute != *newSetting.SendToDlaOnNoRoute {
				return false
			}
		} else {
			return false
		}
		if newSetting.SlowConsumerThreshold == nil {
			if curSetting.SlowConsumerThreshold != nil {
				return false
			}
		} else if curSetting.SlowConsumerThreshold != nil {
			if *curSetting.SlowConsumerThreshold != *newSetting.SlowConsumerThreshold {
				return false
			}
		} else {
			return false
		}
		if newSetting.SlowConsumerPolicy == nil {
			if curSetting.SlowConsumerPolicy != nil {
				return false
			}
		} else if curSetting.SlowConsumerPolicy != nil {
			if *curSetting.SlowConsumerPolicy != *newSetting.SlowConsumerPolicy {
				return false
			}
		} else {
			return false
		}
		if newSetting.SlowConsumerCheckPeriod == nil {
			if curSetting.SlowConsumerCheckPeriod != nil {
				return false
			}
		} else if curSetting.SlowConsumerCheckPeriod != nil {
			if *curSetting.SlowConsumerCheckPeriod != *newSetting.SlowConsumerCheckPeriod {
				return false
			}
		} else {
			return false
		}
		if newSetting.AutoCreateJmsQueues == nil {
			if curSetting.AutoCreateJmsQueues != nil {
				return false
			}
		} else if curSetting.AutoCreateJmsQueues != nil {
			if *curSetting.AutoCreateJmsQueues != *newSetting.AutoCreateJmsQueues {
				return false
			}
		} else {
			return false
		}
		if newSetting.AutoDeleteJmsQueues == nil {
			if curSetting.AutoDeleteJmsQueues != nil {
				return false
			}
		} else if curSetting.AutoDeleteJmsQueues != nil {
			if *curSetting.AutoDeleteJmsQueues != *newSetting.AutoDeleteJmsQueues {
				return false
			}
		} else {
			return false
		}
		if newSetting.AutoCreateJmsTopics == nil {
			if curSetting.AutoCreateJmsTopics != nil {
				return false
			}
		} else if curSetting.AutoCreateJmsTopics != nil {
			if *curSetting.AutoCreateJmsTopics != *newSetting.AutoCreateJmsTopics {
				return false
			}
		} else {
			return false
		}
		if newSetting.AutoDeleteJmsTopics == nil {
			if curSetting.AutoDeleteJmsTopics != nil {
				return false
			}
		} else if curSetting.AutoDeleteJmsTopics != nil {
			if *curSetting.AutoDeleteJmsTopics != *newSetting.AutoDeleteJmsTopics {
				return false
			}
		} else {
			return false
		}
		if newSetting.AutoCreateQueues == nil {
			if curSetting.AutoCreateQueues != nil {
				return false
			}
		} else if curSetting.AutoCreateQueues != nil {
			if *curSetting.AutoCreateQueues != *newSetting.AutoCreateQueues {
				return false
			}
		} else {
			return false
		}
		if newSetting.AutoDeleteQueues == nil {
			if curSetting.AutoDeleteQueues != nil {
				return false
			}
		} else if curSetting.AutoDeleteQueues != nil {
			if *curSetting.AutoDeleteQueues != *newSetting.AutoDeleteQueues {
				return false
			}
		} else {
			return false
		}
		if newSetting.AutoDeleteCreatedQueues == nil {
			if curSetting.AutoDeleteCreatedQueues != nil {
				return false
			}
		} else if curSetting.AutoDeleteCreatedQueues != nil {
			if *curSetting.AutoDeleteCreatedQueues != *newSetting.AutoDeleteCreatedQueues {
				return false
			}
		} else {
			return false
		}
		if newSetting.AutoDeleteQueuesDelay == nil {
			if curSetting.AutoDeleteQueuesDelay != nil {
				return false
			}
		} else if curSetting.AutoDeleteQueuesDelay != nil {
			if *curSetting.AutoDeleteQueuesDelay != *newSetting.AutoDeleteQueuesDelay {
				return false
			}
		} else {
			return false
		}
		if newSetting.AutoDeleteQueuesMessageCount == nil {
			if curSetting.AutoDeleteQueuesMessageCount != nil {
				return false
			}
		} else if curSetting.AutoDeleteQueuesMessageCount != nil {
			if *curSetting.AutoDeleteQueuesMessageCount != *newSetting.AutoDeleteQueuesMessageCount {
				return false
			}
		} else {
			return false
		}
		if newSetting.ConfigDeleteQueues == nil {
			if curSetting.ConfigDeleteQueues != nil {
				return false
			}
		} else if curSetting.ConfigDeleteQueues != nil {
			if *curSetting.ConfigDeleteQueues != *newSetting.ConfigDeleteQueues {
				return false
			}
		} else {
			return false
		}
		if newSetting.AutoCreateAddresses == nil {
			if curSetting.AutoCreateAddresses != nil {
				return false
			}
		} else if curSetting.AutoCreateAddresses != nil {
			if *curSetting.AutoCreateAddresses != *newSetting.AutoCreateAddresses {
				return false
			}
		} else {
			return false
		}
		if newSetting.AutoDeleteAddresses == nil {
			if curSetting.AutoDeleteAddresses != nil {
				return false
			}
		} else if curSetting.AutoDeleteAddresses != nil {
			if *curSetting.AutoDeleteAddresses != *newSetting.AutoDeleteAddresses {
				return false
			}
		} else {
			return false
		}
		if newSetting.AutoDeleteAddressesDelay == nil {
			if curSetting.AutoDeleteAddressesDelay != nil {
				return false
			}
		} else if curSetting.AutoDeleteAddressesDelay != nil {
			if *curSetting.AutoDeleteAddressesDelay != *newSetting.AutoDeleteAddressesDelay {
				return false
			}
		} else {
			return false
		}
		if newSetting.ConfigDeleteAddresses == nil {
			if curSetting.ConfigDeleteAddresses != nil {
				return false
			}
		} else if curSetting.ConfigDeleteAddresses != nil {
			if *curSetting.ConfigDeleteAddresses != *newSetting.ConfigDeleteAddresses {
				return false
			}
		} else {
			return false
		}
		if newSetting.ManagementBrowsePageSize == nil {
			if curSetting.ManagementBrowsePageSize != nil {
				return false
			}
		} else if curSetting.ManagementBrowsePageSize != nil {
			if *curSetting.ManagementBrowsePageSize != *newSetting.ManagementBrowsePageSize {
				return false
			}
		} else {
			return false
		}
		if newSetting.DefaultPurgeOnNoConsumers == nil {
			if curSetting.DefaultPurgeOnNoConsumers != nil {
				return false
			}
		} else if curSetting.DefaultPurgeOnNoConsumers != nil {
			if *curSetting.DefaultPurgeOnNoConsumers != *newSetting.DefaultPurgeOnNoConsumers {
				return false
			}
		} else {
			return false
		}
		if newSetting.DefaultMaxConsumers == nil {
			if curSetting.DefaultMaxConsumers != nil {
				return false
			}
		} else if curSetting.DefaultMaxConsumers != nil {
			if *curSetting.DefaultMaxConsumers != *newSetting.DefaultMaxConsumers {
				return false
			}
		} else {
			return false
		}
		if newSetting.DefaultQueueRoutingType == nil {
			if curSetting.DefaultQueueRoutingType != nil {
				return false
			}
		} else if curSetting.DefaultQueueRoutingType != nil {
			if *curSetting.DefaultQueueRoutingType != *newSetting.DefaultQueueRoutingType {
				return false
			}
		} else {
			return false
		}
		if newSetting.DefaultAddressRoutingType == nil {
			if curSetting.DefaultAddressRoutingType != nil {
				return false
			}
		} else if curSetting.DefaultAddressRoutingType != nil {
			if *curSetting.DefaultAddressRoutingType != *newSetting.DefaultAddressRoutingType {
				return false
			}
		} else {
			return false
		}
		if newSetting.DefaultConsumerWindowSize == nil {
			if curSetting.DefaultConsumerWindowSize != nil {
				return false
			}
		} else if curSetting.DefaultConsumerWindowSize != nil {
			if *curSetting.DefaultConsumerWindowSize != *newSetting.DefaultConsumerWindowSize {
				return false
			}
		} else {
			return false
		}
		if newSetting.DefaultRingSize == nil {
			if curSetting.DefaultRingSize != nil {
				return false
			}
		} else if curSetting.DefaultRingSize != nil {
			if *curSetting.DefaultRingSize != *newSetting.DefaultRingSize {
				return false
			}
		} else {
			return false
		}
		if newSetting.RetroactiveMessageCount == nil {
			if curSetting.RetroactiveMessageCount != nil {
				return false
			}
		} else if curSetting.RetroactiveMessageCount != nil {
			if *curSetting.RetroactiveMessageCount != *newSetting.RetroactiveMessageCount {
				return false
			}
		} else {
			return false
		}
		if newSetting.EnableMetrics == nil {
			if curSetting.EnableMetrics != nil {
				return false
			}
		} else if curSetting.EnableMetrics != nil {
			if *curSetting.EnableMetrics != *newSetting.EnableMetrics {
				return false
			}
		} else {
			return false
		}
		if newSetting.ManagementMessageAttributeSizeLimit == nil {
			if curSetting.ManagementMessageAttributeSizeLimit != nil {
				return false
			}
		} else if curSetting.ManagementMessageAttributeSizeLimit != nil {
			if *curSetting.ManagementMessageAttributeSizeLimit != *newSetting.ManagementMessageAttributeSizeLimit {
				return false
			}
		} else {
			return false
		}
		if newSetting.SlowConsumerThresholdMeasurementUnit == nil {
			if curSetting.SlowConsumerThresholdMeasurementUnit != nil {
				return false
			}
		} else if curSetting.SlowConsumerThresholdMeasurementUnit != nil {
			if *curSetting.SlowConsumerThresholdMeasurementUnit != *newSetting.SlowConsumerThresholdMeasurementUnit {
				return false
			}
		} else {
			return false
		}
		if newSetting.EnableIngressTimestamp == nil {
			if curSetting.EnableIngressTimestamp != nil {
				return false
			}
		} else if curSetting.EnableIngressTimestamp != nil {
			if *curSetting.EnableIngressTimestamp != *newSetting.EnableIngressTimestamp {
				return false
			}
		} else {
			return false
		}
	}
	return true
}

// assuming the lengths of 2 array are equal
func IsEqualV2Alpha4(currentAddressSetting []brokerv2alpha4.AddressSettingType, newAddressSetting []brokerv2alpha4.AddressSettingType) bool {
	var currentAddressSettingV2Alpha3 []brokerv2alpha3.AddressSettingType
	var newAddressSettingV2Alpha3 []brokerv2alpha3.AddressSettingType
	for _, v := range currentAddressSetting {
		currentAddressSettingV2Alpha3 = append(currentAddressSettingV2Alpha3, brokerv2alpha3.AddressSettingType(v))
	}
	for _, v := range newAddressSetting {
		newAddressSettingV2Alpha3 = append(newAddressSettingV2Alpha3, brokerv2alpha3.AddressSettingType(v))
	}
	return IsEqual(currentAddressSettingV2Alpha3, newAddressSettingV2Alpha3)
}

// assuming the lengths of 2 array are equal
func IsEqual(currentAddressSetting []brokerv2alpha3.AddressSettingType, newAddressSetting []brokerv2alpha3.AddressSettingType) bool {
	log := ctrl.Log.WithName("util_config")
	log.V(1).Info("Comparing addressSettings...", "current: ", currentAddressSetting, "new: ", newAddressSetting)

	for _, curSetting := range currentAddressSetting {

		var newSetting *brokerv2alpha3.AddressSettingType = nil
		for _, setting := range newAddressSetting {
			if setting.Match == curSetting.Match {
				newSetting = &setting
				break
			}
		}
		if newSetting == nil {
			return false
		}
		//compare the whole struct
		if newSetting.DeadLetterAddress == nil {
			if curSetting.DeadLetterAddress != nil {
				return false
			}
		} else if curSetting.DeadLetterAddress != nil {
			if *curSetting.DeadLetterAddress != *newSetting.DeadLetterAddress {
				return false
			}
		} else {
			return false
		}
		if newSetting.AutoCreateDeadLetterResources == nil {
			if curSetting.AutoCreateDeadLetterResources != nil {
				return false
			}
		} else if curSetting.AutoCreateDeadLetterResources != nil {
			if *curSetting.AutoCreateDeadLetterResources != *newSetting.AutoCreateDeadLetterResources {
				return false
			}
		} else {
			return false
		}
		if newSetting.DeadLetterQueuePrefix == nil {
			if curSetting.DeadLetterQueuePrefix != nil {
				return false
			}
		} else if curSetting.DeadLetterQueuePrefix != nil {
			if *curSetting.DeadLetterQueuePrefix != *newSetting.DeadLetterQueuePrefix {
				return false
			}
		} else {
			return false
		}
		if newSetting.DeadLetterQueueSuffix == nil {
			if curSetting.DeadLetterQueueSuffix != nil {
				return false
			}
		} else if curSetting.DeadLetterQueueSuffix != nil {
			if *curSetting.DeadLetterQueueSuffix != *newSetting.DeadLetterQueueSuffix {
				return false
			}
		} else {
			return false
		}
		if newSetting.ExpiryAddress == nil {
			if curSetting.ExpiryAddress != nil {
				return false
			}
		} else if curSetting.ExpiryAddress != nil {
			if *curSetting.ExpiryAddress != *newSetting.ExpiryAddress {
				return false
			}
		} else {
			return false
		}
		if newSetting.AutoCreateExpiryResources == nil {
			if curSetting.AutoCreateExpiryResources != nil {
				return false
			}
		} else if curSetting.AutoCreateExpiryResources != nil {
			if *curSetting.AutoCreateExpiryResources != *newSetting.AutoCreateExpiryResources {
				return false
			}
		} else {
			return false
		}
		if newSetting.ExpiryQueuePrefix == nil {
			if curSetting.ExpiryQueuePrefix != nil {
				return false
			}
		} else if curSetting.ExpiryQueuePrefix != nil {
			if *curSetting.ExpiryQueuePrefix != *newSetting.ExpiryQueuePrefix {
				return false
			}
		} else {
			return false
		}
		if newSetting.ExpiryQueueSuffix == nil {
			if curSetting.ExpiryQueueSuffix != nil {
				return false
			}
		} else if curSetting.ExpiryQueueSuffix != nil {
			if *curSetting.ExpiryQueueSuffix != *newSetting.ExpiryQueueSuffix {
				return false
			}
		} else {
			return false
		}
		if newSetting.ExpiryDelay == nil {
			if curSetting.ExpiryDelay != nil {
				return false
			}
		} else if curSetting.ExpiryDelay != nil {
			if *curSetting.ExpiryDelay != *newSetting.ExpiryDelay {
				return false
			}
		} else {
			return false
		}
		if newSetting.MinExpiryDelay == nil {
			if curSetting.MinExpiryDelay != nil {
				return false
			}
		} else if curSetting.MinExpiryDelay != nil {
			if *curSetting.MinExpiryDelay != *newSetting.MinExpiryDelay {
				return false
			}
		} else {
			return false
		}
		if newSetting.MaxExpiryDelay == nil {
			if curSetting.MaxExpiryDelay != nil {
				return false
			}
		} else if curSetting.MaxExpiryDelay != nil {
			if *curSetting.MaxExpiryDelay != *newSetting.MaxExpiryDelay {
				return false
			}
		} else {
			return false
		}
		if newSetting.RedeliveryDelay == nil {
			if curSetting.RedeliveryDelay != nil {
				return false
			}
		} else if curSetting.RedeliveryDelay != nil {
			if *curSetting.RedeliveryDelay != *newSetting.RedeliveryDelay {
				return false
			}
		} else {
			return false
		}
		if newSetting.RedeliveryDelayMultiplier == nil {
			if curSetting.RedeliveryDelayMultiplier != nil {
				return false
			}
		} else if curSetting.RedeliveryDelayMultiplier != nil {
			if *curSetting.RedeliveryDelayMultiplier != *newSetting.RedeliveryDelayMultiplier {
				return false
			}
		} else {
			return false
		}
		if newSetting.RedeliveryCollisionAvoidanceFactor == nil {
			if curSetting.RedeliveryCollisionAvoidanceFactor != nil {
				return false
			}
		} else if curSetting.RedeliveryCollisionAvoidanceFactor != nil {
			if *curSetting.RedeliveryCollisionAvoidanceFactor != *newSetting.RedeliveryCollisionAvoidanceFactor {
				return false
			}
		} else {
			return false
		}
		if newSetting.MaxRedeliveryDelay == nil {
			if curSetting.MaxRedeliveryDelay != nil {
				return false
			}
		} else if curSetting.MaxRedeliveryDelay != nil {
			if *curSetting.MaxRedeliveryDelay != *newSetting.MaxRedeliveryDelay {
				return false
			}
		} else {
			return false
		}
		if newSetting.MaxDeliveryAttempts == nil {
			if curSetting.MaxDeliveryAttempts != nil {
				return false
			}
		} else if curSetting.MaxDeliveryAttempts != nil {
			if *curSetting.MaxDeliveryAttempts != *newSetting.MaxDeliveryAttempts {
				return false
			}
		} else {
			return false
		}
		if newSetting.MaxSizeBytes == nil {
			if curSetting.MaxSizeBytes != nil {
				return false
			}
		} else if curSetting.MaxSizeBytes != nil {
			if *curSetting.MaxSizeBytes != *newSetting.MaxSizeBytes {
				return false
			}
		} else {
			return false
		}
		if newSetting.MaxSizeBytesRejectThreshold == nil {
			if curSetting.MaxSizeBytesRejectThreshold != nil {
				return false
			}
		} else if curSetting.MaxSizeBytesRejectThreshold != nil {
			if *curSetting.MaxSizeBytesRejectThreshold != *newSetting.MaxSizeBytesRejectThreshold {
				return false
			}
		} else {
			return false
		}
		if newSetting.PageSizeBytes == nil {
			if curSetting.PageSizeBytes != nil {
				return false
			}
		} else if curSetting.PageSizeBytes != nil {
			if *curSetting.PageSizeBytes != *newSetting.PageSizeBytes {
				return false
			}
		} else {
			return false
		}
		if newSetting.PageMaxCacheSize == nil {
			if curSetting.PageMaxCacheSize != nil {
				return false
			}
		} else if curSetting.PageMaxCacheSize != nil {
			if *curSetting.PageMaxCacheSize != *newSetting.PageMaxCacheSize {
				return false
			}
		} else {
			return false
		}
		if newSetting.AddressFullPolicy == nil {
			if curSetting.AddressFullPolicy != nil {
				return false
			}
		} else if curSetting.AddressFullPolicy != nil {
			if *curSetting.AddressFullPolicy != *newSetting.AddressFullPolicy {
				return false
			}
		} else {
			return false
		}
		if newSetting.MessageCounterHistoryDayLimit == nil {
			if curSetting.MessageCounterHistoryDayLimit != nil {
				return false
			}
		} else if curSetting.MessageCounterHistoryDayLimit != nil {
			if *curSetting.MessageCounterHistoryDayLimit != *newSetting.MessageCounterHistoryDayLimit {
				return false
			}
		} else {
			return false
		}
		if newSetting.LastValueQueue == nil {
			if curSetting.LastValueQueue != nil {
				return false
			}
		} else if curSetting.LastValueQueue != nil {
			if *curSetting.LastValueQueue != *newSetting.LastValueQueue {
				return false
			}
		} else {
			return false
		}
		if newSetting.DefaultLastValueQueue == nil {
			if curSetting.DefaultLastValueQueue != nil {
				return false
			}
		} else if curSetting.DefaultLastValueQueue != nil {
			if *curSetting.DefaultLastValueQueue != *newSetting.DefaultLastValueQueue {
				return false
			}
		} else {
			return false
		}
		if newSetting.DefaultLastValueKey == nil {
			if curSetting.DefaultLastValueKey != nil {
				return false
			}
		} else if curSetting.DefaultLastValueKey != nil {
			if *curSetting.DefaultLastValueKey != *newSetting.DefaultLastValueKey {
				return false
			}
		} else {
			return false
		}
		if newSetting.DefaultNonDestructive == nil {
			if curSetting.DefaultNonDestructive != nil {
				return false
			}
		} else if curSetting.DefaultNonDestructive != nil {
			if *curSetting.DefaultNonDestructive != *newSetting.DefaultNonDestructive {
				return false
			}
		} else {
			return false
		}
		if newSetting.DefaultExclusiveQueue == nil {
			if curSetting.DefaultExclusiveQueue != nil {
				return false
			}
		} else if curSetting.DefaultExclusiveQueue != nil {
			if *curSetting.DefaultExclusiveQueue != *newSetting.DefaultExclusiveQueue {
				return false
			}
		} else {
			return false
		}
		if newSetting.DefaultGroupRebalance == nil {
			if curSetting.DefaultGroupRebalance != nil {
				return false
			}
		} else if curSetting.DefaultGroupRebalance != nil {
			if *curSetting.DefaultGroupRebalance != *newSetting.DefaultGroupRebalance {
				return false
			}
		} else {
			return false
		}
		if newSetting.DefaultGroupRebalancePauseDispatch == nil {
			if curSetting.DefaultGroupRebalancePauseDispatch != nil {
				return false
			}
		} else if curSetting.DefaultGroupRebalancePauseDispatch != nil {
			if *curSetting.DefaultGroupRebalancePauseDispatch != *newSetting.DefaultGroupRebalancePauseDispatch {
				return false
			}
		} else {
			return false
		}
		if newSetting.DefaultGroupBuckets == nil {
			if curSetting.DefaultGroupBuckets != nil {
				return false
			}
		} else if curSetting.DefaultGroupBuckets != nil {
			if *curSetting.DefaultGroupBuckets != *newSetting.DefaultGroupBuckets {
				return false
			}
		} else {
			return false
		}
		if newSetting.DefaultGroupFirstKey == nil {
			if curSetting.DefaultGroupFirstKey != nil {
				return false
			}
		} else if curSetting.DefaultGroupFirstKey != nil {
			if *curSetting.DefaultGroupFirstKey != *newSetting.DefaultGroupFirstKey {
				return false
			}
		} else {
			return false
		}
		if newSetting.DefaultConsumersBeforeDispatch == nil {
			if curSetting.DefaultConsumersBeforeDispatch != nil {
				return false
			}
		} else if curSetting.DefaultConsumersBeforeDispatch != nil {
			if *curSetting.DefaultConsumersBeforeDispatch != *newSetting.DefaultConsumersBeforeDispatch {
				return false
			}
		} else {
			return false
		}
		if newSetting.DefaultDelayBeforeDispatch == nil {
			if curSetting.DefaultDelayBeforeDispatch != nil {
				return false
			}
		} else if curSetting.DefaultDelayBeforeDispatch != nil {
			if *curSetting.DefaultDelayBeforeDispatch != *newSetting.DefaultDelayBeforeDispatch {
				return false
			}
		} else {
			return false
		}
		if newSetting.RedistributionDelay == nil {
			if curSetting.RedistributionDelay != nil {
				return false
			}
		} else if curSetting.RedistributionDelay != nil {
			if *curSetting.RedistributionDelay != *newSetting.RedistributionDelay {
				return false
			}
		} else {
			return false
		}
		if newSetting.SendToDlaOnNoRoute == nil {
			if curSetting.SendToDlaOnNoRoute != nil {
				return false
			}
		} else if curSetting.SendToDlaOnNoRoute != nil {
			if *curSetting.SendToDlaOnNoRoute != *newSetting.SendToDlaOnNoRoute {
				return false
			}
		} else {
			return false
		}
		if newSetting.SlowConsumerThreshold == nil {
			if curSetting.SlowConsumerThreshold != nil {
				return false
			}
		} else if curSetting.SlowConsumerThreshold != nil {
			if *curSetting.SlowConsumerThreshold != *newSetting.SlowConsumerThreshold {
				return false
			}
		} else {
			return false
		}
		if newSetting.SlowConsumerPolicy == nil {
			if curSetting.SlowConsumerPolicy != nil {
				return false
			}
		} else if curSetting.SlowConsumerPolicy != nil {
			if *curSetting.SlowConsumerPolicy != *newSetting.SlowConsumerPolicy {
				return false
			}
		} else {
			return false
		}
		if newSetting.SlowConsumerCheckPeriod == nil {
			if curSetting.SlowConsumerCheckPeriod != nil {
				return false
			}
		} else if curSetting.SlowConsumerCheckPeriod != nil {
			if *curSetting.SlowConsumerCheckPeriod != *newSetting.SlowConsumerCheckPeriod {
				return false
			}
		} else {
			return false
		}
		if newSetting.AutoCreateJmsQueues == nil {
			if curSetting.AutoCreateJmsQueues != nil {
				return false
			}
		} else if curSetting.AutoCreateJmsQueues != nil {
			if *curSetting.AutoCreateJmsQueues != *newSetting.AutoCreateJmsQueues {
				return false
			}
		} else {
			return false
		}
		if newSetting.AutoDeleteJmsQueues == nil {
			if curSetting.AutoDeleteJmsQueues != nil {
				return false
			}
		} else if curSetting.AutoDeleteJmsQueues != nil {
			if *curSetting.AutoDeleteJmsQueues != *newSetting.AutoDeleteJmsQueues {
				return false
			}
		} else {
			return false
		}
		if newSetting.AutoCreateJmsTopics == nil {
			if curSetting.AutoCreateJmsTopics != nil {
				return false
			}
		} else if curSetting.AutoCreateJmsTopics != nil {
			if *curSetting.AutoCreateJmsTopics != *newSetting.AutoCreateJmsTopics {
				return false
			}
		} else {
			return false
		}
		if newSetting.AutoDeleteJmsTopics == nil {
			if curSetting.AutoDeleteJmsTopics != nil {
				return false
			}
		} else if curSetting.AutoDeleteJmsTopics != nil {
			if *curSetting.AutoDeleteJmsTopics != *newSetting.AutoDeleteJmsTopics {
				return false
			}
		} else {
			return false
		}
		if newSetting.AutoCreateQueues == nil {
			if curSetting.AutoCreateQueues != nil {
				return false
			}
		} else if curSetting.AutoCreateQueues != nil {
			if *curSetting.AutoCreateQueues != *newSetting.AutoCreateQueues {
				return false
			}
		} else {
			return false
		}
		if newSetting.AutoDeleteQueues == nil {
			if curSetting.AutoDeleteQueues != nil {
				return false
			}
		} else if curSetting.AutoDeleteQueues != nil {
			if *curSetting.AutoDeleteQueues != *newSetting.AutoDeleteQueues {
				return false
			}
		} else {
			return false
		}
		if newSetting.AutoDeleteCreatedQueues == nil {
			if curSetting.AutoDeleteCreatedQueues != nil {
				return false
			}
		} else if curSetting.AutoDeleteCreatedQueues != nil {
			if *curSetting.AutoDeleteCreatedQueues != *newSetting.AutoDeleteCreatedQueues {
				return false
			}
		} else {
			return false
		}
		if newSetting.AutoDeleteQueuesDelay == nil {
			if curSetting.AutoDeleteQueuesDelay != nil {
				return false
			}
		} else if curSetting.AutoDeleteQueuesDelay != nil {
			if *curSetting.AutoDeleteQueuesDelay != *newSetting.AutoDeleteQueuesDelay {
				return false
			}
		} else {
			return false
		}
		if newSetting.AutoDeleteQueuesMessageCount == nil {
			if curSetting.AutoDeleteQueuesMessageCount != nil {
				return false
			}
		} else if curSetting.AutoDeleteQueuesMessageCount != nil {
			if *curSetting.AutoDeleteQueuesMessageCount != *newSetting.AutoDeleteQueuesMessageCount {
				return false
			}
		} else {
			return false
		}
		if newSetting.ConfigDeleteQueues == nil {
			if curSetting.ConfigDeleteQueues != nil {
				return false
			}
		} else if curSetting.ConfigDeleteQueues != nil {
			if *curSetting.ConfigDeleteQueues != *newSetting.ConfigDeleteQueues {
				return false
			}
		} else {
			return false
		}
		if newSetting.AutoCreateAddresses == nil {
			if curSetting.AutoCreateAddresses != nil {
				return false
			}
		} else if curSetting.AutoCreateAddresses != nil {
			if *curSetting.AutoCreateAddresses != *newSetting.AutoCreateAddresses {
				return false
			}
		} else {
			return false
		}
		if newSetting.AutoDeleteAddresses == nil {
			if curSetting.AutoDeleteAddresses != nil {
				return false
			}
		} else if curSetting.AutoDeleteAddresses != nil {
			if *curSetting.AutoDeleteAddresses != *newSetting.AutoDeleteAddresses {
				return false
			}
		} else {
			return false
		}
		if newSetting.AutoDeleteAddressesDelay == nil {
			if curSetting.AutoDeleteAddressesDelay != nil {
				return false
			}
		} else if curSetting.AutoDeleteAddressesDelay != nil {
			if *curSetting.AutoDeleteAddressesDelay != *newSetting.AutoDeleteAddressesDelay {
				return false
			}
		} else {
			return false
		}
		if newSetting.ConfigDeleteAddresses == nil {
			if curSetting.ConfigDeleteAddresses != nil {
				return false
			}
		} else if curSetting.ConfigDeleteAddresses != nil {
			if *curSetting.ConfigDeleteAddresses != *newSetting.ConfigDeleteAddresses {
				return false
			}
		} else {
			return false
		}
		if newSetting.ManagementBrowsePageSize == nil {
			if curSetting.ManagementBrowsePageSize != nil {
				return false
			}
		} else if curSetting.ManagementBrowsePageSize != nil {
			if *curSetting.ManagementBrowsePageSize != *newSetting.ManagementBrowsePageSize {
				return false
			}
		} else {
			return false
		}
		if newSetting.DefaultPurgeOnNoConsumers == nil {
			if curSetting.DefaultPurgeOnNoConsumers != nil {
				return false
			}
		} else if curSetting.DefaultPurgeOnNoConsumers != nil {
			if *curSetting.DefaultPurgeOnNoConsumers != *newSetting.DefaultPurgeOnNoConsumers {
				return false
			}
		} else {
			return false
		}
		if newSetting.DefaultMaxConsumers == nil {
			if curSetting.DefaultMaxConsumers != nil {
				return false
			}
		} else if curSetting.DefaultMaxConsumers != nil {
			if *curSetting.DefaultMaxConsumers != *newSetting.DefaultMaxConsumers {
				return false
			}
		} else {
			return false
		}
		if newSetting.DefaultQueueRoutingType == nil {
			if curSetting.DefaultQueueRoutingType != nil {
				return false
			}
		} else if curSetting.DefaultQueueRoutingType != nil {
			if *curSetting.DefaultQueueRoutingType != *newSetting.DefaultQueueRoutingType {
				return false
			}
		} else {
			return false
		}
		if newSetting.DefaultAddressRoutingType == nil {
			if curSetting.DefaultAddressRoutingType != nil {
				return false
			}
		} else if curSetting.DefaultAddressRoutingType != nil {
			if *curSetting.DefaultAddressRoutingType != *newSetting.DefaultAddressRoutingType {
				return false
			}
		} else {
			return false
		}
		if newSetting.DefaultConsumerWindowSize == nil {
			if curSetting.DefaultConsumerWindowSize != nil {
				return false
			}
		} else if curSetting.DefaultConsumerWindowSize != nil {
			if *curSetting.DefaultConsumerWindowSize != *newSetting.DefaultConsumerWindowSize {
				return false
			}
		} else {
			return false
		}
		if newSetting.DefaultRingSize == nil {
			if curSetting.DefaultRingSize != nil {
				return false
			}
		} else if curSetting.DefaultRingSize != nil {
			if *curSetting.DefaultRingSize != *newSetting.DefaultRingSize {
				return false
			}
		} else {
			return false
		}
		if newSetting.RetroactiveMessageCount == nil {
			if curSetting.RetroactiveMessageCount != nil {
				return false
			}
		} else if curSetting.RetroactiveMessageCount != nil {
			if *curSetting.RetroactiveMessageCount != *newSetting.RetroactiveMessageCount {
				return false
			}
		} else {
			return false
		}
		if newSetting.EnableMetrics == nil {
			if curSetting.EnableMetrics != nil {
				return false
			}
		} else if curSetting.EnableMetrics != nil {
			if *curSetting.EnableMetrics != *newSetting.EnableMetrics {
				return false
			}
		} else {
			return false
		}
	}
	return true
}
