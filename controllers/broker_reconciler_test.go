package controllers

import (
	"encoding/json"
	"fmt"
	"reflect"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/RHsyseng/operator-utils/pkg/olm"
	"github.com/RHsyseng/operator-utils/pkg/resource/compare"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta2"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/common"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	pointer "k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("HexShaHashOfMap", func() {
	Context("when hashing maps", func() {
		It("should return consistent hash for nil", func() {
			nilOne := hexShaHashOfMap(nil)
			nilTwo := hexShaHashOfMap(nil)
			Expect(nilOne).To(Equal(nilTwo))
		})

		It("should return different hash when map is modified", func() {
			props := []string{"a=a", "b=b"}
			propsOriginal := hexShaHashOfMap(props)

			// modify
			props = append(props, "c=c")
			propsModified := hexShaHashOfMap(props)

			Expect(propsOriginal).NotTo(Equal(propsModified))
		})

		It("should return same hash when reverted to original", func() {
			props := []string{"a=a", "b=b"}
			propsOriginal := hexShaHashOfMap(props)

			// modify
			props = append(props, "c=c")

			// revert, drop the last entry b/c they are ordered
			props = props[:2]

			Expect(propsOriginal).To(Equal(hexShaHashOfMap(props)))
		})

		It("should return different hash when further modified", func() {
			props := []string{"a=a", "b=b"}
			propsOriginal := hexShaHashOfMap(props)

			// modify further, drop first entry
			props = props[:1]

			Expect(propsOriginal).NotTo(Equal(hexShaHashOfMap(props)))
		})
	})
})

var _ = Describe("MapComparatorForStatefulSet", func() {
	Context("when comparing StatefulSets", func() {
		It("should detect additions and updates correctly", func() {
			ss := &appsv1.StatefulSet{
				TypeMeta: metav1.TypeMeta{Kind: "StatefulSet", APIVersion: "apps/v1beta1"},
				ObjectMeta: metav1.ObjectMeta{
					Name:                       "ss",
					GenerateName:               "",
					Namespace:                  "a",
					SelfLink:                   "",
					UID:                        "",
					ResourceVersion:            "1",
					Generation:                 0,
					CreationTimestamp:          metav1.Time{},
					DeletionTimestamp:          &metav1.Time{},
					DeletionGracePeriodSeconds: new(int64),
					Labels:                     nil,
					Annotations:                nil,
					OwnerReferences:            []metav1.OwnerReference{},
					Finalizers:                 []string{},
					ManagedFields:              []metav1.ManagedFieldsEntry{},
				},
				Spec:   appsv1.StatefulSetSpec{},
				Status: appsv1.StatefulSetStatus{},
			}

			ssMod := &appsv1.StatefulSet{
				TypeMeta: metav1.TypeMeta{Kind: "StatefulSet", APIVersion: "apps/v1"},
				ObjectMeta: metav1.ObjectMeta{
					Name:                       "ss",
					GenerateName:               "",
					Namespace:                  "a",
					SelfLink:                   "",
					UID:                        "",
					ResourceVersion:            "1",
					Generation:                 0,
					CreationTimestamp:          metav1.Time{},
					DeletionTimestamp:          &metav1.Time{},
					DeletionGracePeriodSeconds: new(int64),
					Labels:                     nil,
					Annotations:                nil,
					OwnerReferences:            []metav1.OwnerReference{},
					Finalizers:                 []string{},
					ManagedFields:              []metav1.ManagedFieldsEntry{},
				},
				Spec: appsv1.StatefulSetSpec{
					Replicas:             new(int32),
					Selector:             &metav1.LabelSelector{},
					Template:             v1.PodTemplateSpec{},
					VolumeClaimTemplates: []v1.PersistentVolumeClaim{},
					ServiceName:          "ssMod",
					PodManagementPolicy:  "",
					UpdateStrategy:       appsv1.StatefulSetUpdateStrategy{},
					RevisionHistoryLimit: new(int32),
					MinReadySeconds:      0,
				},
				Status: appsv1.StatefulSetStatus{},
			}

			ss0 := &appsv1.StatefulSet{
				TypeMeta: metav1.TypeMeta{Kind: "StatefulSet", APIVersion: "apps/v1"},
				ObjectMeta: metav1.ObjectMeta{
					Name:                       "ss0",
					GenerateName:               "",
					Namespace:                  "a",
					SelfLink:                   "",
					UID:                        "",
					ResourceVersion:            "1",
					Generation:                 0,
					CreationTimestamp:          metav1.Time{},
					DeletionTimestamp:          &metav1.Time{},
					DeletionGracePeriodSeconds: new(int64),
					Labels:                     nil,
					Annotations:                nil,
					OwnerReferences:            []metav1.OwnerReference{},
					Finalizers:                 []string{},
					ManagedFields:              []metav1.ManagedFieldsEntry{},
				},
				Spec:   appsv1.StatefulSetSpec{},
				Status: appsv1.StatefulSetStatus{},
			}

			var requestedResources []client.Object
			requestedResources = append(requestedResources, ss0)
			requestedResources = append(requestedResources, ssMod)

			deployed := make(map[reflect.Type][]client.Object)
			var deployedSets []client.Object
			deployedSets = append(deployedSets, ss)

			ssType := reflect.ValueOf(ss).Elem().Type()
			deployed[ssType] = deployedSets

			requested := compare.NewMapBuilder().Add(requestedResources...).ResourceMap()
			comparator := compare.MapComparator{
				Comparator: compare.SimpleComparator(),
			}

			reconciler := &BrokerReconcilerImpl{
				log:            ctrl.Log.WithName("test"),
				customResource: nil,
			}

			comparator.Comparator.SetComparator(reflect.TypeOf(appsv1.StatefulSet{}), reconciler.CompareMetaAndSpec)
			deltas := comparator.Compare(deployed, requested)

			Expect(deltas[ssType].Added).To(HaveLen(1), "expect new addition to appear")
			Expect(deltas[ssType].Updated).To(HaveLen(1), "expect difference on ss to be respected as an update")
		})
	})
})

var _ = Describe("ComparatorMetaAndSpec", func() {
	Context("when comparing StatefulSet meta and spec", func() {
		It("should return true for same instance", func() {
			reconciler := &BrokerReconcilerImpl{
				log:            ctrl.Log.WithName("test"),
				customResource: nil,
			}

			ss0 := &appsv1.StatefulSet{}
			equal := reconciler.CompareMetaAndSpec(ss0, ss0)

			Expect(equal).To(BeTrue())
		})

		It("should return false for different annotations", func() {
			reconciler := &BrokerReconcilerImpl{
				log:            ctrl.Log.WithName("test"),
				customResource: nil,
			}

			ss0 := &appsv1.StatefulSet{}
			ss1 := &appsv1.StatefulSet{}
			ss1.Annotations = map[string]string{"A": "B"}
			equal := reconciler.CompareMetaAndSpec(ss0, ss1)

			Expect(equal).To(BeFalse())
		})
	})
})

var _ = Describe("GetSingleStatefulSetStatus", func() {
	Context("when getting StatefulSet status", func() {
		It("should return correct ready status for running pod", func() {
			var expected int32 = int32(1)
			ss := &appsv1.StatefulSet{}
			ss.ObjectMeta.Name = "joe"
			ss.Spec.Replicas = &expected
			ss.Status.Replicas = 1
			ss.Status.ReadyReplicas = 1

			cr := &v1beta2.BrokerCluster{}
			statusRunning := common.GetSingleStatefulSetStatus(ss, cr)

			Expect(statusRunning.Ready).To(HaveLen(1))
			Expect(statusRunning.Ready[0]).To(Equal("joe-0"))
		})

		It("should return stopped status when replicas are 0", func() {
			var expected int32 = int32(1)
			ss := &appsv1.StatefulSet{}
			ss.ObjectMeta.Name = "joe"
			ss.Spec.Replicas = &expected
			ss.Status.Replicas = 0
			ss.Status.ReadyReplicas = 0

			cr := &v1beta2.BrokerCluster{}
			statusRunning := common.GetSingleStatefulSetStatus(ss, cr)

			Expect(statusRunning.Stopped).To(HaveLen(1))
			Expect(statusRunning.Stopped[0]).To(Equal("joe"))
		})

		It("should return correct status with multiple replicas", func() {
			var expectedTwo int32 = int32(2)
			ss := &appsv1.StatefulSet{}
			ss.ObjectMeta.Name = "joe"
			ss.Spec.Replicas = &expectedTwo
			ss.Status.Replicas = 2
			ss.Status.ReadyReplicas = 1

			cr := &v1beta2.BrokerCluster{}
			statusRunning := common.GetSingleStatefulSetStatus(ss, cr)

			Expect(statusRunning.Ready).To(HaveLen(1))
			Expect(statusRunning.Ready[0]).To(Equal("joe-0"))
			Expect(statusRunning.Starting).To(HaveLen(1))
			Expect(statusRunning.Starting[0]).To(Equal("joe-1"))
			Expect(cr.Status.DeploymentPlanSize).To(Equal(int32(2)))
		})
	})
})

var _ = Describe("GetConfigAppliedConfigMapName", func() {
	Context("when getting config map name", func() {
		It("should return correct namespace and name", func() {
			cr := v1beta2.Broker{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "test-ns",
					Name:      "test",
				},
			}
			name := getPropertiesResourceNsNameForBroker(&cr)

			Expect(name.Namespace).To(Equal("test-ns"))
			Expect(name.Name).To(Equal("test-props"))
		})
	})
})

var _ = Describe("ExtractSha", func() {
	Context("when extracting SHA from status", func() {
		It("should extract SHA successfully", func() {
			json := `{"configuration": {"properties": {"a_status.properties": {"alder32": "123456"}}}}`
			status, err := unmarshallStatus(json)
			Expect(err).NotTo(HaveOccurred())

			sha, err := extractShaFromBroker(status, "a_status.properties")
			Expect(sha).To(Equal("123456"))
			Expect(err).NotTo(HaveOccurred())
		})

		It("should return empty SHA when not present", func() {
			json := `{"configuration": {"properties": {"a_status.properties": {}}}}`
			status, err := unmarshallStatus(json)
			Expect(err).NotTo(HaveOccurred())

			sha, err := extractShaFromBroker(status, "a_status.properties")
			Expect(sha).To(BeEmpty())
			Expect(err).NotTo(HaveOccurred())
		})

		It("should return error for invalid JSON", func() {
			json := `you shall fail`
			status, err := unmarshallStatus(json)
			Expect(err).To(HaveOccurred())

			sha, err := extractShaFromBroker(status, "a_status.properties")
			Expect(sha).To(BeEmpty())
			Expect(err).To(HaveOccurred())
		})
	})
})

var _ = Describe("Alder32 Hash Generation", func() {
	Context("when generating Alder32 hash", func() {
		It("should generate correct hash for user properties", func() {
			userProps := `admin=admin
			tom=tom
			peter=peter`

			res := alder32FromData([]byte(userProps))
			Expect(res).To(ContainSubstring("2905476010"))
		})

		It("should handle properties with spaces", func() {
			userProps := `admin = joe`

			res := alder32FromData([]byte(userProps))
			Expect(res).To(ContainSubstring("295568261"))
		})

		It("should handle properties with empty lines", func() {
			userProps := `
			admin=admin
			tom=tom
			peter=peter`

			res := alder32FromData([]byte(userProps))
			Expect(res).To(ContainSubstring("2905476010"))
		})

		It("should handle properties with escaped spaces", func() {
			userProps := `addressesSettings.#.redeliveryMultiplier=2.3
	addressesSettings.#.redeliveryCollisionAvoidanceFactor=1.2
	addressesSettings.Some\ value\ with\ space.redeliveryCollisionAvoidanceFactor=1.2`

			res := alder32FromData([]byte(userProps))
			Expect(res).To(Equal("2211202255"))
		})

		It("should handle properties with dots", func() {
			userProps := `addressSettings.#.redeliveryMultiplier=5
    addressSettings.\"news.#\".redeliveryMultiplier=2
    addressSettings.\"order.#\".redeliveryMultiplier=3`

			res := alder32FromData([]byte(userProps))
			Expect(res).To(Equal("3264295767"))
		})

		It("should generate correct hash for broker properties", func() {
			propsString := "# generated by crd\n#\nconnectionRouters.autoShard.keyType=CLIENT_ID\nconnectionRouters.autoShard.localTargetFilter=NULL|${STATEFUL_SET_ORDINAL}|-${STATEFUL_SET_ORDINAL}\nconnectionRouters.autoShard.policyConfiguration=CONSISTENT_HASH_MODULO\nconnectionRouters.autoShard.policyConfiguration.properties.MODULO=2\nacceptorConfigurations.tcp.params.router=autoShard\naddressesSettings.\"LB.#\".defaultAddressRoutingType=ANYCAST\n"

			res := alder32FromData([]byte(propsString))
			Expect(res).To(ContainSubstring("1897435425"))
		})

		It("should strip comments from roles properties", func() {
			propsStringWithLeadingWhiteSpaceBeforeComment := `
	# rbac
    control-plane=control-plane,control-plane-0,control-plane-1
    consumers=c1,c2,c3,c4
    producers=p
	! exclimation mark comment to strip with leading ws
! as start of line to strip
     # partitioned consumer roles for connectionRouter
shard-consumers-broker-0=c1,c2
shard-consumers-broker-1=c3,c4

     		# should resolve to NULL in absence of this
shard-control-plane=control-plane,control-plane-0,control-plane-1
shard-producers=p`

			propsStringCommentsStripped := `
control-plane=control-plane,control-plane-0,control-plane-1
consumers=c1,c2,c3,c4
producers=p
shard-consumers-broker-0=c1,c2
shard-consumers-broker-1=c3,c4
shard-control-plane=control-plane,control-plane-0,control-plane-1
shard-producers=p`

			res := alder32FromData([]byte(propsStringWithLeadingWhiteSpaceBeforeComment))
			expected := alder32FromData([]byte(propsStringCommentsStripped))

			Expect(res).To(Equal(expected))
		})

		It("should handle properties with form feed characters", func() {
			propsStringWithLeadingWhiteSpaceBeforeComment := "\n\t\f# with form feed\nproducers=p"
			propsStringCommentsStripped := "producers=p"

			res := alder32FromData([]byte(propsStringWithLeadingWhiteSpaceBeforeComment))
			expected := alder32FromData([]byte(propsStringCommentsStripped))

			Expect(res).To(Equal(expected))
		})
	})
})

var _ = Describe("ExtractErrors", func() {
	Context("when extracting errors from status", func() {
		It("should extract SHA from valid JSON", func() {
			json := "{\"configuration\":{\"properties\":{\"broker.properties\":{\"alder32\":\"1\"},\"system\":{\"alder32\":\"1\"}}},\"server\":{\"jaas\":{\"properties\":{\"artemis-users.properties\":{\"reloadTime\":\"1669744377685\",\"Alder32\":\"955331033\"},\"artemis-roles.properties\":{\"reloadTime\":\"1669744377685\",\"Alder32\":\"701302135\"}}},\"state\":\"STARTED\",\"version\":\"2.27.0\",\"nodeId\":\"a644c0c6-700e-11ed-9d4f-0a580ad90188\",\"identity\":null,\"uptime\":\"33.176 seconds\"}}"
			status, err := unmarshallStatus(json)
			Expect(err).NotTo(HaveOccurred())

			sha, err := extractShaFromBroker(status, "broker.properties")
			Expect(sha).To(Equal("1"))
			Expect(err).NotTo(HaveOccurred())
		})

		It("should extract and marshall apply errors", func() {
			json := `{"configuration": {
			"properties": {
				"a_status.properties": {
					"alder32": "110827957",
					"cr:alder32": "1f4004ae",
					"errors": []
				},
				"broker.properties": {
					"alder32": "524289198",
					"errors": [
						{
							"value": "notValid=bla",
							"reason": "No accessor method descriptor for: notValid on: class org.apache.activemq.artemis.core.config.impl.FileConfiguration"
						}
					]
				}
			}
		}
	}`
			status, err := unmarshallStatus(json)
			Expect(err).NotTo(HaveOccurred())

			appplyErrors := status.BrokerConfigStatus.PropertiesStatus["broker.properties"].ApplyErrors
			Expect(appplyErrors).To(HaveLen(1))

			marshalledErrorsStr := marshallApplyErrors(appplyErrors)
			Expect(marshalledErrorsStr).To(ContainSubstring("bla"))
		})
	})
})

// Test helper functions
func extractShaFromBroker(status brokerStatus, name string) (string, error) {
	current, present := status.BrokerConfigStatus.PropertiesStatus[name]
	if !present {
		return "", fmt.Errorf("property %s not present", name)
	}
	return current.Alder32, nil
}

var _ = Describe("Status Marshalling", func() {
	Context("when marshalling broker status", func() {
		It("should include false boolean values in JSON", func() {
			Status := v1beta2.BrokerStatus{
				Conditions: []metav1.Condition{},
				PodStatus: olm.DeploymentStatus{
					Ready:    []string{},
					Starting: []string{},
					Stopped:  []string{},
				},
				DeploymentPlanSize: 0,
				ScaleLabelSelector: "",
				ExternalConfigs:    []v1beta2.ExternalConfigStatus{},
				Version:            v1beta2.VersionStatus{},
				Upgrade:            v1beta2.UpgradeStatus{},
			}
			v, err := json.Marshal(Status)
			Expect(err).To(BeNil())
			Expect(string(v)).To(ContainSubstring(":false"))
		})
	})
})

var _ = Describe("Broker Host Formatting", func() {
	Context("when formatting templated ingress host strings", func() {
		It("should replace template variables with actual values", func() {
			cr := v1beta2.BrokerCluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "test-ns",
					Name:      "test",
				},
				Spec: v1beta2.BrokerClusterSpec{
					IngressDomain: "my-domain.com",
				},
			}

			specIngressHost := "$(CR_NAME)-$(CR_NAMESPACE)-$(ITEM_NAME)-$(BROKER_ORDINAL)-$(RES_TYPE).$(INGRESS_DOMAIN)"

			ingressHost := formatTemplatedString(&cr, specIngressHost, "0", "my-acceptor", "ing")
			Expect(ingressHost).To(Equal("test-test-ns-my-acceptor-0-ing.my-domain.com"))

			ingressHost = formatTemplatedString(&cr, specIngressHost, "1", "my-connector", "rte")
			Expect(ingressHost).To(Equal("test-test-ns-my-connector-1-rte.my-domain.com"))

			ingressHost = formatTemplatedString(&cr, specIngressHost, "2", "my-console", "abc")
			Expect(ingressHost).To(Equal("test-test-ns-my-console-2-abc.my-domain.com"))
		})
	})
})

var _ = Describe("Templated String with Invalid Variables", func() {
	Context("when template contains unknown variables", func() {
		It("should leave unknown variables unchanged", func() {
			cr := v1beta2.BrokerCluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "test-ns",
					Name:      "test",
				},
				Spec: v1beta2.BrokerClusterSpec{
					IngressDomain: "my-domain.com",
				},
			}

			Expect(formatTemplatedString(&cr, "test-$(UNKNOWN_VAR)", "", "", "")).To(Equal("test-$(UNKNOWN_VAR)"))
			Expect(formatTemplatedString(&cr, "prefix-$(CR_NAME)-$(INVALID)-suffix", "0", "", "")).To(Equal("prefix-test-$(INVALID)-suffix"))
		})
	})
})

var _ = Describe("Templated Object Formatting", func() {
	Context("when formatting complex nested objects with templates", func() {
		It("should recursively replace template variables in maps and arrays", func() {
			cr := v1beta2.BrokerCluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "test-ns",
					Name:      "test",
				},
				Spec: v1beta2.BrokerClusterSpec{
					IngressDomain: "my-domain.com",
				},
			}

			templatedValue := "$(CR_NAME)-$(CR_NAMESPACE)-$(ITEM_NAME)-$(BROKER_ORDINAL)-$(RES_TYPE).$(INGRESS_DOMAIN)"
			templatedObject := map[string]interface{}{
				"plain-string": "TEST",
				"plain-int":    1,
				"test-map": map[string]interface{}{
					"test-map-key":        templatedValue,
					"nested-plain-string": "TEST",
					"nested-plain-int":    1,
					"nested-test-array": []interface{}{
						templatedValue,
					},
				},
				"test-array": []interface{}{
					templatedValue,
					map[string]interface{}{
						"nested-test-map-key": templatedValue,
					},
				},
				"test-string": templatedValue,
			}

			formattedObject := formatTemplatedObject(&cr, templatedObject, "0", "test-name-a", "test-type-A").(map[string]interface{})
			expectedString := "test-test-ns-test-name-a-0-test-type-A.my-domain.com"

			Expect(formattedObject["plain-string"]).To(Equal("TEST"))
			Expect(formattedObject["plain-int"]).To(Equal(1))
			Expect(formattedObject["test-map"].(map[string]interface{})["test-map-key"]).To(Equal(expectedString))
			Expect(formattedObject["test-map"].(map[string]interface{})["nested-plain-string"]).To(Equal("TEST"))
			Expect(formattedObject["test-map"].(map[string]interface{})["nested-plain-int"]).To(Equal(1))
			Expect(formattedObject["test-map"].(map[string]interface{})["nested-test-array"].([]interface{})[0]).To(Equal(expectedString))
			Expect(formattedObject["test-array"].([]interface{})[0]).To(Equal(expectedString))
			Expect(formattedObject["test-array"].([]interface{})[1].(map[string]interface{})["nested-test-map-key"]).To(Equal(expectedString))
			Expect(formattedObject["test-string"]).To(Equal(expectedString))

			formattedObject = formatTemplatedObject(&cr, templatedObject, "1", "test-name-b", "test-type-B").(map[string]interface{})
			expectedString = "test-test-ns-test-name-b-1-test-type-B.my-domain.com"
			Expect(formattedObject["test-string"]).To(Equal(expectedString))
		})
	})
})

var _ = Describe("Broker Property Parsing with Ordinal", func() {
	Context("when parsing broker properties with ordinal prefix", func() {
		It("should correctly parse valid broker-N.property format", func() {
			matches := ParseBrokerPropertyWithOrdinal("broker-0.maxDiskUsage")
			Expect(matches).To(HaveLen(3))
			Expect(matches[0]).To(Equal("broker-0.maxDiskUsage"))
			Expect(matches[1]).To(Equal("broker-0"))
			Expect(matches[2]).To(Equal("maxDiskUsage"))

			matches = ParseBrokerPropertyWithOrdinal("broker-999.maxDiskUsage=97")
			Expect(matches).To(HaveLen(3))
			Expect(matches[0]).To(Equal("broker-999.maxDiskUsage=97"))
			Expect(matches[1]).To(Equal("broker-999"))
			Expect(matches[2]).To(Equal("maxDiskUsage=97"))
		})

		It("should return empty for invalid formats", func() {
			Expect(ParseBrokerPropertyWithOrdinal("maxDiskUsage=97")).To(HaveLen(0))
			Expect(ParseBrokerPropertyWithOrdinal("a.broker-0.maxDiskUsage")).To(HaveLen(0))
			Expect(ParseBrokerPropertyWithOrdinal("broker-0-maxDiskUsage")).To(HaveLen(0))
			Expect(ParseBrokerPropertyWithOrdinal("broker-a.maxDiskUsage")).To(HaveLen(0))
		})
	})
})

var _ = Describe("Broker Properties Data", func() {
	Context("when creating broker properties data without ordinals", func() {
		It("should create single broker.properties entry", func() {
			data := BrokerPropertiesData([]string{
				"maxDiskUsage=97",
				"minDiskFree=5",
			})

			Expect(data).To(HaveLen(1))
			Expect(string(data[BrokerPropertiesName])).To(ContainSubstring("maxDiskUsage=97"))
			Expect(string(data[BrokerPropertiesName])).To(ContainSubstring("minDiskFree=5"))
		})
	})

	Context("when creating broker properties data with ordinals only", func() {
		It("should create separate entries for each ordinal", func() {
			data := BrokerPropertiesData([]string{
				"broker-0.maxDiskUsage=98",
				"broker-0.minDiskFree=6",
				"broker-999.maxDiskUsage=99",
				"broker-999.minDiskFree=7",
			})

			Expect(data).To(HaveLen(3))
			Expect(string(data[BrokerPropertiesName])).NotTo(ContainSubstring("maxDiskUsage"))
			Expect(string(data[BrokerPropertiesName])).NotTo(ContainSubstring("minDiskFree"))

			broker0BrokerPropertiesName := "broker-0" + OrdinalPrefixSep + BrokerPropertiesName
			Expect(string(data[broker0BrokerPropertiesName])).To(ContainSubstring("maxDiskUsage=98"))
			Expect(string(data[broker0BrokerPropertiesName])).To(ContainSubstring("minDiskFree=6"))

			broker999BrokerPropertiesName := "broker-999" + OrdinalPrefixSep + BrokerPropertiesName
			Expect(string(data[broker999BrokerPropertiesName])).To(ContainSubstring("maxDiskUsage=99"))
			Expect(string(data[broker999BrokerPropertiesName])).To(ContainSubstring("minDiskFree=7"))
		})
	})

	Context("when creating broker properties data with mixed ordinals and non-ordinals", func() {
		It("should create entries for both common and ordinal-specific properties", func() {
			data := BrokerPropertiesData([]string{
				"maxDiskUsage=97",
				"minDiskFree=5",
				"broker-0.maxDiskUsage=98",
				"broker-0.minDiskFree=6",
				"broker-999.maxDiskUsage=99",
				"broker-999.minDiskFree=7",
			})

			Expect(data).To(HaveLen(3))
			Expect(string(data[BrokerPropertiesName])).To(ContainSubstring("maxDiskUsage=97"))
			Expect(string(data[BrokerPropertiesName])).To(ContainSubstring("minDiskFree=5"))

			broker0BrokerPropertiesName := "broker-0" + OrdinalPrefixSep + BrokerPropertiesName
			Expect(string(data[broker0BrokerPropertiesName])).To(ContainSubstring("maxDiskUsage=98"))
			Expect(string(data[broker0BrokerPropertiesName])).To(ContainSubstring("minDiskFree=6"))

			broker999BrokerPropertiesName := "broker-999" + OrdinalPrefixSep + BrokerPropertiesName
			Expect(string(data[broker999BrokerPropertiesName])).To(ContainSubstring("maxDiskUsage=99"))
			Expect(string(data[broker999BrokerPropertiesName])).To(ContainSubstring("minDiskFree=7"))
		})
	})
})

var _ = Describe("Duplicate Key Detection", func() {
	Context("when checking for duplicate keys in properties", func() {
		It("should return empty string when no duplicates exist", func() {
			data := []byte("aa\\=a=VAL\naa\\=b=VAL")
			kv := KeyValuePairs(data)

			Expect(kv).To(HaveLen(2))
			Expect(kv[0]).To(HavePrefix("aa"))
			Expect(kv[1]).To(HavePrefix("aa"))
			Expect(DuplicateKeyIn(kv)).To(Equal(""))
		})
	})
})

var _ = Describe("Owner Reference API Version Management", func() {
	Context("when ensuring owner reference API versions", func() {
		It("should return true when no owner references exist", func() {
			cr := &v1beta2.Broker{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "broker.amq.io/v1beta1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-broker",
					Namespace: "test-ns",
				},
			}

			existing := &v1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:            "test-secret",
					OwnerReferences: []metav1.OwnerReference{},
				},
			}

			candidate := &v1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-secret",
				},
			}

			r := NewBrokerReconciler(&NillCluster{}, ctrl.Log, isOpenshift)
			ri := NewBrokerReconcilerImpl(cr, r)

			result := ri.ensureOwnerReferenceAPIVersion(cr, existing, candidate)

			Expect(result).To(BeTrue(), "should return true when no owner references exist")
			Expect(candidate.GetOwnerReferences()).To(BeEmpty(), "candidate owner references should not be modified")
		})

		It("should return true when API versions match", func() {
			cr := &v1beta2.Broker{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "broker.amq.io/v1beta1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-broker",
					Namespace: "test-ns",
				},
			}

			existing := &v1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-secret",
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "broker.amq.io/v1beta1",
							Kind:       "ActiveMQArtemis",
							Name:       "test-broker",
							UID:        "test-uid",
						},
					},
				},
			}

			candidate := &v1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-secret",
				},
			}

			r := NewBrokerReconciler(&NillCluster{}, ctrl.Log, isOpenshift)
			ri := NewBrokerReconcilerImpl(cr, r)

			result := ri.ensureOwnerReferenceAPIVersion(cr, existing, candidate)

			Expect(result).To(BeTrue(), "should return true when API versions match")
			Expect(candidate.GetOwnerReferences()).To(BeEmpty(), "candidate owner references should not be modified when versions match")
		})

		It("should return false and update when API versions differ", func() {
			cr := &v1beta2.Broker{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "broker.amq.io/v1beta1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-broker",
					Namespace: "test-ns",
				},
			}

			existing := &v1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-secret",
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "broker.amq.io/v1alpha1",
							Kind:       "ActiveMQArtemis",
							Name:       "test-broker",
							UID:        "test-uid",
						},
					},
				},
			}

			candidate := &v1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-secret",
				},
			}

			r := NewBrokerReconciler(&NillCluster{}, ctrl.Log, isOpenshift)
			ri := NewBrokerReconcilerImpl(cr, r)

			result := ri.ensureOwnerReferenceAPIVersion(cr, existing, candidate)

			Expect(result).To(BeFalse(), "should return false when API versions differ")
			Expect(candidate.GetOwnerReferences()).To(HaveLen(1), "candidate should have owner references set")
			Expect(candidate.GetOwnerReferences()[0].APIVersion).To(Equal("broker.amq.io/v1beta1"), "candidate should have updated API version")
		})

		It("should handle multiple owner references correctly", func() {
			cr := &v1beta2.Broker{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "broker.amq.io/v1beta1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-broker",
					Namespace: "test-ns",
				},
			}

			existing := &v1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-secret",
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "apps/v1",
							Kind:       "Deployment",
							Name:       "other-owner",
							UID:        "other-uid",
						},
						{
							APIVersion: "broker.amq.io/v1alpha1",
							Kind:       "ActiveMQArtemis",
							Name:       "test-broker",
							UID:        "test-uid",
						},
					},
				},
			}

			candidate := &v1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-secret",
				},
			}

			r := NewBrokerReconciler(&NillCluster{}, ctrl.Log, isOpenshift)
			ri := NewBrokerReconcilerImpl(cr, r)

			result := ri.ensureOwnerReferenceAPIVersion(cr, existing, candidate)

			Expect(result).To(BeFalse(), "should return false when ActiveMQArtemis owner reference API version differs")
			Expect(candidate.GetOwnerReferences()).To(HaveLen(2), "candidate should have both owner references")
			Expect(candidate.GetOwnerReferences()[0].APIVersion).To(Equal("apps/v1"), "first owner reference should remain unchanged")
			Expect(candidate.GetOwnerReferences()[1].APIVersion).To(Equal("broker.amq.io/v1beta1"), "ActiveMQArtemis owner reference should be updated")
		})

		It("should return true when owner reference is for a different broker", func() {
			cr := &v1beta2.Broker{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "broker.amq.io/v1beta1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-broker",
					Namespace: "test-ns",
				},
			}

			existing := &v1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-secret",
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "broker.amq.io/v1alpha1",
							Kind:       "ActiveMQArtemis",
							Name:       "different-broker",
							UID:        "test-uid",
						},
					},
				},
			}

			candidate := &v1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-secret",
				},
			}

			r := NewBrokerReconciler(&NillCluster{}, ctrl.Log, isOpenshift)
			ri := NewBrokerReconcilerImpl(cr, r)

			result := ri.ensureOwnerReferenceAPIVersion(cr, existing, candidate)

			Expect(result).To(BeTrue(), "should return true when owner reference is for a different broker")
			Expect(candidate.GetOwnerReferences()).To(BeEmpty(), "candidate owner references should not be modified")
		})
	})
})

var _ = Describe("Resource Comparison with API Version Updates", func() {
	Context("when comparing Secrets", func() {
		It("should detect and update owner reference API version differences", func() {
			cr := &v1beta2.Broker{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "broker.amq.io/v1beta1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-broker",
					Namespace: "test-ns",
				},
			}

			deployed := &v1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-secret",
					Namespace: "test-ns",
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "broker.amq.io/v1alpha1",
							Kind:       "ActiveMQArtemis",
							Name:       "test-broker",
							UID:        "test-uid",
						},
					},
				},
				Data: map[string][]byte{
					"key": []byte("value"),
				},
			}

			requested := &v1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-secret",
					Namespace: "test-ns",
				},
				Data: map[string][]byte{
					"key": []byte("value"),
				},
			}

			r := NewBrokerReconciler(&NillCluster{}, ctrl.Log, isOpenshift)
			ri := NewBrokerReconcilerImpl(cr, r)

			result := ri.CompareSecret(deployed, requested)

			Expect(result).To(BeFalse(), "should return false when owner reference API version needs update")
			Expect(requested.GetOwnerReferences()).To(HaveLen(1), "requested should have updated owner references")
			Expect(requested.GetOwnerReferences()[0].APIVersion).To(Equal("broker.amq.io/v1beta1"), "API version should be updated")
		})
	})

	Context("when comparing ConfigMaps", func() {
		It("should detect and update owner reference API version differences", func() {
			cr := &v1beta2.Broker{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "broker.amq.io/v1beta1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-broker",
					Namespace: "test-ns",
				},
			}

			deployed := &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: "test-ns",
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "broker.amq.io/v1alpha1",
							Kind:       "ActiveMQArtemis",
							Name:       "test-broker",
							UID:        "test-uid",
						},
					},
				},
			}

			requested := &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: "test-ns",
				},
			}

			r := NewBrokerReconciler(&NillCluster{}, ctrl.Log, isOpenshift)
			ri := NewBrokerReconcilerImpl(cr, r)

			result := ri.CompareConfigMap(deployed, requested)

			Expect(result).To(BeFalse(), "should return false when owner reference API version needs update")
			Expect(requested.GetOwnerReferences()).To(HaveLen(1), "requested should have updated owner references")
			Expect(requested.GetOwnerReferences()[0].APIVersion).To(Equal("broker.amq.io/v1beta1"), "API version should be updated")
		})
	})

	Context("when comparing StatefulSets", func() {
		It("should detect and update owner reference API version differences", func() {
			cr := &v1beta2.Broker{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "broker.amq.io/v1beta1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-broker",
					Namespace: "test-ns",
				},
			}

			deployed := &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-ss",
					Namespace: "test-ns",
					Labels: map[string]string{
						"app": "test",
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "broker.amq.io/v1alpha1",
							Kind:       "ActiveMQArtemis",
							Name:       "test-broker",
							UID:        "test-uid",
						},
					},
				},
				Spec: appsv1.StatefulSetSpec{
					Replicas: pointer.To(int32(1)),
				},
			}

			requested := &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-ss",
					Namespace: "test-ns",
					Labels: map[string]string{
						"app": "test",
					},
				},
				Spec: appsv1.StatefulSetSpec{
					Replicas: pointer.To(int32(1)),
				},
			}

			r := NewBrokerReconciler(&NillCluster{}, ctrl.Log, isOpenshift)
			ri := NewBrokerReconcilerImpl(cr, r)

			result := ri.CompareMetaAndSpec(deployed, requested)

			Expect(result).To(BeFalse(), "should return false when owner reference API version needs update")
			Expect(requested.GetOwnerReferences()).To(HaveLen(1), "requested should have updated owner references")
			Expect(requested.GetOwnerReferences()[0].APIVersion).To(Equal("broker.amq.io/v1beta1"), "API version should be updated")
		})
	})
})

var _ = Describe("CompareConfigMap with API Version Update", func() {
	Context("when owner reference API version needs update", func() {
		It("should return false and update the API version", func() {
			cr := &v1beta2.Broker{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "broker.amq.io/v1beta1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-broker",
					Namespace: "test-ns",
				},
			}

			deployed := &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: "test-ns",
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "broker.amq.io/v1alpha1",
							Kind:       "ActiveMQArtemis",
							Name:       "test-broker",
							UID:        "test-uid",
						},
					},
				},
			}

			requested := &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-config",
					Namespace: "test-ns",
				},
			}

			r := NewBrokerReconciler(&NillCluster{}, ctrl.Log, isOpenshift)
			ri := NewBrokerReconcilerImpl(cr, r)

			result := ri.CompareConfigMap(deployed, requested)

			Expect(result).To(BeFalse(), "should return false when owner reference API version needs update")
			Expect(requested.GetOwnerReferences()).To(HaveLen(1), "requested should have updated owner references")
			Expect(requested.GetOwnerReferences()[0].APIVersion).To(Equal("broker.amq.io/v1beta1"), "API version should be updated")
		})
	})
})

var _ = Describe("CompareMetaAndSpec with API Version Update", func() {
	Context("when owner reference API version needs update", func() {
		It("should return false and update the API version", func() {
			cr := &v1beta2.Broker{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "broker.amq.io/v1beta1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-broker",
					Namespace: "test-ns",
				},
			}

			deployed := &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-ss",
					Namespace: "test-ns",
					Labels: map[string]string{
						"app": "test",
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "broker.amq.io/v1alpha1",
							Kind:       "ActiveMQArtemis",
							Name:       "test-broker",
							UID:        "test-uid",
						},
					},
				},
				Spec: appsv1.StatefulSetSpec{
					Replicas: pointer.To(int32(1)),
				},
			}

			requested := &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-ss",
					Namespace: "test-ns",
					Labels: map[string]string{
						"app": "test",
					},
				},
				Spec: appsv1.StatefulSetSpec{
					Replicas: pointer.To(int32(1)),
				},
			}

			r := NewBrokerReconciler(&NillCluster{}, ctrl.Log, isOpenshift)
			ri := NewBrokerReconcilerImpl(cr, r)

			result := ri.CompareMetaAndSpec(deployed, requested)

			Expect(result).To(BeFalse(), "should return false when owner reference API version needs update")
			Expect(requested.GetOwnerReferences()).To(HaveLen(1), "requested should have updated owner references")
			Expect(requested.GetOwnerReferences()[0].APIVersion).To(Equal("broker.amq.io/v1beta1"), "API version should be updated")
		})
	})
})
