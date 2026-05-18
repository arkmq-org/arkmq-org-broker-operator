package log

import (
	"bytes"
	"errors"
	"reflect"
	"regexp"
	"testing"

	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/common"
	"github.com/stretchr/testify/assert"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

func TestFilteredLogSink(t *testing.T) {

	bufferWriter := common.BufferWriter{
		Buffer: bytes.NewBuffer(nil),
	}

	opts := zap.Options{
		Development: true,
		DestWriter:  bufferWriter,
	}
	logger := zap.New(zap.UseFlagOptions(&opts))

	filteredLogSink := NewFilteredLogSink(logger.GetSink(), []string{"test"})
	assert.NotNil(t, filteredLogSink)

	filteredLogger := logger.WithSink(filteredLogSink)

	setupLog := filteredLogger.WithName("setup")
	assert.NotEqual(t, reflect.TypeOf(log.NullLogSink{}), reflect.TypeOf(setupLog.GetSink()))

	setupLog.Info("setup-info")
	setupLogInfo, err := regexp.Match("setup-info", bufferWriter.Buffer.Bytes())
	assert.Nil(t, err)
	assert.True(t, setupLogInfo)

	setupLog.Error(errors.New("setup-error"), "setup-error")
	setupLogError, err := regexp.Match("setup-error", bufferWriter.Buffer.Bytes())
	assert.Nil(t, err)
	assert.True(t, setupLogError)

	testLog := filteredLogger.WithName("test")
	assert.Equal(t, reflect.TypeOf(log.NullLogSink{}), reflect.TypeOf(testLog.GetSink()))

	testLog.Info("test-info")
	testLogInfo, err := regexp.Match("test-info", bufferWriter.Buffer.Bytes())
	assert.Nil(t, err)
	assert.False(t, testLogInfo)

	testLog.Error(errors.New("test-error"), "test-error")
	testLogError, err := regexp.Match("test-error", bufferWriter.Buffer.Bytes())
	assert.Nil(t, err)
	assert.False(t, testLogError)
}
