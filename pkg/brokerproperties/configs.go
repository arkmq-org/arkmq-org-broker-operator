package brokerproperties

import (
	"bytes"
	"fmt"
	"sort"
)

func SecurityConfigData(mountPathRoot string) []byte {
	buf := NewPropsWithHeader()
	fmt.Fprintf(buf, "login.config.url.1=file:%s/login.config\n", mountPathRoot)
	fmt.Fprintf(buf, "security.provider.13=de.dentrassi.crypto.pem.PemKeyStoreProvider\n")
	fmt.Fprintf(buf, "fips.provider.8=de.dentrassi.crypto.pem.PemKeyStoreProvider\n")
	return buf.Bytes()
}

func LoginConfigData(realm, mountPathRoot, certUsersKey, certRolesKey string) []byte {
	buf := NewBufferWithHeader("//")
	fmt.Fprintf(buf, "%s {\n", realm)
	fmt.Fprintln(buf, "  org.apache.activemq.artemis.spi.core.security.jaas.TextFileCertificateLoginModule required")
	fmt.Fprintln(buf, "   reload=true")
	fmt.Fprintln(buf, "   debug=true")
	fmt.Fprintf(buf, "   org.apache.activemq.jaas.textfiledn.user=%s\n", certUsersKey)
	fmt.Fprintf(buf, "   org.apache.activemq.jaas.textfiledn.role=%s\n", certRolesKey)
	fmt.Fprintf(buf, "   baseDir=\"%v\"\n", mountPathRoot)
	fmt.Fprintln(buf, "  ;")
	fmt.Fprintln(buf, "};")
	return buf.Bytes()
}

func BrokerCertUsersData(operatorCN, operandCN, prometheusCN string) []byte {
	buf := NewPropsWithHeader()
	fmt.Fprintln(buf, "hawtio=/CN = hawtio-online\\.hawtio\\.svc.*/")
	fmt.Fprintf(buf, "operator=/.*%s.*/\n", operatorCN)
	fmt.Fprintf(buf, "probe=/.*%s.*/\n", operandCN)
	if prometheusCN != "" {
		fmt.Fprintf(buf, "prometheus=/.*%s.*/\n", prometheusCN)
	}
	return buf.Bytes()
}

func BrokerCertRolesData() []byte {
	buf := NewPropsWithHeader()
	fmt.Fprintln(buf, "status=operator,probe")
	fmt.Fprintln(buf, "metrics=operator,prometheus")
	fmt.Fprintln(buf, "hawtio=hawtio")
	return buf.Bytes()
}

func JolokiaConfigData(caCertPath, serverCertPath, serverKeyPath string) []byte {
	buf := NewPropsWithHeader()
	fmt.Fprintln(buf, "protocol=https")
	fmt.Fprintln(buf, "authClass=org.apache.activemq.artemis.spi.core.security.jaas.HttpServerAuthenticator")
	fmt.Fprintf(buf, "caCert=%s\n", caCertPath)
	fmt.Fprintf(buf, "serverCert=%s\n", serverCertPath)
	fmt.Fprintf(buf, "serverKey=%s\n", serverKeyPath)
	fmt.Fprintln(buf, "port=8778")
	fmt.Fprintln(buf, "useSslClientAuthentication=true")
	fmt.Fprintln(buf, "disabledServices=org.jolokia.service.history.HistoryMBeanRequestInterceptor")
	fmt.Fprintln(buf, "disableDetectors=true")
	fmt.Fprintln(buf, "debug=false")
	return buf.Bytes()
}

func PemCfgData(alias, certKeyPath, certCrtPath string) []byte {
	buf := NewPropsWithHeader()
	if alias != "" {
		fmt.Fprintf(buf, "alias=%s\n", alias)
	}
	fmt.Fprintf(buf, "source.key=%s\n", certKeyPath)
	fmt.Fprintf(buf, "source.cert=%s\n", certCrtPath)
	return buf.Bytes()
}

func BrokerPrometheusConfigData(pemCfgPath, caTrustStorePath, brokerName string) []byte {
	buf := NewPropsWithHeader()
	writePrometheusSSLHeader(buf, pemCfgPath, caTrustStorePath)
	fmt.Fprintf(buf, "lowercaseOutputName: true\n")
	fmt.Fprintf(buf, "lowercaseOutputLabelNames: true\n")
	fmt.Fprintf(buf, "includeObjectNames: [org.apache.activemq.artemis:broker=\"%s\"]\n", brokerName)
	fmt.Fprintf(buf, "includeObjectNameAttributes:\n")
	fmt.Fprintf(buf, "  'org.apache.activemq.artemis:broker=\"%s\"':\n", brokerName)
	fmt.Fprintf(buf, "    - \"TotalMessageCount\"\n")
	fmt.Fprintf(buf, "    - \"TotalMessagesAdded\"\n")
	fmt.Fprintf(buf, "    - \"TotalMessagesAcknowledged\"\n")
	fmt.Fprintf(buf, "rules:\n")
	fmt.Fprintf(buf, "  - pattern: 'org.apache.activemq.artemis<broker=\"%s\"><>TotalMessageCount'\n", brokerName)
	fmt.Fprintf(buf, "    help: Number of pending messages\n")
	fmt.Fprintf(buf, "    name: artemis_total_pending_message_count\n")
	fmt.Fprintf(buf, "    type: GAUGE\n")
	fmt.Fprintf(buf, "  - pattern: 'org.apache.activemq.artemis<broker=\"%s\"><>TotalMessagesAcknowledged'\n", brokerName)
	fmt.Fprintf(buf, "    help: Number of messages consumed since start\n")
	fmt.Fprintf(buf, "    name: artemis_total_consumed_message_count\n")
	fmt.Fprintf(buf, "    type: COUNTER\n")
	fmt.Fprintf(buf, "  - pattern: 'org.apache.activemq.artemis<broker=\"%s\"><>TotalMessagesAdded'\n", brokerName)
	fmt.Fprintf(buf, "    help: Number of messages produced since start\n")
	fmt.Fprintf(buf, "    name: artemis_total_produced_message_count\n")
	fmt.Fprintf(buf, "    type: COUNTER\n")
	return buf.Bytes()
}

func ServicePrometheusConfigData(pemCfgPath, caTrustStorePath, brokerName string, appQueues map[string]bool) []byte {
	buf := NewPropsWithHeader()
	writePrometheusSSLHeader(buf, pemCfgPath, caTrustStorePath)
	fmt.Fprintf(buf, "attrNameSnakeCase: true\n")
	fmt.Fprintf(buf, "includeObjectNames:\n")
	fmt.Fprintf(buf, "  - \"org.apache.activemq.artemis:broker=*,component=addresses,address=*,subcomponent=queues,routing-type=*,queue=*\"\n")
	if len(appQueues) > 0 {
		fmt.Fprintf(buf, "includeObjectNameAttributes:\n")
		addresses := make([]string, 0, len(appQueues))
		for address := range appQueues {
			addresses = append(addresses, address)
		}
		sort.Strings(addresses)
		for _, address := range addresses {
			fqqn := splitFQQN(address)
			if len(fqqn) > 1 {
				fmt.Fprintf(buf, "  org.apache.activemq.artemis:broker=\"%s\",component=addresses,address=\"%s\",subcomponent=queues,routing-type=\"multicast\",queue=\"%s\":\n",
					brokerName, fqqn[0], fqqn[1])
			} else {
				fmt.Fprintf(buf, "  org.apache.activemq.artemis:broker=\"%s\",component=addresses,address=\"%s\",subcomponent=queues,routing-type=\"anycast\",queue=\"%s\":\n",
					brokerName, address, address)
			}
			fmt.Fprintf(buf, "    - MessageCount\n")
			fmt.Fprintf(buf, "    - ConsumerCount\n")
			fmt.Fprintf(buf, "    - DeliveringCount\n")
			fmt.Fprintf(buf, "    - PersistentSize\n")
		}
	}
	fmt.Fprintf(buf, "rules:\n")
	fmt.Fprintf(buf, `  - pattern: "org.apache.activemq.artemis<broker=\"([^\"]+)\", component=addresses, address=\"([^\"]+)\", subcomponent=queues, routing-type=\"([^\"]+)\", queue=\"([^\"]+)\"><>([^:]+):"`+"\n")
	fmt.Fprintf(buf, "    name: broker_queue_$5\n")
	fmt.Fprintf(buf, "    help: $5\n")
	fmt.Fprintf(buf, "    attrNameSnakeCase: true\n")
	fmt.Fprintf(buf, "    type: GAUGE\n")
	fmt.Fprintf(buf, "    labels:\n")
	fmt.Fprintf(buf, "      broker: \"$1\"\n")
	fmt.Fprintf(buf, "      address: \"$2\"\n")
	fmt.Fprintf(buf, "      routing_type: \"$3\"\n")
	fmt.Fprintf(buf, "      queue: \"$4\"\n")
	return buf.Bytes()
}

func writePrometheusSSLHeader(buf *bytes.Buffer, pemCfgPath, caTrustStorePath string) {
	fmt.Fprintf(buf, "httpServer:\n")
	fmt.Fprintf(buf, "  authentication:\n")
	fmt.Fprintf(buf, "    plugin:\n")
	fmt.Fprintf(buf, "      class: org.apache.activemq.artemis.spi.core.security.jaas.HttpServerAuthenticator\n")
	fmt.Fprintf(buf, "      subjectAttributeName: org.jolokia.jaasSubject\n")
	fmt.Fprintf(buf, "  ssl:\n")
	fmt.Fprintf(buf, "    mutualTLS: true\n")
	fmt.Fprintf(buf, "    keyStore:\n")
	fmt.Fprintf(buf, "      filename: %s\n", pemCfgPath)
	fmt.Fprintf(buf, "      type: PEMCFG\n")
	fmt.Fprintf(buf, "    trustStore:\n")
	fmt.Fprintf(buf, "      filename: %s\n", caTrustStorePath)
	fmt.Fprintf(buf, "      type: PEMCA\n")
	fmt.Fprintf(buf, "    certificate:\n")
	fmt.Fprintf(buf, "      alias: alias\n")
}

func splitFQQN(address string) []string {
	for i := 0; i < len(address)-1; i++ {
		if address[i] == ':' && address[i+1] == ':' {
			return []string{address[:i], address[i+2:]}
		}
	}
	return []string{address}
}
