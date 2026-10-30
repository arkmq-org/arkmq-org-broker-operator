package artemis

import (
	"fmt"
	"testing"

	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/jolokia"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
)

func TestGetStatus(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	j := jolokia.NewMockIJolokia(ctrl)

	artemis := createMockArtemis(j)

	expectedStatus := "{\"properties\":{\"a_status.properties\": { \"cr:alder32\": \"3d8706a6\"}}}"
	j.
		EXPECT().
		Read(gomock.Eq("org.apache.activemq.artemis:broker=\"someBroker\"/Status")).
		DoAndReturn(func(_ string) (*jolokia.ResponseData, error) {
			return &jolokia.ResponseData{
				Status:    200,
				Value:     expectedStatus,
				ErrorType: "",
				Error:     "",
			}, nil
		}).
		AnyTimes()
	data, err := artemis.GetStatus()

	assert.Equal(t, expectedStatus, data)
	assert.Nil(t, err)
}

func TestGetStatusWithError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	j := jolokia.NewMockIJolokia(ctrl)

	artemis := createMockArtemis(j)

	j.
		EXPECT().
		Read(gomock.Eq("org.apache.activemq.artemis:broker=\"someBroker\"/Status")).
		DoAndReturn(func(_ string) (*jolokia.ResponseData, error) {
			return &jolokia.ResponseData{
				Status:    404,
				Value:     "",
				ErrorType: "javax.management.AttributeNotFoundException",
				Error:     "javax.management.AttributeNotFoundException : No such attribute: Status",
			}, fmt.Errorf("javax.management.AttributeNotFoundException")
		}).
		AnyTimes()
	data, err := artemis.GetStatus()

	assert.Empty(t, data)
	assert.Error(t, err)
}

func TestGetStatusWithErrorStatusOnly(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	j := jolokia.NewMockIJolokia(ctrl)

	artemis := createMockArtemis(j)

	j.
		EXPECT().
		Read(gomock.Eq("org.apache.activemq.artemis:broker=\"someBroker\"/Status")).
		DoAndReturn(func(_ string) (*jolokia.ResponseData, error) {
			return &jolokia.ResponseData{
				Status:    404,
				Value:     "",
				ErrorType: "javax.management.AttributeNotFoundException",
				Error:     "javax.management.AttributeNotFoundException : No such attribute: Status",
			}, nil
		}).
		AnyTimes()
	data, err := artemis.GetStatus()

	assert.Empty(t, data)
	assert.Error(t, err)
}

func TestGetStatusWithNilStatus(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	j := jolokia.NewMockIJolokia(ctrl)

	artemis := createMockArtemis(j)

	j.
		EXPECT().
		Read(gomock.Eq("org.apache.activemq.artemis:broker=\"someBroker\"/Status")).
		DoAndReturn(func(_ string) (*jolokia.ResponseData, error) {
			return nil, nil
		}).
		AnyTimes()
	data, err := artemis.GetStatus()

	assert.Empty(t, data)
	assert.Nil(t, err)
}

func TestParseMBeanKey(t *testing.T) {
	addr, queue, rt := parseMBeanKey(
		`org.apache.activemq.artemis:address="orders",broker="broker1",component=addresses,queue="client.orders",routing-type="multicast",subcomponent=queues`)
	assert.Equal(t, "orders", addr)
	assert.Equal(t, "client.orders", queue)
	assert.Equal(t, "multicast", rt)
}

func TestParseMBeanKeyWithCommaInAddress(t *testing.T) {
	addr, queue, rt := parseMBeanKey(
		`org.apache.activemq.artemis:address="topic.a,b",broker="broker1",component=addresses,queue="sub.topic.a,b",routing-type="anycast",subcomponent=queues`)
	assert.Equal(t, "topic.a,b", addr)
	assert.Equal(t, "sub.topic.a,b", queue)
	assert.Equal(t, "anycast", rt)
}

func TestSplitMBeanProperties(t *testing.T) {
	parts := splitMBeanProperties(`address="a,b",broker="br",queue="q"`)
	assert.Equal(t, []string{`address="a,b"`, `broker="br"`, `queue="q"`}, parts)
}

func TestGetAllQueueMessageCounts(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	j := jolokia.NewMockIJolokia(ctrl)
	artemis := createMockArtemis(j)

	j.EXPECT().
		ReadMap(gomock.Eq(
			`org.apache.activemq.artemis:broker="someBroker",component=addresses,address=*,subcomponent=queues,routing-type=*,queue=*/MessageCount`)).
		Return(map[string]interface{}{
			`org.apache.activemq.artemis:address="orders",broker="someBroker",component=addresses,queue="client.orders",routing-type="multicast",subcomponent=queues`: map[string]interface{}{"MessageCount": float64(5)},
			`org.apache.activemq.artemis:address="prices",broker="someBroker",component=addresses,queue="prices",routing-type="anycast",subcomponent=queues`:          map[string]interface{}{"MessageCount": float64(0)},
		}, nil)

	counts, err := artemis.GetAllQueueMessageCounts()
	assert.NoError(t, err)
	assert.Len(t, counts, 2)

	byQueue := make(map[string]QueueMessageCount, len(counts))
	for _, qc := range counts {
		byQueue[qc.Queue] = qc
	}

	assert.Equal(t, QueueMessageCount{Address: "orders", Queue: "client.orders", RoutingType: "multicast", Count: 5}, byQueue["client.orders"])
	assert.Equal(t, QueueMessageCount{Address: "prices", Queue: "prices", RoutingType: "anycast", Count: 0}, byQueue["prices"])
}

func TestGetAllQueueMessageCountsWithDrainBrokerResponse(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	j := jolokia.NewMockIJolokia(ctrl)
	artemis := Artemis{
		ip:          "0.0.0.0",
		jolokiaPort: "8161",
		name:        "brokerservice-drain482a",
		jolokia:     j,
	}

	j.EXPECT().
		ReadMap(gomock.Eq(
			`org.apache.activemq.artemis:broker="brokerservice-drain482a",component=addresses,address=*,subcomponent=queues,routing-type=*,queue=*/MessageCount`)).
		Return(map[string]interface{}{
			`org.apache.activemq.artemis:address="orders",broker="brokerservice-drain482a",component=addresses,queue="drain-client.orders",routing-type="multicast",subcomponent=queues`: map[string]interface{}{"MessageCount": float64(1)},
		}, nil)

	counts, err := artemis.GetAllQueueMessageCounts()
	assert.NoError(t, err)
	assert.Len(t, counts, 1)
	assert.Equal(t, QueueMessageCount{Address: "orders", Queue: "drain-client.orders", RoutingType: "multicast", Count: 1}, counts[0])
}

func TestGetAllQueueMessageCountsError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	j := jolokia.NewMockIJolokia(ctrl)
	artemis := createMockArtemis(j)

	j.EXPECT().
		ReadMap(gomock.Any()).
		Return(nil, fmt.Errorf("connection refused"))

	counts, err := artemis.GetAllQueueMessageCounts()
	assert.Nil(t, counts)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "connection refused")
}

func TestGetAllQueueMessageCountsEmptyMap(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	j := jolokia.NewMockIJolokia(ctrl)
	artemis := createMockArtemis(j)

	j.EXPECT().
		ReadMap(gomock.Any()).
		Return(map[string]interface{}{}, nil)

	counts, err := artemis.GetAllQueueMessageCounts()
	assert.NoError(t, err)
	assert.Empty(t, counts)
}

func TestGetAllQueueMessageCountsStringValue(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	j := jolokia.NewMockIJolokia(ctrl)
	artemis := createMockArtemis(j)

	j.EXPECT().
		ReadMap(gomock.Any()).
		Return(map[string]interface{}{
			`org.apache.activemq.artemis:address="orders",broker="someBroker",component=addresses,queue="orders",routing-type="anycast",subcomponent=queues`: map[string]interface{}{"MessageCount": "42"},
		}, nil)

	counts, err := artemis.GetAllQueueMessageCounts()
	assert.NoError(t, err)
	assert.Len(t, counts, 1)
	assert.Equal(t, int64(42), counts[0].Count)
}

func createMockArtemis(j jolokia.IJolokia) Artemis {
	return Artemis{
		ip:          "0.0.0.0",
		jolokiaPort: "8161",
		name:        "someBroker",
		jolokia:     j,
	}
}
