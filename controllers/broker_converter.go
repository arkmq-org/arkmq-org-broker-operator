package controllers

import (
	brokerv1beta1 "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta1"
	v1beta2 "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ConvertArtemisToBroker converts a v1beta1.ActiveMQArtemis to a v1beta2.Broker
// using static field mapping for compile-time safety.
// The ActiveMQArtemis GVK is preserved on the converted object so that owner
// references on child resources (StatefulSets, Services, etc.) correctly
// point back to the ActiveMQArtemis CR.
func ConvertArtemisToBroker(artemis *brokerv1beta1.ActiveMQArtemis) (*v1beta2.Broker, error) {
	broker := &v1beta2.Broker{
		TypeMeta: metav1.TypeMeta{
			APIVersion: brokerv1beta1.GroupVersion.String(),
			Kind:       "ActiveMQArtemis",
		},
		ObjectMeta: *artemis.ObjectMeta.DeepCopy(),
		Spec:       convertSpecToBroker(&artemis.Spec),
		Status:     convertStatusToBroker(&artemis.Status),
	}
	return broker, nil
}

// ConvertBrokerStatusToArtemis copies the reconciled status from a Broker
// back to the original ActiveMQArtemis CR for persistence.
func ConvertBrokerStatusToArtemis(broker *v1beta2.Broker, artemis *brokerv1beta1.ActiveMQArtemis) error {
	artemis.Status = convertStatusToArtemis(&broker.Status)
	return nil
}

func convertSpecToBroker(s *brokerv1beta1.ActiveMQArtemisSpec) v1beta2.BrokerSpec {
	return v1beta2.BrokerSpec{
		AdminUser:         s.AdminUser,
		AdminPassword:     s.AdminPassword,
		DeploymentPlan:    convertDeploymentPlanToBroker(&s.DeploymentPlan),
		Acceptors:         convertAcceptorsToBroker(s.Acceptors),
		Connectors:        convertConnectorsToBroker(s.Connectors),
		Console:           convertConsoleToBroker(&s.Console),
		Version:           s.Version,
		Upgrades:          convertUpgradesToBroker(&s.Upgrades),
		AddressSettings:   convertAddressSettingsToBroker(&s.AddressSettings),
		BrokerProperties:  s.BrokerProperties,
		Env:               s.Env,
		IngressDomain:     s.IngressDomain,
		ResourceTemplates: convertResourceTemplatesToBroker(s.ResourceTemplates),
		Restricted:        s.Restricted,
	}
}

func convertAddressSettingsToBroker(as *brokerv1beta1.AddressSettingsType) v1beta2.AddressSettingsType {
	result := v1beta2.AddressSettingsType{
		ApplyRule: as.ApplyRule,
	}
	if as.AddressSetting != nil {
		result.AddressSetting = make([]v1beta2.AddressSettingType, len(as.AddressSetting))
		for i, a := range as.AddressSetting {
			result.AddressSetting[i] = v1beta2.AddressSettingType{
				DeadLetterAddress:                    a.DeadLetterAddress,
				AutoCreateDeadLetterResources:        a.AutoCreateDeadLetterResources,
				DeadLetterQueuePrefix:                a.DeadLetterQueuePrefix,
				DeadLetterQueueSuffix:                a.DeadLetterQueueSuffix,
				ExpiryAddress:                        a.ExpiryAddress,
				AutoCreateExpiryResources:            a.AutoCreateExpiryResources,
				ExpiryQueuePrefix:                    a.ExpiryQueuePrefix,
				ExpiryQueueSuffix:                    a.ExpiryQueueSuffix,
				ExpiryDelay:                          a.ExpiryDelay,
				MinExpiryDelay:                       a.MinExpiryDelay,
				MaxExpiryDelay:                       a.MaxExpiryDelay,
				RedeliveryDelay:                      a.RedeliveryDelay,
				MaxRedeliveryDelay:                   a.MaxRedeliveryDelay,
				MaxDeliveryAttempts:                  a.MaxDeliveryAttempts,
				MaxSizeBytes:                         a.MaxSizeBytes,
				MaxSizeBytesRejectThreshold:          a.MaxSizeBytesRejectThreshold,
				PageSizeBytes:                        a.PageSizeBytes,
				PageMaxCacheSize:                     a.PageMaxCacheSize,
				AddressFullPolicy:                    a.AddressFullPolicy,
				MessageCounterHistoryDayLimit:        a.MessageCounterHistoryDayLimit,
				LastValueQueue:                       a.LastValueQueue,
				DefaultLastValueQueue:                a.DefaultLastValueQueue,
				DefaultLastValueKey:                  a.DefaultLastValueKey,
				DefaultNonDestructive:                a.DefaultNonDestructive,
				DefaultExclusiveQueue:                a.DefaultExclusiveQueue,
				DefaultGroupRebalance:                a.DefaultGroupRebalance,
				DefaultGroupRebalancePauseDispatch:   a.DefaultGroupRebalancePauseDispatch,
				DefaultGroupBuckets:                  a.DefaultGroupBuckets,
				DefaultGroupFirstKey:                 a.DefaultGroupFirstKey,
				DefaultConsumersBeforeDispatch:       a.DefaultConsumersBeforeDispatch,
				DefaultDelayBeforeDispatch:           a.DefaultDelayBeforeDispatch,
				RedistributionDelay:                  a.RedistributionDelay,
				SendToDlaOnNoRoute:                   a.SendToDlaOnNoRoute,
				SlowConsumerThreshold:                a.SlowConsumerThreshold,
				SlowConsumerPolicy:                   a.SlowConsumerPolicy,
				SlowConsumerCheckPeriod:              a.SlowConsumerCheckPeriod,
				AutoCreateJmsQueues:                  a.AutoCreateJmsQueues,
				AutoDeleteJmsQueues:                  a.AutoDeleteJmsQueues,
				AutoCreateJmsTopics:                  a.AutoCreateJmsTopics,
				AutoDeleteJmsTopics:                  a.AutoDeleteJmsTopics,
				AutoCreateQueues:                     a.AutoCreateQueues,
				AutoDeleteQueues:                     a.AutoDeleteQueues,
				AutoDeleteCreatedQueues:              a.AutoDeleteCreatedQueues,
				AutoDeleteQueuesDelay:                a.AutoDeleteQueuesDelay,
				AutoDeleteQueuesMessageCount:         a.AutoDeleteQueuesMessageCount,
				ConfigDeleteQueues:                   a.ConfigDeleteQueues,
				AutoCreateAddresses:                  a.AutoCreateAddresses,
				AutoDeleteAddresses:                  a.AutoDeleteAddresses,
				AutoDeleteAddressesDelay:             a.AutoDeleteAddressesDelay,
				ConfigDeleteAddresses:                a.ConfigDeleteAddresses,
				ManagementBrowsePageSize:             a.ManagementBrowsePageSize,
				DefaultPurgeOnNoConsumers:            a.DefaultPurgeOnNoConsumers,
				DefaultMaxConsumers:                  a.DefaultMaxConsumers,
				DefaultQueueRoutingType:              a.DefaultQueueRoutingType,
				DefaultAddressRoutingType:            a.DefaultAddressRoutingType,
				DefaultConsumerWindowSize:            a.DefaultConsumerWindowSize,
				DefaultRingSize:                      a.DefaultRingSize,
				RetroactiveMessageCount:              a.RetroactiveMessageCount,
				EnableMetrics:                        a.EnableMetrics,
				Match:                                a.Match,
				ManagementMessageAttributeSizeLimit:  a.ManagementMessageAttributeSizeLimit,
				SlowConsumerThresholdMeasurementUnit: a.SlowConsumerThresholdMeasurementUnit,
				EnableIngressTimestamp:               a.EnableIngressTimestamp,
				ConfigDeleteDiverts:                  a.ConfigDeleteDiverts,
				MaxSizeMessages:                      a.MaxSizeMessages,
			}
		}
	}
	return result
}

func convertDeploymentPlanToBroker(d *brokerv1beta1.DeploymentPlanType) v1beta2.DeploymentPlanType {
	return v1beta2.DeploymentPlanType{
		Image:                     d.Image,
		InitImage:                 d.InitImage,
		ImagePullSecrets:          d.ImagePullSecrets,
		Size:                      d.Size,
		RequireLogin:              d.RequireLogin,
		PersistenceEnabled:        d.PersistenceEnabled,
		JournalType:               d.JournalType,
		MessageMigration:          d.MessageMigration,
		Resources:                 d.Resources,
		Storage:                   convertStorageToBroker(&d.Storage),
		TopologySpreadConstraints: d.TopologySpreadConstraints,
		JolokiaAgentEnabled:       d.JolokiaAgentEnabled,
		ManagementRBACEnabled:     d.ManagementRBACEnabled,
		ExtraMounts:               convertExtraMountsToBroker(&d.ExtraMounts),
		Clustered:                 d.Clustered,
		PodSecurity:               convertPodSecurityToBroker(&d.PodSecurity),
		StartupProbe:              d.StartupProbe,
		LivenessProbe:             d.LivenessProbe,
		ReadinessProbe:            d.ReadinessProbe,
		EnableMetricsPlugin:       d.EnableMetricsPlugin,
		Tolerations:               d.Tolerations,
		Labels:                    d.Labels,
		NodeSelector:              d.NodeSelector,
		Affinity:                  convertAffinityToBroker(&d.Affinity),
		PodSecurityContext:        d.PodSecurityContext,
		Annotations:               d.Annotations,
		PodDisruptionBudget:       d.PodDisruptionBudget,
		RevisionHistoryLimit:      d.RevisionHistoryLimit,
		ContainerSecurityContext:  d.ContainerSecurityContext,
		ExtraVolumes:              d.ExtraVolumes,
		ExtraVolumeMounts:         d.ExtraVolumeMounts,
		ExtraVolumeClaimTemplates: convertVolumeClaimTemplatesToBroker(d.ExtraVolumeClaimTemplates),
	}
}

func convertVolumeClaimTemplatesToBroker(templates []brokerv1beta1.VolumeClaimTemplate) []v1beta2.VolumeClaimTemplate {
	if templates == nil {
		return nil
	}
	result := make([]v1beta2.VolumeClaimTemplate, len(templates))
	for i, t := range templates {
		result[i] = v1beta2.VolumeClaimTemplate{
			ObjectMeta: v1beta2.ObjectMeta{
				Name:        t.ObjectMeta.Name,
				Annotations: t.ObjectMeta.Annotations,
				Labels:      t.ObjectMeta.Labels,
			},
			Spec: t.Spec,
		}
	}
	return result
}

func convertResourceTemplatesToBroker(templates []brokerv1beta1.ResourceTemplate) []v1beta2.ResourceTemplate {
	if templates == nil {
		return nil
	}
	result := make([]v1beta2.ResourceTemplate, len(templates))
	for i, t := range templates {
		result[i] = v1beta2.ResourceTemplate{
			Annotations: t.Annotations,
			Labels:      t.Labels,
			Patch:       t.Patch,
		}
		if t.Selector != nil {
			result[i].Selector = &v1beta2.ResourceSelector{
				APIGroup: t.Selector.APIGroup,
				Kind:     t.Selector.Kind,
				Name:     t.Selector.Name,
			}
		}
	}
	return result
}

func convertAffinityToBroker(a *brokerv1beta1.AffinityConfig) v1beta2.AffinityConfig {
	return v1beta2.AffinityConfig{
		NodeAffinity:    a.NodeAffinity,
		PodAffinity:     a.PodAffinity,
		PodAntiAffinity: a.PodAntiAffinity,
	}
}

func convertPodSecurityToBroker(p *brokerv1beta1.PodSecurityType) v1beta2.PodSecurityType {
	return v1beta2.PodSecurityType{
		ServiceAccountName: p.ServiceAccountName,
		RunAsUser:          p.RunAsUser,
	}
}

func convertExtraMountsToBroker(e *brokerv1beta1.ExtraMountsType) v1beta2.ExtraMountsType {
	return v1beta2.ExtraMountsType{
		ConfigMaps: e.ConfigMaps,
		Secrets:    e.Secrets,
	}
}

func convertStorageToBroker(s *brokerv1beta1.StorageType) v1beta2.StorageType {
	return v1beta2.StorageType{
		Size:             s.Size,
		StorageClassName: s.StorageClassName,
	}
}

func convertAcceptorsToBroker(acceptors []brokerv1beta1.AcceptorType) []v1beta2.AcceptorType {
	if acceptors == nil {
		return nil
	}
	result := make([]v1beta2.AcceptorType, len(acceptors))
	for i, a := range acceptors {
		result[i] = v1beta2.AcceptorType{
			Name:                              a.Name,
			Port:                              a.Port,
			Protocols:                         a.Protocols,
			SSLEnabled:                        a.SSLEnabled,
			SSLSecret:                         a.SSLSecret,
			EnabledCipherSuites:               a.EnabledCipherSuites,
			EnabledProtocols:                  a.EnabledProtocols,
			NeedClientAuth:                    a.NeedClientAuth,
			WantClientAuth:                    a.WantClientAuth,
			VerifyHost:                        a.VerifyHost,
			SSLProvider:                       a.SSLProvider,
			SNIHost:                           a.SNIHost,
			Expose:                            a.Expose,
			ExposeMode:                        (*v1beta2.ExposeMode)(a.ExposeMode),
			AnycastPrefix:                     a.AnycastPrefix,
			MulticastPrefix:                   a.MulticastPrefix,
			ConnectionsAllowed:                a.ConnectionsAllowed,
			AMQPMinLargeMessageSize:           a.AMQPMinLargeMessageSize,
			SupportAdvisory:                   a.SupportAdvisory,
			SuppressInternalManagementObjects: a.SuppressInternalManagementObjects,
			BindToAllInterfaces:               a.BindToAllInterfaces,
			KeyStoreProvider:                  a.KeyStoreProvider,
			TrustStoreType:                    a.TrustStoreType,
			TrustStoreProvider:                a.TrustStoreProvider,
			IngressHost:                       a.IngressHost,
			TrustSecret:                       a.TrustSecret,
		}
	}
	return result
}

func convertConnectorsToBroker(connectors []brokerv1beta1.ConnectorType) []v1beta2.ConnectorType {
	if connectors == nil {
		return nil
	}
	result := make([]v1beta2.ConnectorType, len(connectors))
	for i, c := range connectors {
		result[i] = v1beta2.ConnectorType{
			Name:                c.Name,
			Type:                c.Type,
			Host:                c.Host,
			Port:                c.Port,
			SSLEnabled:          c.SSLEnabled,
			SSLSecret:           c.SSLSecret,
			EnabledCipherSuites: c.EnabledCipherSuites,
			EnabledProtocols:    c.EnabledProtocols,
			NeedClientAuth:      c.NeedClientAuth,
			WantClientAuth:      c.WantClientAuth,
			VerifyHost:          c.VerifyHost,
			SSLProvider:         c.SSLProvider,
			SNIHost:             c.SNIHost,
			Expose:              c.Expose,
			ExposeMode:          (*v1beta2.ExposeMode)(c.ExposeMode),
			KeyStoreProvider:    c.KeyStoreProvider,
			TrustStoreType:      c.TrustStoreType,
			TrustStoreProvider:  c.TrustStoreProvider,
			IngressHost:         c.IngressHost,
			TrustSecret:         c.TrustSecret,
		}
	}
	return result
}

func convertConsoleToBroker(c *brokerv1beta1.ConsoleType) v1beta2.ConsoleType {
	return v1beta2.ConsoleType{
		Name:          c.Name,
		Expose:        c.Expose,
		ExposeMode:    (*v1beta2.ExposeMode)(c.ExposeMode),
		SSLEnabled:    c.SSLEnabled,
		SSLSecret:     c.SSLSecret,
		UseClientAuth: c.UseClientAuth,
		IngressHost:   c.IngressHost,
		TrustSecret:   c.TrustSecret,
	}
}

func convertUpgradesToBroker(u *brokerv1beta1.ActiveMQArtemisUpgrades) v1beta2.BrokerUpgrades {
	return v1beta2.BrokerUpgrades{
		Enabled: u.Enabled,
		Minor:   u.Minor,
	}
}

func convertStatusToBroker(s *brokerv1beta1.ActiveMQArtemisStatus) v1beta2.BrokerStatus {
	return v1beta2.BrokerStatus{
		Conditions:         s.Conditions,
		PodStatus:          s.PodStatus,
		DeploymentPlanSize: s.DeploymentPlanSize,
		ScaleLabelSelector: s.ScaleLabelSelector,
		ExternalConfigs:    convertExternalConfigsToBroker(s.ExternalConfigs),
		Version:            convertVersionStatusToBroker(&s.Version),
		Upgrade:            convertUpgradeStatusToBroker(&s.Upgrade),
	}
}

func convertVersionStatusToBroker(v *brokerv1beta1.VersionStatus) v1beta2.VersionStatus {
	return v1beta2.VersionStatus{
		BrokerVersion: v.BrokerVersion,
		Image:         v.Image,
		InitImage:     v.InitImage,
	}
}

func convertUpgradeStatusToBroker(u *brokerv1beta1.UpgradeStatus) v1beta2.UpgradeStatus {
	return v1beta2.UpgradeStatus{
		SecurityUpdates: u.SecurityUpdates,
		MajorUpdates:    u.MajorUpdates,
		MinorUpdates:    u.MinorUpdates,
		PatchUpdates:    u.PatchUpdates,
	}
}

func convertExternalConfigsToBroker(configs []brokerv1beta1.ExternalConfigStatus) []v1beta2.ExternalConfigStatus {
	if configs == nil {
		return nil
	}
	result := make([]v1beta2.ExternalConfigStatus, len(configs))
	for i, c := range configs {
		result[i] = v1beta2.ExternalConfigStatus{
			Name:            c.Name,
			ResourceVersion: c.ResourceVersion,
		}
	}
	return result
}

// --- Reverse converters: Broker status -> Artemis status ---

func convertStatusToArtemis(s *v1beta2.BrokerStatus) brokerv1beta1.ActiveMQArtemisStatus {
	return brokerv1beta1.ActiveMQArtemisStatus{
		Conditions:         s.Conditions,
		PodStatus:          s.PodStatus,
		DeploymentPlanSize: s.DeploymentPlanSize,
		ScaleLabelSelector: s.ScaleLabelSelector,
		ExternalConfigs:    convertExternalConfigsToArtemis(s.ExternalConfigs),
		Version:            convertVersionStatusToArtemis(&s.Version),
		Upgrade:            convertUpgradeStatusToArtemis(&s.Upgrade),
	}
}

func convertVersionStatusToArtemis(v *v1beta2.VersionStatus) brokerv1beta1.VersionStatus {
	return brokerv1beta1.VersionStatus{
		BrokerVersion: v.BrokerVersion,
		Image:         v.Image,
		InitImage:     v.InitImage,
	}
}

func convertUpgradeStatusToArtemis(u *v1beta2.UpgradeStatus) brokerv1beta1.UpgradeStatus {
	return brokerv1beta1.UpgradeStatus{
		SecurityUpdates: u.SecurityUpdates,
		MajorUpdates:    u.MajorUpdates,
		MinorUpdates:    u.MinorUpdates,
		PatchUpdates:    u.PatchUpdates,
	}
}

func convertExternalConfigsToArtemis(configs []v1beta2.ExternalConfigStatus) []brokerv1beta1.ExternalConfigStatus {
	if configs == nil {
		return nil
	}
	result := make([]brokerv1beta1.ExternalConfigStatus, len(configs))
	for i, c := range configs {
		result[i] = brokerv1beta1.ExternalConfigStatus{
			Name:            c.Name,
			ResourceVersion: c.ResourceVersion,
		}
	}
	return result
}
