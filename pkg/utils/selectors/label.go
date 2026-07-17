package selectors

const (
	LabelAppKey      = "application"
	LabelResourceKey = "ActiveMQArtemis"
	LabelBrokerKey   = "Broker"
)

type LabelerInterface interface {
	Labels() map[string]string
	Base(baseName string) *LabelerData
	Suffix(labelSuffix string) *LabelerData
	ResourceKey(key string) *LabelerData
	Generate()
}

type LabelerData struct {
	baseName    string
	suffix      string
	resourceKey string
	labels      map[string]string
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

// ResourceKey sets the tracking label key. Defaults to LabelResourceKey ("ActiveMQArtemis")
// when not called. Use LabelBrokerKey ("Broker") for Broker CRs.
func (l *LabelerData) ResourceKey(key string) *LabelerData {
	l.resourceKey = key
	return l
}

func (l *LabelerData) Generate() {
	key := l.resourceKey
	if key == "" {
		key = LabelResourceKey
	}
	l.labels = make(map[string]string)
	l.labels[LabelAppKey] = l.baseName + "-" + l.suffix //"-app"
	l.labels[key] = l.baseName
}

func GetLabels(crName string) map[string]string {
	labelBuilder := LabelerData{}
	labelBuilder.Base(crName).Suffix("app").Generate()
	return labelBuilder.labels
}
