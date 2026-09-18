package brokerproperties

import (
	"encoding/json"
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

type CapabilitiesJSON struct {
	AddressConfigurations map[string]*AddressConfiguration       `json:"addressConfigurations,omitempty"`
	SecurityRoles         map[string]map[string]*RolePermissions `json:"securityRoles,omitempty"`
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
