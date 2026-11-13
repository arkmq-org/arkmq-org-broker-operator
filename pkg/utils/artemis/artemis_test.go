package artemis

import (
	"encoding/json"
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

func TestQuoteObjectNameValue(t *testing.T) {
	assert.Equal(t, `"orders"`, QuoteObjectNameValue("orders"))
	assert.Equal(t, `"mytopic.\*"`, QuoteObjectNameValue("mytopic.*"))
	assert.Equal(t, `"a\"b\\c\?"`, QuoteObjectNameValue(`a"b\c?`))
	assert.Equal(t, `"a,b=c"`, QuoteObjectNameValue("a,b=c"))
}

func readMBeanOf(t *testing.T, body string) string {
	req := map[string]string{}
	assert.NoError(t, json.Unmarshal([]byte(body), &req))
	assert.Equal(t, "read", req["type"])
	assert.Equal(t, "MessageCount", req["attribute"])
	return req["mbean"]
}

func TestGetQueueMessageCount(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	j := jolokia.NewMockIJolokia(ctrl)
	artemis := createMockArtemis(j)

	j.EXPECT().Exec(gomock.Any(), gomock.Any()).DoAndReturn(func(_ string, body string) (*jolokia.ResponseData, error) {
		assert.Equal(t,
			`org.apache.activemq.artemis:broker="someBroker",component=addresses,address="mytopic.\*",subcomponent=queues,routing-type="multicast",queue="sub.\*"`,
			readMBeanOf(t, body))
		return &jolokia.ResponseData{Status: 200, Value: "1e+06"}, nil
	})

	count, err := artemis.GetQueueMessageCount("mytopic.*", "sub.*", "MULTICAST")
	assert.NoError(t, err)
	assert.Equal(t, int64(1000000), count)
}

func TestGetQueueMessageCountMissingQueue(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	j := jolokia.NewMockIJolokia(ctrl)
	artemis := createMockArtemis(j)

	j.EXPECT().Exec(gomock.Any(), gomock.Any()).Return(
		&jolokia.ResponseData{Status: 404, ErrorType: "javax.management.InstanceNotFoundException"},
		fmt.Errorf("Error response code 404"))

	_, err := artemis.GetQueueMessageCount("orders", "orders", "anycast")
	assert.Error(t, err, "a queue that is not on the broker must not read as empty")
}

func TestGetQueueMessageCountUnparsable(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	j := jolokia.NewMockIJolokia(ctrl)
	artemis := createMockArtemis(j)

	j.EXPECT().Exec(gomock.Any(), gomock.Any()).Return(&jolokia.ResponseData{Status: 200, Value: ""}, nil)

	_, err := artemis.GetQueueMessageCount("orders", "orders", "anycast")
	assert.Error(t, err)
}

func createMockArtemis(j jolokia.IJolokia) Artemis {
	return Artemis{
		ip:          "0.0.0.0",
		jolokiaPort: "8161",
		name:        "someBroker",
		jolokia:     j,
	}
}
