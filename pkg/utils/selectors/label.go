package selectors

import (
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
)

const (
	LabelAppKey             = "application"
	LabelActiveMQArtemisKey = "ActiveMQArtemis"
	LabelBrokerKey          = "broker"
	// LabelAppValueArtemis is the fixed application label value for ActiveMQArtemis / BrokerCluster managed resources.
	LabelAppValueArtemis = "Artemis"
	// LabelAppValueBroker is the fixed application label value for Broker managed resources.
	LabelAppValueBroker = "Broker"
)

type LabelerInterface interface {
	Labels() map[string]string
	Base(baseName string) *LabelerData
	Suffix(labelSuffix string) *LabelerData
	Generate()
}

type LabelerData struct {
	baseName    string
	suffix      string
	resourceKey string
	appValue    string
	labels      map[string]string
}

func NewBrokerLabeler() *LabelerData {
	return &LabelerData{resourceKey: LabelBrokerKey, appValue: LabelAppValueBroker}
}

func NewActiveMQArtemisLabeler() *LabelerData {
	return &LabelerData{resourceKey: LabelActiveMQArtemisKey, appValue: LabelAppValueArtemis}
}

func (l *LabelerData) Labels() map[string]string {
	return l.labels
}

func (l *LabelerData) Base(name string) *LabelerData {
	l.baseName = name
	return l
}

func (l *LabelerData) Suffix(labelSuffix string) *LabelerData {
	l.suffix = labelSuffix
	return l
}

func (l *LabelerData) Generate() {
	l.labels = make(map[string]string)
	l.labels[LabelAppKey] = l.appValue
	l.labels[l.resourceKey] = l.baseName
}

// OperatorPodLabelSelector returns the label selector used to limit the
// controller-runtime Pod watch cache to operator-managed broker pods.
// It matches application in (Artemis, Broker).
func OperatorPodLabelSelector() labels.Selector {
	req, err := labels.NewRequirement(LabelAppKey, selection.In, []string{LabelAppValueArtemis, LabelAppValueBroker})
	if err != nil {
		// Only fails if the key/operator/values are invalid; constants above are valid.
		panic(err)
	}
	return labels.NewSelector().Add(*req)
}

func GetLabels(crName string) map[string]string {
	labelBuilder := NewActiveMQArtemisLabeler()
	labelBuilder.Base(crName).Suffix("app").Generate()
	return labelBuilder.Labels()
}
