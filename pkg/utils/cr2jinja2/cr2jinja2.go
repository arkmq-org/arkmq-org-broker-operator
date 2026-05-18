package cr2jinja2

import (
	"hash/fnv"

	"github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta1"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta2"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v2alpha3"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v2alpha4"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v2alpha5"

	//k8syaml "k8s.io/apimachinery/pkg/util/yaml"
	"fmt"
	"io/ioutil"
	"strconv"
	"strings"
)

// the following values in string type will be parsed as bool values
// exception empty string which will be translated to None
// we need to let yacfg know this is what it is, don't try interpret them.
// https://yaml.org/type/bool.html
var specialsMap map[string]bool = map[string]bool{
	"":      true,
	"y":     true,
	"Y":     true,
	"yes":   true,
	"Yes":   true,
	"YES":   true,
	"n":     true,
	"N":     true,
	"no":    true,
	"No":    true,
	"NO":    true,
	"true":  true,
	"True":  true,
	"TRUE":  true,
	"false": true,
	"False": true,
	"FALSE": true,
	"on":    true,
	"On":    true,
	"ON":    true,
	"off":   true,
	"Off":   true,
	"OFF":   true,
}

func isSpecialValue(value string) bool {
	if specialsMap[value] {
		return true
	}
	if strings.Contains(value, "%") {
		return true
	}
	if strings.Contains(value, "$") {
		return true
	}
	if strings.Contains(value, "*") {
		return true
	}
	if strings.Contains(value, "#") {
		return true
	}
	return false
}

func GetUniqueShellSafeSubstution(specialVal string) string {
	hasher := fnv.New64a()
	hasher.Write([]byte(specialVal))
	// append a character to make sure the key not to be interpreted as a number
	return strconv.FormatUint(hasher.Sum64(), 10) + "s"
}

// Used to check properties that has a special values
// which may be misinterpreted by yacfg.
// so we use a determined as uniquekey and in the mean time as prop
func checkStringSpecial(prop *string, specials map[string]string) *string {
	if nil == prop {
		return nil
	} else if isSpecialValue(*prop) {
		uniqueKey := GetUniqueShellSafeSubstution(*prop)
		specials[uniqueKey] = *prop
		return &uniqueKey
	}
	return prop
}

func checkBool(prop *bool) *string {
	if nil == prop {
		return nil
	}
	tmp := strconv.FormatBool(*prop)
	return &tmp
}

func checkInt32(prop *int32) *string {
	if nil == prop {
		return nil
	}
	tmp := fmt.Sprint(*prop)
	return &tmp
}

func checkInt64(prop *int64) *string {
	if nil == prop {
		return nil
	}
	tmp := strconv.FormatInt(*prop, 10)
	return &tmp
}

func checkFloat32(prop *float32) *string {
	if nil == prop {
		return nil
	}
	tmp := fmt.Sprint(*prop)
	return &tmp
}

/* return a yaml string and a map of special values that need to pass to yacfg */
func MakeBrokerCfgOverrides(customResource interface{}, envVar *string, output *string) (string, map[string]string) {

	var sb strings.Builder
	var specials map[string]string = make(map[string]string)

	var processed bool = false
	v2alpha3Res, ok := customResource.(*v2alpha3.ActiveMQArtemis)
	if ok {
		MakeBrokerCfgOverridesForV2alpha3(v2alpha3Res, envVar, output, &sb, specials)
		processed = true
	} else {
		v2alpha4Res, ok := customResource.(*v2alpha4.ActiveMQArtemis)
		if ok {
			MakeBrokerCfgOverridesForV2alpha4(v2alpha4Res, envVar, output, &sb, specials)
			processed = true
		} else {
			v2alpha5Res, ok := customResource.(*v2alpha5.ActiveMQArtemis)
			if ok {
				MakeBrokerCfgOverridesForV2alpha5(v2alpha5Res, envVar, output, &sb, specials)
				processed = true
			} else {
				v1beta1Res, ok := customResource.(*v1beta1.ActiveMQArtemis)
				if ok {
					MakeBrokerCfgOverridesForV1beta1(v1beta1Res, envVar, output, &sb, specials)
					processed = true
				} else {
					v1beta2Res, ok := customResource.(*v1beta2.Broker)
					if ok {
						MakeBrokerCfgOverridesForV1beta2(v1beta2Res, envVar, output, &sb, specials)
						processed = true
					}
				}
			}
		}
	}

	if !processed {
		panic("Unregnized resource type " + fmt.Sprintf("%T", customResource))
	}

	if envVar != nil && *envVar != "" {
		fmt.Println("envvar: " + (*envVar))
	}

	result := sb.String()

	if output != nil && *output != "" {
		fmt.Println("output " + *output)
		err := ioutil.WriteFile(*output, []byte(result), 0644)
		if err != nil {
			panic(err)
		}
	}
	return result, specials
}

func MakeBrokerCfgOverridesForV1beta2(customResource *v1beta2.Broker, envVar *string, output *string, sb *strings.Builder, specials map[string]string) {
	var addressSettings *[]v1beta2.AddressSettingType = &customResource.Spec.AddressSettings.AddressSetting

	processAddressSettingsV1beta2(sb, addressSettings, specials)
}

func MakeBrokerCfgOverridesForV1beta1(customResource *v1beta1.ActiveMQArtemis, envVar *string, output *string, sb *strings.Builder, specials map[string]string) {
	var addressSettings *[]v1beta1.AddressSettingType = &customResource.Spec.AddressSettings.AddressSetting

	processAddressSettingsV1beta1(sb, addressSettings, specials)
}

func MakeBrokerCfgOverridesForV2alpha5(customResource *v2alpha5.ActiveMQArtemis, envVar *string, output *string, sb *strings.Builder, specials map[string]string) {
	var addressSettings *[]v2alpha5.AddressSettingType = &customResource.Spec.AddressSettings.AddressSetting

	processAddressSettingsV2alpha5(sb, addressSettings, specials)
}

func MakeBrokerCfgOverridesForV2alpha4(customResource *v2alpha4.ActiveMQArtemis, envVar *string, output *string, sb *strings.Builder, specials map[string]string) {
	var addressSettings *[]v2alpha4.AddressSettingType = &customResource.Spec.AddressSettings.AddressSetting

	//because the address settings are same between v2alpha3 and v2alpha4, reuse the code
	var addressSettingsV2alpha3 []v2alpha3.AddressSettingType
	for _, a := range *addressSettings {
		addressSettingsV2alpha3 = append(addressSettingsV2alpha3, v2alpha3.AddressSettingType(a))
	}

	processAddressSettings(sb, &addressSettingsV2alpha3, specials)
}

func MakeBrokerCfgOverridesForV2alpha3(customResource *v2alpha3.ActiveMQArtemis, envVar *string, output *string, sb *strings.Builder, specials map[string]string) {

	var addressSettings *[]v2alpha3.AddressSettingType = &customResource.Spec.AddressSettings.AddressSetting

	processAddressSettings(sb, addressSettings, specials)

}

func topointer(value string) *string {
	result := value
	return &result
}

func processAddressSettings(sb *strings.Builder, addressSettings *[]v2alpha3.AddressSettingType, specials map[string]string) {

	if addressSettings == nil || len(*addressSettings) == 0 {
		return
	}
	sb.WriteString("user_address_settings:\n")
	for _, s := range *addressSettings {
		if matchValue := checkStringSpecial(&s.Match, specials); matchValue != nil {
			sb.WriteString("- match: " + *matchValue + "\n")
		}
		if value := checkStringSpecial(s.DeadLetterAddress, specials); value != nil {
			sb.WriteString("  dead_letter_address: " + *value + "\n")
		}
		if value := checkBool(s.AutoCreateDeadLetterResources); value != nil {
			sb.WriteString("  auto_create_dead_letter_resources: " + *value + "\n")
		}
		if value := checkStringSpecial(s.DeadLetterQueuePrefix, specials); value != nil {
			sb.WriteString("  dead_letter_queue_prefix: " + *value + "\n")
		}
		if value := checkStringSpecial(s.DeadLetterQueueSuffix, specials); value != nil {
			sb.WriteString("  dead_letter_queue_suffix: " + *value + "\n")
		}
		if value := checkStringSpecial(s.ExpiryAddress, specials); value != nil {
			sb.WriteString("  expiry_address: " + *value + "\n")
		}
		if value := checkBool(s.AutoCreateExpiryResources); value != nil {
			sb.WriteString("  auto_create_expiry_resources: " + *value + "\n")
		}
		if value := checkStringSpecial(s.ExpiryQueuePrefix, specials); value != nil {
			sb.WriteString("  expiry_queue_prefix: " + *value + "\n")
		}
		if value := checkStringSpecial(s.ExpiryQueueSuffix, specials); value != nil {
			sb.WriteString("  expiry_queue_suffix: " + *value + "\n")
		}
		if value := checkInt32(s.ExpiryDelay); value != nil {
			sb.WriteString("  expiry_delay: " + *value + "\n")
		}
		if value := checkInt32(s.MinExpiryDelay); value != nil {
			sb.WriteString("  min_expiry_delay: " + *value + "\n")
		}
		if value := checkInt32(s.MaxExpiryDelay); value != nil {
			sb.WriteString("  max_expiry_delay: " + *value + "\n")
		}
		if value := checkInt32(s.RedeliveryDelay); value != nil {
			sb.WriteString("  redelivery_delay: " + *value + "\n")
		}
		if value := checkInt32(s.RedeliveryDelayMultiplier); value != nil {
			sb.WriteString("  redelivery_delay_multiplier: " + *value + "\n")
		}
		if value := checkFloat32(s.RedeliveryCollisionAvoidanceFactor); value != nil {
			sb.WriteString("  redelivery_collision_avoidance_factor: " + *value + "\n")
		}
		if value := checkInt32(s.MaxRedeliveryDelay); value != nil {
			sb.WriteString("  max_redelivery_delay: " + *value + "\n")
		}
		if value := checkInt32(s.MaxDeliveryAttempts); value != nil {
			sb.WriteString("  max_delivery_attempts: " + *value + "\n")
		}
		if value := checkStringSpecial(s.MaxSizeBytes, specials); value != nil {
			sb.WriteString("  max_size_bytes: " + *value + "\n")
		}
		if value := checkInt32(s.MaxSizeBytesRejectThreshold); value != nil {
			sb.WriteString("  max_size_bytes_reject_threshold: " + *value + "\n")
		}
		if value := checkStringSpecial(s.PageSizeBytes, specials); value != nil {
			sb.WriteString("  page_size_bytes: " + *value + "\n")
		}
		if value := checkInt32(s.PageMaxCacheSize); value != nil {
			sb.WriteString("  page_max_cache_size: " + *value + "\n")
		}
		if value := checkStringSpecial(s.AddressFullPolicy, specials); value != nil {
			sb.WriteString("  address_full_policy: " + *value + "\n")
		}
		if value := checkInt32(s.MessageCounterHistoryDayLimit); value != nil {
			sb.WriteString("  message_counter_history_day_limit: " + *value + "\n")
		}
		if value := checkBool(s.LastValueQueue); value != nil {
			sb.WriteString("  last_value_queue: " + *value + "\n")
		}
		if value := checkBool(s.DefaultLastValueQueue); value != nil {
			sb.WriteString("  default_last_value_queue: " + *value + "\n")
		}
		if value := checkStringSpecial(s.DefaultLastValueKey, specials); value != nil {
			sb.WriteString("  default_last_value_key: " + *value + "\n")
		}
		if value := checkBool(s.DefaultNonDestructive); value != nil {
			sb.WriteString("  default_non_destructive: " + *value + "\n")
		}
		if value := checkBool(s.DefaultExclusiveQueue); value != nil {
			sb.WriteString("  default_exclusive_queue: " + *value + "\n")
		}
		if value := checkBool(s.DefaultGroupRebalance); value != nil {
			sb.WriteString("  default_group_rebalance: " + *value + "\n")
		}
		if value := checkBool(s.DefaultGroupRebalancePauseDispatch); value != nil {
			sb.WriteString("  default_group_rebalance_pause_dispatch: " + *value + "\n")
		}
		if value := checkInt32(s.DefaultGroupBuckets); value != nil {
			sb.WriteString("  default_group_buckets: " + *value + "\n")
		}
		if value := checkStringSpecial(s.DefaultGroupFirstKey, specials); value != nil {
			sb.WriteString("  default_group_first_key: " + *value + "\n")
		}
		if value := checkInt32(s.DefaultConsumersBeforeDispatch); value != nil {
			sb.WriteString("  default_consumers_before_dispatch: " + *value + "\n")
		}
		if value := checkInt32(s.DefaultDelayBeforeDispatch); value != nil {
			sb.WriteString("  default_delay_before_dispatch: " + *value + "\n")
		}
		if value := checkInt32(s.RedistributionDelay); value != nil {
			sb.WriteString("  redistribution_delay: " + *value + "\n")
		}
		if value := checkBool(s.SendToDlaOnNoRoute); value != nil {
			sb.WriteString("  send_to_dla_on_no_route: " + *value + "\n")
		}
		if value := checkInt32(s.SlowConsumerThreshold); value != nil {
			sb.WriteString("  slow_consumer_threshold: " + *value + "\n")
		}
		if value := checkStringSpecial(s.SlowConsumerPolicy, specials); value != nil {
			sb.WriteString("  slow_consumer_policy: " + *value + "\n")
		}
		if value := checkInt32(s.SlowConsumerCheckPeriod); value != nil {
			sb.WriteString("  slow_consumer_check_period: " + *value + "\n")
		}
		if value := checkBool(s.AutoCreateJmsQueues); value != nil {
			sb.WriteString("  auto_create_jms_queues: " + *value + "\n")
		}
		if value := checkBool(s.AutoDeleteJmsQueues); value != nil {
			sb.WriteString("  auto_delete_jms_queues: " + *value + "\n")
		}
		if value := checkBool(s.AutoCreateJmsTopics); value != nil {
			sb.WriteString("  auto_create_jms_topics: " + *value + "\n")
		}
		if value := checkBool(s.AutoDeleteJmsTopics); value != nil {
			sb.WriteString("  auto_delete_jms_topics: " + *value + "\n")
		}
		if value := checkBool(s.AutoCreateQueues); value != nil {
			sb.WriteString("  auto_create_queues: " + *value + "\n")
		}
		if value := checkBool(s.AutoDeleteQueues); value != nil {
			sb.WriteString("  auto_delete_queues: " + *value + "\n")
		}
		if value := checkBool(s.AutoDeleteCreatedQueues); value != nil {
			sb.WriteString("  auto_delete_created_queues: " + *value + "\n")
		}
		if value := checkInt32(s.AutoDeleteQueuesDelay); value != nil {
			sb.WriteString("  auto_delete_queues_delay: " + *value + "\n")
		}
		if value := checkInt32(s.AutoDeleteQueuesMessageCount); value != nil {
			sb.WriteString("  auto_delete_queues_message_count: " + *value + "\n")
		}
		if value := checkStringSpecial(s.ConfigDeleteQueues, specials); value != nil {
			sb.WriteString("  config_delete_queues: " + *value + "\n")
		}
		if value := checkBool(s.AutoCreateAddresses); value != nil {
			sb.WriteString("  auto_create_addresses: " + *value + "\n")
		}
		if value := checkBool(s.AutoDeleteAddresses); value != nil {
			sb.WriteString("  auto_delete_addresses: " + *value + "\n")
		}
		if value := checkInt32(s.AutoDeleteAddressesDelay); value != nil {
			sb.WriteString("  auto_delete_addresses_delay: " + *value + "\n")
		}
		if value := checkStringSpecial(s.ConfigDeleteAddresses, specials); value != nil {
			sb.WriteString("  config_delete_addresses: " + *value + "\n")
		}
		if value := checkInt32(s.ManagementBrowsePageSize); value != nil {
			sb.WriteString("  management_browse_page_size: " + *value + "\n")
		}
		if value := checkBool(s.DefaultPurgeOnNoConsumers); value != nil {
			sb.WriteString("  default_purge_on_no_consumers: " + *value + "\n")
		}
		if value := checkInt32(s.DefaultMaxConsumers); value != nil {
			sb.WriteString("  default_max_consumers: " + *value + "\n")
		}
		if value := checkStringSpecial(s.DefaultQueueRoutingType, specials); value != nil {
			sb.WriteString("  default_queue_routing_type: " + *value + "\n")
		}
		if value := checkStringSpecial(s.DefaultAddressRoutingType, specials); value != nil {
			sb.WriteString("  default_address_routing_type: " + *value + "\n")
		}
		if value := checkInt32(s.DefaultConsumerWindowSize); value != nil {
			sb.WriteString("  default_consumer_window_size: " + *value + "\n")
		}
		if value := checkInt32(s.DefaultRingSize); value != nil {
			sb.WriteString("  default_ring_size: " + *value + "\n")
		}
		if value := checkInt32(s.RetroactiveMessageCount); value != nil {
			sb.WriteString("  retroactive_message_count: " + *value + "\n")
		}
		if value := checkBool(s.EnableMetrics); value != nil {
			sb.WriteString("  enable_metrics: " + *value + "\n")
		}
	}

}

func processAddressSettingsV2alpha5(sb *strings.Builder, addressSettings *[]v2alpha5.AddressSettingType, specials map[string]string) {

	if addressSettings == nil || len(*addressSettings) == 0 {
		return
	}
	sb.WriteString("user_address_settings:\n")
	for _, s := range *addressSettings {
		if matchValue := checkStringSpecial(&s.Match, specials); matchValue != nil {
			sb.WriteString("- match: " + *matchValue + "\n")
		}
		if value := checkStringSpecial(s.DeadLetterAddress, specials); value != nil {
			sb.WriteString("  dead_letter_address: " + *value + "\n")
		}
		if value := checkBool(s.AutoCreateDeadLetterResources); value != nil {
			sb.WriteString("  auto_create_dead_letter_resources: " + *value + "\n")
		}
		if value := checkStringSpecial(s.DeadLetterQueuePrefix, specials); value != nil {
			sb.WriteString("  dead_letter_queue_prefix: " + *value + "\n")
		}
		if value := checkStringSpecial(s.DeadLetterQueueSuffix, specials); value != nil {
			sb.WriteString("  dead_letter_queue_suffix: " + *value + "\n")
		}
		if value := checkStringSpecial(s.ExpiryAddress, specials); value != nil {
			sb.WriteString("  expiry_address: " + *value + "\n")
		}
		if value := checkBool(s.AutoCreateExpiryResources); value != nil {
			sb.WriteString("  auto_create_expiry_resources: " + *value + "\n")
		}
		if value := checkStringSpecial(s.ExpiryQueuePrefix, specials); value != nil {
			sb.WriteString("  expiry_queue_prefix: " + *value + "\n")
		}
		if value := checkStringSpecial(s.ExpiryQueueSuffix, specials); value != nil {
			sb.WriteString("  expiry_queue_suffix: " + *value + "\n")
		}
		if value := checkInt32(s.ExpiryDelay); value != nil {
			sb.WriteString("  expiry_delay: " + *value + "\n")
		}
		if value := checkInt32(s.MinExpiryDelay); value != nil {
			sb.WriteString("  min_expiry_delay: " + *value + "\n")
		}
		if value := checkInt32(s.MaxExpiryDelay); value != nil {
			sb.WriteString("  max_expiry_delay: " + *value + "\n")
		}
		if value := checkInt32(s.RedeliveryDelay); value != nil {
			sb.WriteString("  redelivery_delay: " + *value + "\n")
		}
		if value := checkFloat32(s.RedeliveryDelayMultiplier); value != nil {
			sb.WriteString("  redelivery_delay_multiplier: " + *value + "\n")
		}
		if value := checkFloat32(s.RedeliveryCollisionAvoidanceFactor); value != nil {
			sb.WriteString("  redelivery_collision_avoidance_factor: " + *value + "\n")
		}
		if value := checkInt32(s.MaxRedeliveryDelay); value != nil {
			sb.WriteString("  max_redelivery_delay: " + *value + "\n")
		}
		if value := checkInt32(s.MaxDeliveryAttempts); value != nil {
			sb.WriteString("  max_delivery_attempts: " + *value + "\n")
		}
		if value := checkStringSpecial(s.MaxSizeBytes, specials); value != nil {
			sb.WriteString("  max_size_bytes: " + *value + "\n")
		}
		if value := checkInt32(s.MaxSizeBytesRejectThreshold); value != nil {
			sb.WriteString("  max_size_bytes_reject_threshold: " + *value + "\n")
		}
		if value := checkStringSpecial(s.PageSizeBytes, specials); value != nil {
			sb.WriteString("  page_size_bytes: " + *value + "\n")
		}
		if value := checkInt32(s.PageMaxCacheSize); value != nil {
			sb.WriteString("  page_max_cache_size: " + *value + "\n")
		}
		if value := checkStringSpecial(s.AddressFullPolicy, specials); value != nil {
			sb.WriteString("  address_full_policy: " + *value + "\n")
		}
		if value := checkInt32(s.MessageCounterHistoryDayLimit); value != nil {
			sb.WriteString("  message_counter_history_day_limit: " + *value + "\n")
		}
		if value := checkBool(s.LastValueQueue); value != nil {
			sb.WriteString("  last_value_queue: " + *value + "\n")
		}
		if value := checkBool(s.DefaultLastValueQueue); value != nil {
			sb.WriteString("  default_last_value_queue: " + *value + "\n")
		}
		if value := checkStringSpecial(s.DefaultLastValueKey, specials); value != nil {
			sb.WriteString("  default_last_value_key: " + *value + "\n")
		}
		if value := checkBool(s.DefaultNonDestructive); value != nil {
			sb.WriteString("  default_non_destructive: " + *value + "\n")
		}
		if value := checkBool(s.DefaultExclusiveQueue); value != nil {
			sb.WriteString("  default_exclusive_queue: " + *value + "\n")
		}
		if value := checkBool(s.DefaultGroupRebalance); value != nil {
			sb.WriteString("  default_group_rebalance: " + *value + "\n")
		}
		if value := checkBool(s.DefaultGroupRebalancePauseDispatch); value != nil {
			sb.WriteString("  default_group_rebalance_pause_dispatch: " + *value + "\n")
		}
		if value := checkInt32(s.DefaultGroupBuckets); value != nil {
			sb.WriteString("  default_group_buckets: " + *value + "\n")
		}
		if value := checkStringSpecial(s.DefaultGroupFirstKey, specials); value != nil {
			sb.WriteString("  default_group_first_key: " + *value + "\n")
		}
		if value := checkInt32(s.DefaultConsumersBeforeDispatch); value != nil {
			sb.WriteString("  default_consumers_before_dispatch: " + *value + "\n")
		}
		if value := checkInt32(s.DefaultDelayBeforeDispatch); value != nil {
			sb.WriteString("  default_delay_before_dispatch: " + *value + "\n")
		}
		if value := checkInt32(s.RedistributionDelay); value != nil {
			sb.WriteString("  redistribution_delay: " + *value + "\n")
		}
		if value := checkBool(s.SendToDlaOnNoRoute); value != nil {
			sb.WriteString("  send_to_dla_on_no_route: " + *value + "\n")
		}
		if value := checkInt32(s.SlowConsumerThreshold); value != nil {
			sb.WriteString("  slow_consumer_threshold: " + *value + "\n")
		}
		if value := checkStringSpecial(s.SlowConsumerPolicy, specials); value != nil {
			sb.WriteString("  slow_consumer_policy: " + *value + "\n")
		}
		if value := checkInt32(s.SlowConsumerCheckPeriod); value != nil {
			sb.WriteString("  slow_consumer_check_period: " + *value + "\n")
		}
		if value := checkBool(s.AutoCreateJmsQueues); value != nil {
			sb.WriteString("  auto_create_jms_queues: " + *value + "\n")
		}
		if value := checkBool(s.AutoDeleteJmsQueues); value != nil {
			sb.WriteString("  auto_delete_jms_queues: " + *value + "\n")
		}
		if value := checkBool(s.AutoCreateJmsTopics); value != nil {
			sb.WriteString("  auto_create_jms_topics: " + *value + "\n")
		}
		if value := checkBool(s.AutoDeleteJmsTopics); value != nil {
			sb.WriteString("  auto_delete_jms_topics: " + *value + "\n")
		}
		if value := checkBool(s.AutoCreateQueues); value != nil {
			sb.WriteString("  auto_create_queues: " + *value + "\n")
		}
		if value := checkBool(s.AutoDeleteQueues); value != nil {
			sb.WriteString("  auto_delete_queues: " + *value + "\n")
		}
		if value := checkBool(s.AutoDeleteCreatedQueues); value != nil {
			sb.WriteString("  auto_delete_created_queues: " + *value + "\n")
		}
		if value := checkInt32(s.AutoDeleteQueuesDelay); value != nil {
			sb.WriteString("  auto_delete_queues_delay: " + *value + "\n")
		}
		if value := checkInt32(s.AutoDeleteQueuesMessageCount); value != nil {
			sb.WriteString("  auto_delete_queues_message_count: " + *value + "\n")
		}
		if value := checkStringSpecial(s.ConfigDeleteQueues, specials); value != nil {
			sb.WriteString("  config_delete_queues: " + *value + "\n")
		}
		if value := checkBool(s.AutoCreateAddresses); value != nil {
			sb.WriteString("  auto_create_addresses: " + *value + "\n")
		}
		if value := checkBool(s.AutoDeleteAddresses); value != nil {
			sb.WriteString("  auto_delete_addresses: " + *value + "\n")
		}
		if value := checkInt32(s.AutoDeleteAddressesDelay); value != nil {
			sb.WriteString("  auto_delete_addresses_delay: " + *value + "\n")
		}
		if value := checkStringSpecial(s.ConfigDeleteAddresses, specials); value != nil {
			sb.WriteString("  config_delete_addresses: " + *value + "\n")
		}
		if value := checkInt32(s.ManagementBrowsePageSize); value != nil {
			sb.WriteString("  management_browse_page_size: " + *value + "\n")
		}
		if value := checkBool(s.DefaultPurgeOnNoConsumers); value != nil {
			sb.WriteString("  default_purge_on_no_consumers: " + *value + "\n")
		}
		if value := checkInt32(s.DefaultMaxConsumers); value != nil {
			sb.WriteString("  default_max_consumers: " + *value + "\n")
		}
		if value := checkStringSpecial(s.DefaultQueueRoutingType, specials); value != nil {
			sb.WriteString("  default_queue_routing_type: " + *value + "\n")
		}
		if value := checkStringSpecial(s.DefaultAddressRoutingType, specials); value != nil {
			sb.WriteString("  default_address_routing_type: " + *value + "\n")
		}
		if value := checkInt32(s.DefaultConsumerWindowSize); value != nil {
			sb.WriteString("  default_consumer_window_size: " + *value + "\n")
		}
		if value := checkInt32(s.DefaultRingSize); value != nil {
			sb.WriteString("  default_ring_size: " + *value + "\n")
		}
		if value := checkInt32(s.RetroactiveMessageCount); value != nil {
			sb.WriteString("  retroactive_message_count: " + *value + "\n")
		}
		if value := checkBool(s.EnableMetrics); value != nil {
			sb.WriteString("  enable_metrics: " + *value + "\n")
		}
		if value := checkInt32(s.ManagementMessageAttributeSizeLimit); value != nil {
			sb.WriteString("  management_message_attribute_size_limit: " + *value + "\n")
		}
		if value := checkStringSpecial(s.SlowConsumerThresholdMeasurementUnit, specials); value != nil {
			sb.WriteString("  slow_consumer_threshold_measurement_unit: " + *value + "\n")
		}
		if value := checkBool(s.EnableIngressTimestamp); value != nil {
			sb.WriteString("  enable_ingress_timestamp: " + *value + "\n")
		}
	}

}

func processAddressSettingsV1beta1(sb *strings.Builder, addressSettings *[]v1beta1.AddressSettingType, specials map[string]string) {

	if addressSettings == nil || len(*addressSettings) == 0 {
		return
	}
	sb.WriteString("user_address_settings:\n")
	for _, s := range *addressSettings {
		if matchValue := checkStringSpecial(&s.Match, specials); matchValue != nil {
			sb.WriteString("- match: " + *matchValue + "\n")
		}
		if value := checkStringSpecial(s.DeadLetterAddress, specials); value != nil {
			sb.WriteString("  dead_letter_address: " + *value + "\n")
		}
		if value := checkBool(s.AutoCreateDeadLetterResources); value != nil {
			sb.WriteString("  auto_create_dead_letter_resources: " + *value + "\n")
		}
		if value := checkStringSpecial(s.DeadLetterQueuePrefix, specials); value != nil {
			sb.WriteString("  dead_letter_queue_prefix: " + *value + "\n")
		}
		if value := checkStringSpecial(s.DeadLetterQueueSuffix, specials); value != nil {
			sb.WriteString("  dead_letter_queue_suffix: " + *value + "\n")
		}
		if value := checkStringSpecial(s.ExpiryAddress, specials); value != nil {
			sb.WriteString("  expiry_address: " + *value + "\n")
		}
		if value := checkBool(s.AutoCreateExpiryResources); value != nil {
			sb.WriteString("  auto_create_expiry_resources: " + *value + "\n")
		}
		if value := checkStringSpecial(s.ExpiryQueuePrefix, specials); value != nil {
			sb.WriteString("  expiry_queue_prefix: " + *value + "\n")
		}
		if value := checkStringSpecial(s.ExpiryQueueSuffix, specials); value != nil {
			sb.WriteString("  expiry_queue_suffix: " + *value + "\n")
		}
		if value := checkInt32(s.ExpiryDelay); value != nil {
			sb.WriteString("  expiry_delay: " + *value + "\n")
		}
		if value := checkInt32(s.MinExpiryDelay); value != nil {
			sb.WriteString("  min_expiry_delay: " + *value + "\n")
		}
		if value := checkInt32(s.MaxExpiryDelay); value != nil {
			sb.WriteString("  max_expiry_delay: " + *value + "\n")
		}
		if value := checkInt32(s.RedeliveryDelay); value != nil {
			sb.WriteString("  redelivery_delay: " + *value + "\n")
		}
		if value := checkInt32(s.MaxRedeliveryDelay); value != nil {
			sb.WriteString("  max_redelivery_delay: " + *value + "\n")
		}
		if value := checkInt32(s.MaxDeliveryAttempts); value != nil {
			sb.WriteString("  max_delivery_attempts: " + *value + "\n")
		}
		if value := checkStringSpecial(s.MaxSizeBytes, specials); value != nil {
			sb.WriteString("  max_size_bytes: " + *value + "\n")
		}
		if value := checkInt32(s.MaxSizeBytesRejectThreshold); value != nil {
			sb.WriteString("  max_size_bytes_reject_threshold: " + *value + "\n")
		}
		if value := checkStringSpecial(s.PageSizeBytes, specials); value != nil {
			sb.WriteString("  page_size_bytes: " + *value + "\n")
		}
		if value := checkInt32(s.PageMaxCacheSize); value != nil {
			sb.WriteString("  page_max_cache_size: " + *value + "\n")
		}
		if value := checkStringSpecial(s.AddressFullPolicy, specials); value != nil {
			sb.WriteString("  address_full_policy: " + *value + "\n")
		}
		if value := checkInt32(s.MessageCounterHistoryDayLimit); value != nil {
			sb.WriteString("  message_counter_history_day_limit: " + *value + "\n")
		}
		if value := checkBool(s.LastValueQueue); value != nil {
			sb.WriteString("  last_value_queue: " + *value + "\n")
		}
		if value := checkBool(s.DefaultLastValueQueue); value != nil {
			sb.WriteString("  default_last_value_queue: " + *value + "\n")
		}
		if value := checkStringSpecial(s.DefaultLastValueKey, specials); value != nil {
			sb.WriteString("  default_last_value_key: " + *value + "\n")
		}
		if value := checkBool(s.DefaultNonDestructive); value != nil {
			sb.WriteString("  default_non_destructive: " + *value + "\n")
		}
		if value := checkBool(s.DefaultExclusiveQueue); value != nil {
			sb.WriteString("  default_exclusive_queue: " + *value + "\n")
		}
		if value := checkBool(s.DefaultGroupRebalance); value != nil {
			sb.WriteString("  default_group_rebalance: " + *value + "\n")
		}
		if value := checkBool(s.DefaultGroupRebalancePauseDispatch); value != nil {
			sb.WriteString("  default_group_rebalance_pause_dispatch: " + *value + "\n")
		}
		if value := checkInt32(s.DefaultGroupBuckets); value != nil {
			sb.WriteString("  default_group_buckets: " + *value + "\n")
		}
		if value := checkStringSpecial(s.DefaultGroupFirstKey, specials); value != nil {
			sb.WriteString("  default_group_first_key: " + *value + "\n")
		}
		if value := checkInt32(s.DefaultConsumersBeforeDispatch); value != nil {
			sb.WriteString("  default_consumers_before_dispatch: " + *value + "\n")
		}
		if value := checkInt32(s.DefaultDelayBeforeDispatch); value != nil {
			sb.WriteString("  default_delay_before_dispatch: " + *value + "\n")
		}
		if value := checkInt32(s.RedistributionDelay); value != nil {
			sb.WriteString("  redistribution_delay: " + *value + "\n")
		}
		if value := checkBool(s.SendToDlaOnNoRoute); value != nil {
			sb.WriteString("  send_to_dla_on_no_route: " + *value + "\n")
		}
		if value := checkInt32(s.SlowConsumerThreshold); value != nil {
			sb.WriteString("  slow_consumer_threshold: " + *value + "\n")
		}
		if value := checkStringSpecial(s.SlowConsumerPolicy, specials); value != nil {
			sb.WriteString("  slow_consumer_policy: " + *value + "\n")
		}
		if value := checkInt32(s.SlowConsumerCheckPeriod); value != nil {
			sb.WriteString("  slow_consumer_check_period: " + *value + "\n")
		}
		if value := checkBool(s.AutoCreateJmsQueues); value != nil {
			sb.WriteString("  auto_create_jms_queues: " + *value + "\n")
		}
		if value := checkBool(s.AutoDeleteJmsQueues); value != nil {
			sb.WriteString("  auto_delete_jms_queues: " + *value + "\n")
		}
		if value := checkBool(s.AutoCreateJmsTopics); value != nil {
			sb.WriteString("  auto_create_jms_topics: " + *value + "\n")
		}
		if value := checkBool(s.AutoDeleteJmsTopics); value != nil {
			sb.WriteString("  auto_delete_jms_topics: " + *value + "\n")
		}
		if value := checkBool(s.AutoCreateQueues); value != nil {
			sb.WriteString("  auto_create_queues: " + *value + "\n")
		}
		if value := checkBool(s.AutoDeleteQueues); value != nil {
			sb.WriteString("  auto_delete_queues: " + *value + "\n")
		}
		if value := checkBool(s.AutoDeleteCreatedQueues); value != nil {
			sb.WriteString("  auto_delete_created_queues: " + *value + "\n")
		}
		if value := checkInt32(s.AutoDeleteQueuesDelay); value != nil {
			sb.WriteString("  auto_delete_queues_delay: " + *value + "\n")
		}
		if value := checkInt32(s.AutoDeleteQueuesMessageCount); value != nil {
			sb.WriteString("  auto_delete_queues_message_count: " + *value + "\n")
		}
		if value := checkStringSpecial(s.ConfigDeleteQueues, specials); value != nil {
			sb.WriteString("  config_delete_queues: " + *value + "\n")
		}
		if value := checkBool(s.AutoCreateAddresses); value != nil {
			sb.WriteString("  auto_create_addresses: " + *value + "\n")
		}
		if value := checkBool(s.AutoDeleteAddresses); value != nil {
			sb.WriteString("  auto_delete_addresses: " + *value + "\n")
		}
		if value := checkInt32(s.AutoDeleteAddressesDelay); value != nil {
			sb.WriteString("  auto_delete_addresses_delay: " + *value + "\n")
		}
		if value := checkStringSpecial(s.ConfigDeleteAddresses, specials); value != nil {
			sb.WriteString("  config_delete_addresses: " + *value + "\n")
		}
		if value := checkInt32(s.ManagementBrowsePageSize); value != nil {
			sb.WriteString("  management_browse_page_size: " + *value + "\n")
		}
		if value := checkBool(s.DefaultPurgeOnNoConsumers); value != nil {
			sb.WriteString("  default_purge_on_no_consumers: " + *value + "\n")
		}
		if value := checkInt32(s.DefaultMaxConsumers); value != nil {
			sb.WriteString("  default_max_consumers: " + *value + "\n")
		}
		if value := checkStringSpecial(s.DefaultQueueRoutingType, specials); value != nil {
			sb.WriteString("  default_queue_routing_type: " + *value + "\n")
		}
		if value := checkStringSpecial(s.DefaultAddressRoutingType, specials); value != nil {
			sb.WriteString("  default_address_routing_type: " + *value + "\n")
		}
		if value := checkInt32(s.DefaultConsumerWindowSize); value != nil {
			sb.WriteString("  default_consumer_window_size: " + *value + "\n")
		}
		if value := checkInt32(s.DefaultRingSize); value != nil {
			sb.WriteString("  default_ring_size: " + *value + "\n")
		}
		if value := checkInt32(s.RetroactiveMessageCount); value != nil {
			sb.WriteString("  retroactive_message_count: " + *value + "\n")
		}
		if value := checkBool(s.EnableMetrics); value != nil {
			sb.WriteString("  enable_metrics: " + *value + "\n")
		}
		if value := checkInt32(s.ManagementMessageAttributeSizeLimit); value != nil {
			sb.WriteString("  management_message_attribute_size_limit: " + *value + "\n")
		}
		if value := checkStringSpecial(s.SlowConsumerThresholdMeasurementUnit, specials); value != nil {
			sb.WriteString("  slow_consumer_threshold_measurement_unit: " + *value + "\n")
		}
		if value := checkBool(s.EnableIngressTimestamp); value != nil {
			sb.WriteString("  enable_ingress_timestamp: " + *value + "\n")
		}
		if value := checkInt64(s.MaxSizeMessages); value != nil {
			sb.WriteString("  max_size_messages: " + *value + "\n")
		}
		if value := checkStringSpecial(s.ConfigDeleteDiverts, specials); value != nil {
			sb.WriteString("  config_delete_diverts: " + *value + "\n")
		}
	}

}

func processAddressSettingsV1beta2(sb *strings.Builder, addressSettings *[]v1beta2.AddressSettingType, specials map[string]string) {

	if addressSettings == nil || len(*addressSettings) == 0 {
		return
	}
	sb.WriteString("user_address_settings:\n")
	for _, s := range *addressSettings {
		if matchValue := checkStringSpecial(&s.Match, specials); matchValue != nil {
			sb.WriteString("- match: " + *matchValue + "\n")
		}
		if value := checkStringSpecial(s.DeadLetterAddress, specials); value != nil {
			sb.WriteString("  dead_letter_address: " + *value + "\n")
		}
		if value := checkBool(s.AutoCreateDeadLetterResources); value != nil {
			sb.WriteString("  auto_create_dead_letter_resources: " + *value + "\n")
		}
		if value := checkStringSpecial(s.DeadLetterQueuePrefix, specials); value != nil {
			sb.WriteString("  dead_letter_queue_prefix: " + *value + "\n")
		}
		if value := checkStringSpecial(s.DeadLetterQueueSuffix, specials); value != nil {
			sb.WriteString("  dead_letter_queue_suffix: " + *value + "\n")
		}
		if value := checkStringSpecial(s.ExpiryAddress, specials); value != nil {
			sb.WriteString("  expiry_address: " + *value + "\n")
		}
		if value := checkBool(s.AutoCreateExpiryResources); value != nil {
			sb.WriteString("  auto_create_expiry_resources: " + *value + "\n")
		}
		if value := checkStringSpecial(s.ExpiryQueuePrefix, specials); value != nil {
			sb.WriteString("  expiry_queue_prefix: " + *value + "\n")
		}
		if value := checkStringSpecial(s.ExpiryQueueSuffix, specials); value != nil {
			sb.WriteString("  expiry_queue_suffix: " + *value + "\n")
		}
		if value := checkInt32(s.ExpiryDelay); value != nil {
			sb.WriteString("  expiry_delay: " + *value + "\n")
		}
		if value := checkInt32(s.MinExpiryDelay); value != nil {
			sb.WriteString("  min_expiry_delay: " + *value + "\n")
		}
		if value := checkInt32(s.MaxExpiryDelay); value != nil {
			sb.WriteString("  max_expiry_delay: " + *value + "\n")
		}
		if value := checkInt32(s.RedeliveryDelay); value != nil {
			sb.WriteString("  redelivery_delay: " + *value + "\n")
		}
		if value := checkInt32(s.MaxRedeliveryDelay); value != nil {
			sb.WriteString("  max_redelivery_delay: " + *value + "\n")
		}
		if value := checkInt32(s.MaxDeliveryAttempts); value != nil {
			sb.WriteString("  max_delivery_attempts: " + *value + "\n")
		}
		if value := checkStringSpecial(s.MaxSizeBytes, specials); value != nil {
			sb.WriteString("  max_size_bytes: " + *value + "\n")
		}
		if value := checkInt32(s.MaxSizeBytesRejectThreshold); value != nil {
			sb.WriteString("  max_size_bytes_reject_threshold: " + *value + "\n")
		}
		if value := checkStringSpecial(s.PageSizeBytes, specials); value != nil {
			sb.WriteString("  page_size_bytes: " + *value + "\n")
		}
		if value := checkInt32(s.PageMaxCacheSize); value != nil {
			sb.WriteString("  page_max_cache_size: " + *value + "\n")
		}
		if value := checkStringSpecial(s.AddressFullPolicy, specials); value != nil {
			sb.WriteString("  address_full_policy: " + *value + "\n")
		}
		if value := checkInt32(s.MessageCounterHistoryDayLimit); value != nil {
			sb.WriteString("  message_counter_history_day_limit: " + *value + "\n")
		}
		if value := checkBool(s.LastValueQueue); value != nil {
			sb.WriteString("  last_value_queue: " + *value + "\n")
		}
		if value := checkBool(s.DefaultLastValueQueue); value != nil {
			sb.WriteString("  default_last_value_queue: " + *value + "\n")
		}
		if value := checkStringSpecial(s.DefaultLastValueKey, specials); value != nil {
			sb.WriteString("  default_last_value_key: " + *value + "\n")
		}
		if value := checkBool(s.DefaultNonDestructive); value != nil {
			sb.WriteString("  default_non_destructive: " + *value + "\n")
		}
		if value := checkBool(s.DefaultExclusiveQueue); value != nil {
			sb.WriteString("  default_exclusive_queue: " + *value + "\n")
		}
		if value := checkBool(s.DefaultGroupRebalance); value != nil {
			sb.WriteString("  default_group_rebalance: " + *value + "\n")
		}
		if value := checkBool(s.DefaultGroupRebalancePauseDispatch); value != nil {
			sb.WriteString("  default_group_rebalance_pause_dispatch: " + *value + "\n")
		}
		if value := checkInt32(s.DefaultGroupBuckets); value != nil {
			sb.WriteString("  default_group_buckets: " + *value + "\n")
		}
		if value := checkStringSpecial(s.DefaultGroupFirstKey, specials); value != nil {
			sb.WriteString("  default_group_first_key: " + *value + "\n")
		}
		if value := checkInt32(s.DefaultConsumersBeforeDispatch); value != nil {
			sb.WriteString("  default_consumers_before_dispatch: " + *value + "\n")
		}
		if value := checkInt32(s.DefaultDelayBeforeDispatch); value != nil {
			sb.WriteString("  default_delay_before_dispatch: " + *value + "\n")
		}
		if value := checkInt32(s.RedistributionDelay); value != nil {
			sb.WriteString("  redistribution_delay: " + *value + "\n")
		}
		if value := checkBool(s.SendToDlaOnNoRoute); value != nil {
			sb.WriteString("  send_to_dla_on_no_route: " + *value + "\n")
		}
		if value := checkInt32(s.SlowConsumerThreshold); value != nil {
			sb.WriteString("  slow_consumer_threshold: " + *value + "\n")
		}
		if value := checkStringSpecial(s.SlowConsumerPolicy, specials); value != nil {
			sb.WriteString("  slow_consumer_policy: " + *value + "\n")
		}
		if value := checkInt32(s.SlowConsumerCheckPeriod); value != nil {
			sb.WriteString("  slow_consumer_check_period: " + *value + "\n")
		}
		if value := checkBool(s.AutoCreateJmsQueues); value != nil {
			sb.WriteString("  auto_create_jms_queues: " + *value + "\n")
		}
		if value := checkBool(s.AutoDeleteJmsQueues); value != nil {
			sb.WriteString("  auto_delete_jms_queues: " + *value + "\n")
		}
		if value := checkBool(s.AutoCreateJmsTopics); value != nil {
			sb.WriteString("  auto_create_jms_topics: " + *value + "\n")
		}
		if value := checkBool(s.AutoDeleteJmsTopics); value != nil {
			sb.WriteString("  auto_delete_jms_topics: " + *value + "\n")
		}
		if value := checkBool(s.AutoCreateQueues); value != nil {
			sb.WriteString("  auto_create_queues: " + *value + "\n")
		}
		if value := checkBool(s.AutoDeleteQueues); value != nil {
			sb.WriteString("  auto_delete_queues: " + *value + "\n")
		}
		if value := checkBool(s.AutoDeleteCreatedQueues); value != nil {
			sb.WriteString("  auto_delete_created_queues: " + *value + "\n")
		}
		if value := checkInt32(s.AutoDeleteQueuesDelay); value != nil {
			sb.WriteString("  auto_delete_queues_delay: " + *value + "\n")
		}
		if value := checkInt32(s.AutoDeleteQueuesMessageCount); value != nil {
			sb.WriteString("  auto_delete_queues_message_count: " + *value + "\n")
		}
		if value := checkStringSpecial(s.ConfigDeleteQueues, specials); value != nil {
			sb.WriteString("  config_delete_queues: " + *value + "\n")
		}
		if value := checkBool(s.AutoCreateAddresses); value != nil {
			sb.WriteString("  auto_create_addresses: " + *value + "\n")
		}
		if value := checkBool(s.AutoDeleteAddresses); value != nil {
			sb.WriteString("  auto_delete_addresses: " + *value + "\n")
		}
		if value := checkInt32(s.AutoDeleteAddressesDelay); value != nil {
			sb.WriteString("  auto_delete_addresses_delay: " + *value + "\n")
		}
		if value := checkStringSpecial(s.ConfigDeleteAddresses, specials); value != nil {
			sb.WriteString("  config_delete_addresses: " + *value + "\n")
		}
		if value := checkInt32(s.ManagementBrowsePageSize); value != nil {
			sb.WriteString("  management_browse_page_size: " + *value + "\n")
		}
		if value := checkBool(s.DefaultPurgeOnNoConsumers); value != nil {
			sb.WriteString("  default_purge_on_no_consumers: " + *value + "\n")
		}
		if value := checkInt32(s.DefaultMaxConsumers); value != nil {
			sb.WriteString("  default_max_consumers: " + *value + "\n")
		}
		if value := checkStringSpecial(s.DefaultQueueRoutingType, specials); value != nil {
			sb.WriteString("  default_queue_routing_type: " + *value + "\n")
		}
		if value := checkStringSpecial(s.DefaultAddressRoutingType, specials); value != nil {
			sb.WriteString("  default_address_routing_type: " + *value + "\n")
		}
		if value := checkInt32(s.DefaultConsumerWindowSize); value != nil {
			sb.WriteString("  default_consumer_window_size: " + *value + "\n")
		}
		if value := checkInt32(s.DefaultRingSize); value != nil {
			sb.WriteString("  default_ring_size: " + *value + "\n")
		}
		if value := checkInt32(s.RetroactiveMessageCount); value != nil {
			sb.WriteString("  retroactive_message_count: " + *value + "\n")
		}
		if value := checkBool(s.EnableMetrics); value != nil {
			sb.WriteString("  enable_metrics: " + *value + "\n")
		}
		if value := checkInt32(s.ManagementMessageAttributeSizeLimit); value != nil {
			sb.WriteString("  management_message_attribute_size_limit: " + *value + "\n")
		}
		if value := checkStringSpecial(s.SlowConsumerThresholdMeasurementUnit, specials); value != nil {
			sb.WriteString("  slow_consumer_threshold_measurement_unit: " + *value + "\n")
		}
		if value := checkBool(s.EnableIngressTimestamp); value != nil {
			sb.WriteString("  enable_ingress_timestamp: " + *value + "\n")
		}
		if value := checkInt64(s.MaxSizeMessages); value != nil {
			sb.WriteString("  max_size_messages: " + *value + "\n")
		}
		if value := checkStringSpecial(s.ConfigDeleteDiverts, specials); value != nil {
			sb.WriteString("  config_delete_diverts: " + *value + "\n")
		}
	}

}
