package artemis

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/jolokia"
	rtclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	QUEUE_ALREADY_EXISTS   = "AMQ229019"
	ADDRESS_ALREADY_EXISTS = "AMQ229204"
	QUEUE_NOT_EXISTS       = "AMQ229017"
	UNKNOWN_ERROR          = "AMQ_UNKNOWN"
)

func GetCreationError(jdata *jolokia.ResponseData) string {
	if jdata == nil {
		return UNKNOWN_ERROR
	}
	if strings.Contains(jdata.Error, QUEUE_ALREADY_EXISTS) {
		return QUEUE_ALREADY_EXISTS
	}
	if strings.Contains(jdata.Error, ADDRESS_ALREADY_EXISTS) {
		return ADDRESS_ALREADY_EXISTS
	}
	if strings.Contains(jdata.Error, QUEUE_NOT_EXISTS) {
		return QUEUE_NOT_EXISTS
	}
	return UNKNOWN_ERROR
}

type IArtemis interface {
	NewArtemis(_ip string, _jolokiaPort string, _name string, _userName string, _password string) *Artemis
	Uptime() (*jolokia.ResponseData, error)
	CreateQueue(addressName string, queueName string) (*jolokia.ResponseData, error)
	DeleteQueue(queueName string) (*jolokia.ResponseData, error)
	ListBindingsForAddress(addressName string) (*jolokia.ResponseData, error)
	DeleteAddress(addressName string) (*jolokia.ResponseData, error)
	CreateQueueFromConfig(queueConfig string, ignoreIfExists bool) (jolokia.ResponseData, error)
	UpdateQueue(queueConfig string) (jolokia.ResponseData, error)
}

type Artemis struct {
	ip          string
	jolokiaPort string
	name        string
	jolokia     jolokia.IJolokia
}

func GetArtemisAgentForRestricted(client rtclient.Client, brokerName string, ordinalFqdn string) *Artemis {
	artemis := Artemis{
		ip:          ordinalFqdn,
		jolokiaPort: jolokia.JOLOKIA_AGENT_PORT,
		name:        brokerName,
		jolokia:     jolokia.GetRestrictedJolokia(client, ordinalFqdn, jolokia.JOLOKIA_AGENT_PORT, "/jolokia"),
	}
	return &artemis

}

func GetArtemis(_client rtclient.Client, _ip string, _jolokiaPort string, _name string, _user string, _password string, _protocol string) *Artemis {

	artemis := Artemis{
		ip:          _ip,
		jolokiaPort: _jolokiaPort,
		name:        _name,
		jolokia:     jolokia.GetJolokia(_client, _ip, _jolokiaPort, "/console/jolokia", _user, _password, _protocol),
	}

	return &artemis
}

func GetArtemisWithJolokia(j jolokia.IJolokia, name string) *Artemis {
	artemis := Artemis{
		jolokia: j,
		name:    name,
	}
	return &artemis
}

func (artemis *Artemis) GetJolokia() jolokia.IJolokia {
	return artemis.jolokia
}

func (artemis *Artemis) Uptime() (*jolokia.ResponseData, error) {

	uptimeURL := "org.apache.activemq.artemis:broker=\"" + artemis.name + "\"/Uptime"
	data, err := artemis.jolokia.Read(uptimeURL)

	return data, err
}

func (artemis *Artemis) GetStatus() (string, error) {
	url := "org.apache.activemq.artemis:broker=\"" + artemis.name + "\"/Status"

	resp, err := artemis.jolokia.Read(url)
	if err != nil || resp == nil {
		return "", err
	}
	if resp.Status != 200 {
		return "", fmt.Errorf("unable to retrieve status %v", resp.Error)
	}
	return resp.Value, nil
}

func (artemis *Artemis) CreateQueue(addressName string, queueName string, routingType string) (*jolokia.ResponseData, error) {

	url := "org.apache.activemq.artemis:broker=\"" + artemis.name + "\""
	routingType = strings.ToUpper(routingType)
	parameters := `"` + addressName + `","` + queueName + `",` + `"` + routingType + `"`
	jsonStr := `{ "type":"EXEC","mbean":"` + strings.Replace(url, "\"", "\\\"", -1) + `","operation":"createQueue(java.lang.String,java.lang.String,java.lang.String)","arguments":[` + parameters + `]` + ` }`
	data, err := artemis.jolokia.Exec(url, jsonStr)

	return data, err
}

func (artemis *Artemis) UpdateQueue(queueConfig string) (*jolokia.ResponseData, error) {
	url := "org.apache.activemq.artemis:broker=\"" + artemis.name + "\""
	parameters := queueConfig
	jsonStr := `{ "type":"EXEC","mbean":"` + strings.Replace(url, "\"", "\\\"", -1) + `","operation":"updateQueue(java.lang.String)","arguments":[` + parameters + `]` + ` }`

	data, err := artemis.jolokia.Exec(url, jsonStr)

	return data, err

}

func (artemis *Artemis) CreateQueueFromConfig(queueConfig string, ignoreIfExists bool) (*jolokia.ResponseData, error) {
	var ignoreIfExistsValue string
	if ignoreIfExists {
		ignoreIfExistsValue = "true"
	} else {
		ignoreIfExistsValue = "false"
	}
	url := "org.apache.activemq.artemis:broker=\"" + artemis.name + "\""
	parameters := queueConfig + `,` + ignoreIfExistsValue
	jsonStr := `{ "type":"EXEC","mbean":"` + strings.Replace(url, "\"", "\\\"", -1) + `","operation":"createQueue(java.lang.String,boolean)","arguments":[` + parameters + `]` + ` }`

	data, err := artemis.jolokia.Exec(url, jsonStr)

	return data, err
}

func (artemis *Artemis) CreateAddress(addressName string, routingType string) (*jolokia.ResponseData, error) {

	url := "org.apache.activemq.artemis:broker=\"" + artemis.name + "\""
	routingType = strings.ToUpper(routingType)
	parameters := `"` + addressName + `","` + routingType + `"`
	jsonStr := `{ "type":"EXEC","mbean":"` + strings.Replace(url, "\"", "\\\"", -1) + `","operation":"createAddress(java.lang.String,java.lang.String)","arguments":[` + parameters + `]` + ` }`
	data, err := artemis.jolokia.Exec(url, jsonStr)

	return data, err
}

func (artemis *Artemis) DeleteQueue(queueName string) (*jolokia.ResponseData, error) {

	url := "org.apache.activemq.artemis:broker=\"" + artemis.name + "\""
	parameters := `"` + queueName + `"`
	jsonStr := `{ "type":"EXEC","mbean":"` + strings.Replace(url, "\"", "\\\"", -1) + `","operation":"destroyQueue(java.lang.String)","arguments":[` + parameters + `]` + ` }`
	data, err := artemis.jolokia.Exec(url, jsonStr)

	return data, err
}

func (artemis *Artemis) ListBindingsForAddress(addressName string) (*jolokia.ResponseData, error) {

	url := "org.apache.activemq.artemis:broker=\"" + artemis.name + "\""
	parameters := `"` + addressName + `"`
	jsonStr := `{ "type":"EXEC","mbean":"` + strings.Replace(url, "\"", "\\\"", -1) + `","operation":"listBindingsForAddress(java.lang.String)","arguments":[` + parameters + `]` + ` }`
	data, err := artemis.jolokia.Exec(url, jsonStr)

	return data, err
}

func (artemis *Artemis) DeleteAddress(addressName string) (*jolokia.ResponseData, error) {

	url := "org.apache.activemq.artemis:broker=\"" + artemis.name + "\""
	parameters := `"` + addressName + `"`
	jsonStr := `{ "type":"EXEC","mbean":"` + strings.ReplaceAll(url, "\"", "\\\"") + `","operation":"deleteAddress(java.lang.String)","arguments":[` + parameters + `]` + ` }`
	data, err := artemis.jolokia.Exec(url, jsonStr)

	return data, err
}

func (artemis *Artemis) ForceFailover() (*jolokia.ResponseData, error) {

	url := "org.apache.activemq.artemis:broker=\"" + artemis.name + "\""
	jsonStr := `{ "type":"EXEC","mbean":"` + strings.ReplaceAll(url, "\"", "\\\"") + `","operation":"forceFailover()","arguments":[]` + ` }`
	data, err := artemis.jolokia.Exec(url, jsonStr)

	return data, err
}

func (artemis *Artemis) ScaleDown() (*jolokia.ResponseData, error) {

	url := "org.apache.activemq.artemis:broker=\"" + artemis.name + "\""
	jsonStr := `{ "type":"EXEC","mbean":"` + strings.ReplaceAll(url, "\"", "\\\"") + `","operation":"scaleDown(java.lang.String)","arguments":[null]` + ` }`
	data, err := artemis.jolokia.ExecWithClient(artemis.jolokia.GetClientWithTimeout(time.Second*30), url, jsonStr)
	if err != nil {
		if errors.Is(err, io.EOF) {
			// expected as broker will exit
			return data, nil
		}
	}
	return data, err
}

func (artemis *Artemis) GetTotalMessageCount() (string, error) {
	url := "org.apache.activemq.artemis:broker=\"" + artemis.name + "\"/TotalMessageCount"
	resp, err := artemis.jolokia.Read(url)
	if err != nil || resp == nil {
		return "", err
	}
	if resp.Status != 200 {
		return "", fmt.Errorf("unable to retrieve TotalMessageCount %v", resp.Error)
	}
	return resp.Value, nil
}

// QueueMessageCount holds the message count for a single queue.
type QueueMessageCount struct {
	Address     string
	Queue       string
	RoutingType string
	Count       int64
}

// GetAllQueueMessageCounts reads MessageCount for all queues in a single Jolokia
// pattern read, returning one entry per queue. Callers can filter the result by
// address name instead of issuing per-queue requests.
func (artemis *Artemis) GetAllQueueMessageCounts() ([]QueueMessageCount, error) {
	path := "org.apache.activemq.artemis:broker=\"" + artemis.name + "\"" +
		",component=addresses,address=*" +
		",subcomponent=queues,routing-type=*" +
		",queue=*/MessageCount"
	raw, err := artemis.jolokia.ReadMap(path)
	if err != nil {
		return nil, fmt.Errorf("pattern read for queue MessageCount: %w", err)
	}

	var result []QueueMessageCount
	for mbeanKey, val := range raw {
		addr, queue, rt := parseMBeanKey(mbeanKey)
		if addr == "" {
			continue
		}
		var countVal interface{}
		if attrs, ok := val.(map[string]interface{}); ok {
			countVal = attrs["MessageCount"]
		} else {
			countVal = val
		}
		var count int64
		switch v := countVal.(type) {
		case float64:
			count = int64(v)
		case string:
			if n, err := strconv.ParseInt(v, 10, 64); err == nil {
				count = n
			}
		}
		result = append(result, QueueMessageCount{
			Address:     addr,
			Queue:       queue,
			RoutingType: rt,
			Count:       count,
		})
	}
	return result, nil
}

// parseMBeanKey extracts address, queue, and routing-type from a Jolokia MBean
// object name like:
// "org.apache.activemq.artemis:address=\"orders\",broker=\"brokerservice-drain1420a\",component=addresses,queue=\"drain-client.orders\",routing-type=\"multicast\",subcomponent=queues"
func parseMBeanKey(key string) (address, queue, routingType string) {
	for _, part := range splitMBeanProperties(strings.TrimPrefix(key, "org.apache.activemq.artemis:")) {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}
		val := strings.Trim(kv[1], "\"")
		switch kv[0] {
		case "address":
			address = val
		case "queue":
			queue = val
		case "routing-type":
			routingType = val
		}
	}
	return
}

// splitMBeanProperties splits a JMX ObjectName property list on commas,
// respecting quoted values that may contain commas.
func splitMBeanProperties(s string) []string {
	var parts []string
	start := 0
	inQuote := false
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '"':
			inQuote = !inQuote
		case ',':
			if !inQuote {
				parts = append(parts, s[start:i])
				start = i + 1
			}
		}
	}
	if start < len(s) {
		parts = append(parts, s[start:])
	}
	return parts
}
