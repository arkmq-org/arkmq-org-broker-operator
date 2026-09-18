// Broker properties structs serialized to JSON via json.Marshal.
// Artemis parses these through ConfigurationImpl.parseFileProperties().
//
// Source of truth for the JSON structure:
//   - Test:     artemis-server/src/test/java/org/apache/activemq/artemis/core/config/impl/JsonConfigurationFullTest.java
//   - Fixture:  artemis-server/src/test/resources/broker-full-config.json
//
// See: https://github.com/apache/activemq-artemis
package brokerproperties

import (
	"encoding/json"
)

const (
	RoutingTypeAnycast   = "ANYCAST"
	RoutingTypeMulticast = "MULTICAST"

	SaslExternal        = "EXTERNAL"
	KeyStoreTypePEMCFG  = "PEMCFG"
	TrustStoreTypePEMCA = "PEMCA"

	ControlFlagRequired = "required"

	NettyAcceptorFactory    = "org.apache.activemq.artemis.core.remoting.impl.netty.NettyAcceptorFactory"
	TextFileCertLoginModule = "org.apache.activemq.artemis.spi.core.security.jaas.TextFileCertificateLoginModule"
)

type RolePermissions struct {
	View    bool `json:"view,omitempty"`
	Send    bool `json:"send,omitempty"`
	Consume bool `json:"consume,omitempty"`
	Manage  bool `json:"manage,omitempty"`
}

type QueueConfig struct {
	RoutingType string `json:"routingType"`
	Address     string `json:"address,omitempty"`
}

type AddressConfiguration struct {
	RoutingTypes string                  `json:"routingTypes,omitempty"`
	QueueConfigs map[string]*QueueConfig `json:"queueConfigs,omitempty"`
}

func (a *AddressConfiguration) EnsureQueueConfig(name string) *QueueConfig {
	if a.QueueConfigs == nil {
		a.QueueConfigs = map[string]*QueueConfig{}
	}
	if a.QueueConfigs[name] == nil {
		a.QueueConfigs[name] = &QueueConfig{}
	}
	return a.QueueConfigs[name]
}

type CapabilitiesJSON struct {
	AddressConfigurations map[string]*AddressConfiguration       `json:"addressConfigurations,omitempty"`
	SecurityRoles         map[string]map[string]*RolePermissions `json:"securityRoles,omitempty"`
}

func (c *CapabilitiesJSON) EnsureAddressConfig(addressKey string) *AddressConfiguration {
	if c.AddressConfigurations[addressKey] == nil {
		c.AddressConfigurations[addressKey] = &AddressConfiguration{}
	}
	return c.AddressConfigurations[addressKey]
}

func (c *CapabilitiesJSON) EnsureSecurityRole(addressKey, roleKey string) *RolePermissions {
	if c.SecurityRoles[addressKey] == nil {
		c.SecurityRoles[addressKey] = map[string]*RolePermissions{}
	}
	if c.SecurityRoles[addressKey][roleKey] == nil {
		c.SecurityRoles[addressKey][roleKey] = &RolePermissions{}
	}
	return c.SecurityRoles[addressKey][roleKey]
}

type AcceptorParams struct {
	SecurityDomain string `json:"securityDomain"`
	Host           string `json:"host"`
	Port           int32  `json:"port"`
	SslEnabled     bool   `json:"sslEnabled"`
	NeedClientAuth bool   `json:"needClientAuth"`
	SaslMechanisms string `json:"saslMechanisms"`
	KeyStoreType   string `json:"keyStoreType"`
	KeyStorePath   string `json:"keyStorePath"`
	TrustStoreType string `json:"trustStoreType"`
	TrustStorePath string `json:"trustStorePath"`
}

type AcceptorConfiguration struct {
	FactoryClassName string         `json:"factoryClassName"`
	Params           AcceptorParams `json:"params"`
}

type JaasModuleParams struct {
	TextFileDNRole string `json:"org.apache.activemq.jaas.textfiledn.role"`
	TextFileDNUser string `json:"org.apache.activemq.jaas.textfiledn.user"`
	BaseDir        string `json:"baseDir"`
}

type JaasLoginModule struct {
	LoginModuleClass string           `json:"loginModuleClass"`
	ControlFlag      string           `json:"controlFlag"`
	Params           JaasModuleParams `json:"params"`
}

type JaasModules struct {
	Cert JaasLoginModule `json:"cert"`
}

type JaasRealmConfig struct {
	Modules JaasModules `json:"modules"`
}

type AcceptorJSON struct {
	AcceptorConfigurations map[string]*AcceptorConfiguration `json:"acceptorConfigurations"`
	JaasConfigs            map[string]*JaasRealmConfig       `json:"jaasConfigs"`
}

type restrictedConfig struct {
	Name                    string `json:"name"`
	CriticalAnalyzer        bool   `json:"criticalAnalyzer"`
	LiteralMatchMarkers     string `json:"literalMatchMarkers"`
	AuthenticationCacheSize int    `json:"authenticationCacheSize"`
	MessageCounterEnabled   bool   `json:"messageCounterEnabled"`
	JournalDirectory        string `json:"journalDirectory"`
	BindingsDirectory       string `json:"bindingsDirectory"`
	LargeMessagesDirectory  string `json:"largeMessagesDirectory"`
	PagingDirectory         string `json:"pagingDirectory"`
}

func RestrictedConfigData(brokerName string) ([]byte, error) {
	return json.Marshal(restrictedConfig{
		Name:                   brokerName,
		LiteralMatchMarkers:    "()",
		JournalDirectory:       "/app/data",
		BindingsDirectory:      "/app/data/bindings",
		LargeMessagesDirectory: "/app/data/largemessages",
		PagingDirectory:        "/app/data/paging",
	})
}

type rbacConfig struct {
	SecurityRoles map[string]map[string]map[string]bool `json:"securityRoles"`
}

func RBACConfigData() ([]byte, error) {
	return json.Marshal(rbacConfig{
		SecurityRoles: map[string]map[string]map[string]bool{
			"mops.broker.getStatus":                    {"status": {"view": true}},
			"mops.mbeanserver.queryMBeans":             {"metrics": {"view": true}},
			"mops.broker":                              {"metrics": {"view": true}},
			"mops.broker.getTotalMessageCount":         {"metrics": {"view": true}},
			"mops.broker.getTotalMessagesAcknowledged": {"metrics": {"view": true}},
			"mops.broker.getTotalMessagesAdded":        {"metrics": {"view": true}},
		},
	})
}
