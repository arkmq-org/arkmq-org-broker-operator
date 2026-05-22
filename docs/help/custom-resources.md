---
title: "Custom Resources"
description: "Custom Resource arkmq-org.io"
lead: "Custom Resource arkmq-org.io"
date: 2020-10-06T08:49:31+00:00
lastmod: 2020-10-06T08:49:31+00:00
draft: false
images: []
menu:
  docs:
    parent: "help"
weight: 630
toc: true
---

## Custom Resource configuration reference
A Custom Resource Definition (CRD) is a schema of configuration items for a custom Kubernetes object deployed with an Operator. 
By deploying a corresponding Custom Resource (CR) instance, you specify values for configuration items shown in the CRD.

The following sub-sections detail the configuration items that you can set in Custom Resource instances based on the main 
broker and addressing CRDs.

### Broker Custom Resource configuration reference

A CR instance based on the main broker CRD enables you to configure brokers for deployment in a Kubernetes project, see the [arkmq-org/arkmq-org-broker-operator CRDs](https://doc.crds.dev/github.com/arkmq-org/arkmq-org-broker-operator). The following is the full CRD yaml file

```yaml
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  annotations:
    controller-gen.kubebuilder.io/version: v0.16.5
  name: brokers.broker.arkmq.org
spec:
  group: broker.arkmq.org
  names:
    kind: Broker
    listKind: BrokerList
    plural: brokers
    shortNames:
    - b
    singular: broker
  scope: Namespaced
  versions:
  - name: v1beta2
    schema:
      openAPIV3Schema:
        description: A stateful deployment of one or more brokers
        properties:
          apiVersion:
            description: 'APIVersion defines the versioned schema of this representation
              of an object. Servers should convert recognized schemas to the latest
              internal value, and may reject unrecognized values. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources'
            type: string
          kind:
            description: 'Kind is a string value representing the REST resource this
              object represents. Servers may infer this from the endpoint the client
              submits requests to. Cannot be updated. In CamelCase. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds'
            type: string
          metadata:
            type: object
          spec:
            description: BrokerSpec defines the desired state of Broker
            properties:
              acceptors:
                description: Acceptor configuration
                items:
                  properties:
                    amqpMinLargeMessageSize:
                      description: AMQP Minimum Large Message Size
                      type: integer
                    anycastPrefix:
                      description: To indicate which kind of routing type to use.
                      type: string
                    connectionsAllowed:
                      description: Max number of connections allowed to make
                      type: integer
                    enabledCipherSuites:
                      description: Comma separated list of cipher suites used for
                        SSL communication.
                      type: string
                    enabledProtocols:
                      description: Comma separated list of protocols used for SSL
                        communication.
                      type: string
                    expose:
                      description: Whether or not to expose this acceptor
                      type: boolean
                    multicastPrefix:
                      description: To indicate which kind of routing type to use
                      type: string
                    name:
                      type: string
                    needClientAuth:
                      description: Tells a client connecting to this acceptor that
                        2-way SSL is required. This property takes precedence over
                        wantClientAuth.
                      type: boolean
                    port:
                      description: Port number
                      format: int32
                      type: integer
                    protocols:
                      description: The protocols to enable for this acceptor
                      type: string
                    sniHost:
                      description: A regular expression used to match the server_name
                        extension on incoming SSL connections. If the name doesn't
                        match then the connection to the acceptor will be rejected.
                      type: string
                    sslEnabled:
                      description: Whether or not to enable SSL on this port
                      type: boolean
                    sslProvider:
                      description: Used to change the SSL Provider between JDK and
                        OPENSSL. The default is JDK.
                      type: string
                    sslSecret:
                      description: Name of the secret to use for ssl information
                      type: string
                    supportAdvisory:
                      description: For openwire protocol if advisory topics are enabled,
                        default false
                      type: boolean
                    suppressInternalManagementObjects:
                      description: If prevents advisory addresses/queues to be registered
                        to management service, default false
                      type: boolean
                    verifyHost:
                      description: The CN of the connecting client's SSL certificate
                        will be compared to its hostname to verify they match. This
                        is useful only for 2-way SSL.
                      type: boolean
                    wantClientAuth:
                      description: Tells a client connecting to this acceptor that
                        2-way SSL is requested but not required. Overridden by needClientAuth.
                      type: boolean
                  required:
                  - name
                  type: object
                type: array
              addressSettings:
                properties:
                  addressSetting:
                    items:
                      properties:
                        addressFullPolicy:
                          description: what happens when an address where maxSizeBytes
                            is specified becomes full
                          type: string
                        autoCreateAddresses:
                          description: whether or not to automatically create addresses
                            when a client sends a message to or attempts to consume
                            a message from a queue mapped to an address that doesnt
                            exist
                          type: boolean
                        autoCreateDeadLetterResources:
                          description: whether or not to automatically create the
                            dead-letter-address and/or a corresponding queue on that
                            address when a message found to be undeliverable
                          type: boolean
                        autoCreateExpiryResources:
                          description: whether or not to automatically create the
                            expiry-address and/or a corresponding queue on that address
                            when a message is sent to a matching queue
                          type: boolean
                        autoCreateJmsQueues:
                          description: DEPRECATED. whether or not to automatically
                            create JMS queues when a producer sends or a consumer
                            connects to a queue
                          type: boolean
                        autoCreateJmsTopics:
                          description: DEPRECATED. whether or not to automatically
                            create JMS topics when a producer sends or a consumer
                            subscribes to a topic
                          type: boolean
                        autoCreateQueues:
                          description: whether or not to automatically create a queue
                            when a client sends a message to or attempts to consume
                            a message from a queue
                          type: boolean
                        autoDeleteAddresses:
                          description: whether or not to delete auto-created addresses
                            when it no longer has any queues
                          type: boolean
                        autoDeleteAddressesDelay:
                          description: how long to wait (in milliseconds) before deleting
                            auto-created addresses after they no longer have any queues
                          format: int32
                          type: integer
                        autoDeleteCreatedQueues:
                          description: whether or not to delete created queues when
                            the queue has 0 consumers and 0 messages
                          type: boolean
                        autoDeleteJmsQueues:
                          description: DEPRECATED. whether or not to delete auto-created
                            JMS queues when the queue has 0 consumers and 0 messages
                          type: boolean
                        autoDeleteJmsTopics:
                          description: DEPRECATED. whether or not to delete auto-created
                            JMS topics when the last subscription is closed
                          type: boolean
                        autoDeleteQueues:
                          description: whether or not to delete auto-created queues
                            when the queue has 0 consumers and 0 messages
                          type: boolean
                        autoDeleteQueuesDelay:
                          description: how long to wait (in milliseconds) before deleting
                            auto-created queues after the queue has 0 consumers.
                          format: int32
                          type: integer
                        autoDeleteQueuesMessageCount:
                          description: the message count the queue must be at or below
                            before it can be evaluated to be auto deleted, 0 waits
                            until empty queue (default) and -1 disables this check.
                          format: int32
                          type: integer
                        configDeleteAddresses:
                          description: What to do when an address is no longer in
                            broker.xml.  OFF = will do nothing addresses will remain,
                            FORCE = delete address and its queues even if messages
                            remaining.
                          type: string
                        configDeleteQueues:
                          description: What to do when a queue is no longer in broker.xml.  OFF
                            = will do nothing queues will remain, FORCE = delete queues
                            even if messages remaining.
                          type: string
                        deadLetterAddress:
                          description: the address to send dead messages to
                          type: string
                        deadLetterQueuePrefix:
                          description: the prefix to use for auto-created dead letter
                            queues
                          type: string
                        deadLetterQueueSuffix:
                          description: the suffix to use for auto-created dead letter
                            queues
                          type: string
                        defaultAddressRoutingType:
                          description: the routing-type used on auto-created addresses
                          type: string
                        defaultConsumerWindowSize:
                          description: the default window size for a consumer
                          format: int32
                          type: integer
                        defaultConsumersBeforeDispatch:
                          description: the default number of consumers needed before
                            dispatch can start for queues under the address.
                          format: int32
                          type: integer
                        defaultDelayBeforeDispatch:
                          description: the default delay (in milliseconds) to wait
                            before dispatching if number of consumers before dispatch
                            is not met for queues under the address.
                          format: int32
                          type: integer
                        defaultExclusiveQueue:
                          description: whether to treat the queues under the address
                            as exclusive queues by default
                          type: boolean
                        defaultGroupBuckets:
                          description: number of buckets to use for grouping, -1 (default)
                            is unlimited and uses the raw group, 0 disables message
                            groups.
                          format: int32
                          type: integer
                        defaultGroupFirstKey:
                          description: key used to mark a message is first in a group
                            for a consumer
                          type: string
                        defaultGroupRebalance:
                          description: whether to rebalance groups when a consumer
                            is added
                          type: boolean
                        defaultGroupRebalancePauseDispatch:
                          description: whether to pause dispatch when rebalancing
                            groups
                          type: boolean
                        defaultLastValueKey:
                          description: the property to use as the key for a last value
                            queue by default
                          type: string
                        defaultLastValueQueue:
                          description: whether to treat the queues under the address
                            as a last value queues by default
                          type: boolean
                        defaultMaxConsumers:
                          description: the maximum number of consumers allowed on
                            this queue at any one time
                          format: int32
                          type: integer
                        defaultNonDestructive:
                          description: whether the queue should be non-destructive
                            by default
                          type: boolean
                        defaultPurgeOnNoConsumers:
                          description: purge the contents of the queue once there
                            are no consumers
                          type: boolean
                        defaultQueueRoutingType:
                          description: the routing-type used on auto-created queues
                          type: string
                        defaultRingSize:
                          description: the default ring-size value for any matching
                            queue which doesnt have ring-size explicitly defined
                          format: int32
                          type: integer
                        enableIngressTimestamp:
                          description: Whether or not set the timestamp of arrival
                            on messages. default false
                          type: boolean
                        enableMetrics:
                          description: whether or not to enable metrics for metrics
                            plugins on the matching address
                          type: boolean
                        expiryAddress:
                          description: the address to send expired messages to
                          type: string
                        expiryDelay:
                          description: Overrides the expiration time for messages
                            using the default value for expiration time. "-1" disables
                            this setting.
                          format: int32
                          type: integer
                        expiryQueuePrefix:
                          description: the prefix to use for auto-created expiry queues
                          type: string
                        expiryQueueSuffix:
                          description: the suffix to use for auto-created expiry queues
                          type: string
                        lastValueQueue:
                          description: This is deprecated please use default-last-value-queue
                            instead.
                          type: boolean
                        managementBrowsePageSize:
                          description: how many message a management resource can
                            browse
                          format: int32
                          type: integer
                        managementMessageAttributeSizeLimit:
                          description: max size of the message returned from management
                            API, default 256
                          format: int32
                          type: integer
                        match:
                          description: pattern for matching settings against addresses;
                            can use wildards
                          type: string
                        maxDeliveryAttempts:
                          description: how many times to attempt to deliver a message
                            before sending to dead letter address
                          format: int32
                          type: integer
                        maxExpiryDelay:
                          description: Overrides the expiration time for messages
                            using a higher value. "-1" disables this setting.
                          format: int32
                          type: integer
                        maxRedeliveryDelay:
                          description: Maximum value for the redelivery-delay
                          format: int32
                          type: integer
                        maxSizeBytes:
                          description: the maximum size in bytes for an address. -1
                            means no limits. This is used in PAGING, BLOCK and FAIL
                            policies. Supports byte notation like K, Mb, GB, etc.
                          type: string
                        maxSizeBytesRejectThreshold:
                          description: used with the address full BLOCK policy, the
                            maximum size in bytes an address can reach before messages
                            start getting rejected. Works in combination with max-size-bytes
                            for AMQP protocol only.  Default = -1 (no limit).
                          format: int32
                          type: integer
                        messageCounterHistoryDayLimit:
                          description: how many days to keep message counter history
                            for this address
                          format: int32
                          type: integer
                        minExpiryDelay:
                          description: Overrides the expiration time for messages
                            using a lower value. "-1" disables this setting.
                          format: int32
                          type: integer
                        pageMaxCacheSize:
                          description: Number of paging files to cache in memory to
                            avoid IO during paging navigation
                          format: int32
                          type: integer
                        pageSizeBytes:
                          description: The page size in bytes to use for an address.
                            Supports byte notation like K, Mb, GB, etc.
                          type: string
                        redeliveryCollisionAvoidanceFactor:
                          description: factor by which to modify the redelivery delay
                            slightly to avoid collisions
                          type: string
                        redeliveryDelay:
                          description: the time (in ms) to wait before redelivering
                            a cancelled message.
                          format: int32
                          type: integer
                        redeliveryDelayMultiplier:
                          description: multiplier to apply to the redelivery-delay
                          type: string
                        redistributionDelay:
                          description: how long (in ms) to wait after the last consumer
                            is closed on a queue before redistributing messages.
                          format: int32
                          type: integer
                        retroactiveMessageCount:
                          description: the number of messages to preserve for future
                            queues created on the matching address
                          format: int32
                          type: integer
                        sendToDlaOnNoRoute:
                          description: if there are no queues matching this address,
                            whether to forward message to DLA (if it exists for this
                            address)
                          type: boolean
                        slowConsumerCheckPeriod:
                          description: How often to check for slow consumers on a
                            particular queue. Measured in seconds.
                          format: int32
                          type: integer
                        slowConsumerPolicy:
                          description: what happens when a slow consumer is identified
                          type: string
                        slowConsumerThreshold:
                          description: The minimum rate of message consumption allowed
                            before a consumer is considered "slow." Measured in messages-per-second.
                          format: int32
                          type: integer
                        slowConsumerThresholdMeasurementUnit:
                          description: Unit used in specifying slow consumer threshold,
                            default is MESSAGE_PER_SECOND
                          type: string
                      type: object
                    type: array
                  applyRule:
                    description: How to merge the address settings to broker configuration
                    type: string
                type: object
              adminPassword:
                description: Password for standard broker user. It is required for
                  connecting to the broker and the web console. If left empty, it
                  will be generated.
                type: string
              adminUser:
                description: User name for standard broker user. It is required for
                  connecting to the broker and the web console. If left empty, it
                  will be generated.
                type: string
              connectors:
                items:
                  properties:
                    enabledCipherSuites:
                      description: Comma separated list of cipher suites used for
                        SSL communication.
                      type: string
                    enabledProtocols:
                      description: Comma separated list of protocols used for SSL
                        communication.
                      type: string
                    expose:
                      description: Whether or not to expose this connector
                      type: boolean
                    host:
                      description: Hostname or IP to connect to
                      type: string
                    name:
                      description: The name of the connector
                      type: string
                    needClientAuth:
                      description: Tells a client connecting to this connector that
                        2-way SSL is required. This property takes precedence over
                        wantClientAuth.
                      type: boolean
                    port:
                      description: Port number
                      format: int32
                      type: integer
                    sniHost:
                      description: A regular expression used to match the server_name
                        extension on incoming SSL connections. If the name doesn't
                        match then the connection to the acceptor will be rejected.
                      type: string
                    sslEnabled:
                      description: ' Whether or not to enable SSL on this port'
                      type: boolean
                    sslProvider:
                      description: Used to change the SSL Provider between JDK and
                        OPENSSL. The default is JDK.
                      type: string
                    sslSecret:
                      description: Name of the secret to use for ssl information
                      type: string
                    type:
                      description: The type either tcp or vm
                      type: string
                    verifyHost:
                      description: The CN of the connecting client's SSL certificate
                        will be compared to its hostname to verify they match. This
                        is useful only for 2-way SSL.
                      type: boolean
                    wantClientAuth:
                      description: Tells a client connecting to this connector that
                        2-way SSL is requested but not required. Overridden by needClientAuth.
                      type: boolean
                  required:
                  - host
                  - name
                  - port
                  type: object
                type: array
              console:
                properties:
                  expose:
                    description: Whether or not to expose this port
                    type: boolean
                  sslEnabled:
                    description: Whether or not to enable SSL on this port
                    type: boolean
                  sslSecret:
                    description: Name of the secret to use for ssl information
                    type: string
                  useClientAuth:
                    description: If the embedded server requires client authentication
                    type: boolean
                type: object
              deploymentPlan:
                properties:
                  clustered:
                    description: Whether broker is clustered
                    type: boolean
                  enableMetricsPlugin:
                    description: Whether or not to install the artemis metrics plugin
                    type: boolean
                  extraMounts:
                    properties:
                      configMaps:
                        description: Name of ConfigMap
                        items:
                          type: string
                        type: array
                      secrets:
                        description: Name of Secret
                        items:
                          type: string
                        type: array
                    type: object
                  image:
                    description: The image used for the broker deployment
                    type: string
                  initImage:
                    description: The init container image used to configure broker
                    type: string
                  jolokiaAgentEnabled:
                    description: If true enable the Jolokia JVM Agent
                    type: boolean
                  journalType:
                    description: If aio use ASYNCIO, if nio use NIO for journal IO
                    type: string
                  livenessProbe:
                    properties:
                      timeoutSeconds:
                        description: Liveness Probe timeoutSeconds for broker container
                        format: int32
                        type: integer
                    type: object
                  managementRBACEnabled:
                    description: If true enable the management role based access control
                    type: boolean
                  messageMigration:
                    description: If true migrate messages on scaledown
                    type: boolean
                  persistenceEnabled:
                    description: If true use persistent volume via persistent volume
                      claim for journal storage
                    type: boolean
                  podSecurity:
                    properties:
                      runAsUser:
                        description: runAsUser as defined in PodSecurityContext for
                          the pod
                        format: int64
                        type: integer
                      serviceAccountName:
                        description: ServiceAccount Name of the pod
                        type: string
                    type: object
                  readinessProbe:
                    properties:
                      timeoutSeconds:
                        description: Readiness Probe timeoutSeconds for broker container
                        format: int32
                        type: integer
                    type: object
                  requireLogin:
                    description: If true require user password login credentials for
                      broker protocol ports
                    type: boolean
                  resources:
                    description: ResourceRequirements describes the compute resource
                      requirements.
                    properties:
                      limits:
                        additionalProperties:
                          anyOf:
                          - type: integer
                          - type: string
                          pattern: ^(\+|-)?(([0-9]+(\.[0-9]*)?)|(\.[0-9]+))(([KMGTPE]i)|[numkMGTPE]|([eE](\+|-)?(([0-9]+(\.[0-9]*)?)|(\.[0-9]+))))?$
                          x-kubernetes-int-or-string: true
                        description: 'Limits describes the maximum amount of compute
                          resources allowed. More info: https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/'
                        type: object
                      requests:
                        additionalProperties:
                          anyOf:
                          - type: integer
                          - type: string
                          pattern: ^(\+|-)?(([0-9]+(\.[0-9]*)?)|(\.[0-9]+))(([KMGTPE]i)|[numkMGTPE]|([eE](\+|-)?(([0-9]+(\.[0-9]*)?)|(\.[0-9]+))))?$
                          x-kubernetes-int-or-string: true
                        description: 'Requests describes the minimum amount of compute
                          resources required. If Requests is omitted for a container,
                          it defaults to Limits if that is explicitly specified, otherwise
                          to an implementation-defined value. More info: https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/'
                        type: object
                    type: object
                  size:
                    description: The number of broker pods to deploy
                    format: int32
                    type: integer
                  storage:
                    properties:
                      size:
                        type: string
                    type: object
                type: object
              upgrades:
                description: Specifies the upgrades (deprecated in favour of Version)
                properties:
                  enabled:
                    description: Set to true to enable automatic micro version product
                      upgrades, disabled by default.
                    type: boolean
                  minor:
                    description: Set to true to enable automatic micro version product
                      upgrades, disabled by default. Requires spec.upgrades.enabled
                      true.
                    type: boolean
                required:
                - enabled
                - minor
                type: object
              version:
                description: The version of the broker deployment.
                type: string
            type: object
          status:
            description: BrokerStatus defines the observed state of Broker
            properties:
              podStatus:
                description: Pod Status
                properties:
                  ready:
                    description: Deployments are ready to serve requests
                    items:
                      type: string
                    type: array
                  starting:
                    description: Deployments are starting, may or may not succeed
                    items:
                      type: string
                    type: array
                  stopped:
                    description: Deployments are not starting, unclear what next step
                      will be
                    items:
                      type: string
                    type: array
                type: object
            required:
            - podStatus
            type: object
        type: object
    served: true
    storage: true
    subresources:
      status: {}
status:
  acceptedNames:
    kind: ""
    plural: ""
  conditions: []
  storedVersions: []
```

### Address Custom Resource configuration reference
A CR instance based on the address CRD enables you to define addresses and queues for the brokers in your deployment. 
The following is thefull CRD yaml

```yaml
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  annotations:
    controller-gen.kubebuilder.io/version: v0.7.0
  name: activemqartemisaddresses.broker.amq.io
spec:
  group: broker.amq.io
  names:
    kind: ActiveMQArtemisAddress
    listKind: ActiveMQArtemisAddressList
    plural: activemqartemisaddresses
    singular: activemqartemisaddress
  scope: Namespaced
  versions:
  - name: v1beta1
    schema:
      openAPIV3Schema:
        description: ActiveMQArtemisAddress is the Schema for the activemqartemisaddresses
          API
        properties:
          apiVersion:
            description: 'APIVersion defines the versioned schema of this representation
              of an object. Servers should convert recognized schemas to the latest
              internal value, and may reject unrecognized values. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources'
            type: string
          kind:
            description: 'Kind is a string value representing the REST resource this
              object represents. Servers may infer this from the endpoint the client
              submits requests to. Cannot be updated. In CamelCase. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds'
            type: string
          metadata:
            type: object
          spec:
            description: ActiveMQArtemisAddressSpec defines the desired state of ActiveMQArtemisAddress
            properties:
              addressName:
                description: Address Name
                type: string
              applyToCrNames:
                description: Apply to the broker crs in the current namespace. A value
                  of * or empty string means applying to all broker crs. Default apply
                  to all broker crs
                items:
                  type: string
                type: array
              password:
                description: The user's password
                type: string
              queueConfiguration:
                properties:
                  autoCreateAddress:
                    description: Whether auto create address
                    type: boolean
                  autoDelete:
                    description: Auto-delete the queue
                    type: boolean
                  autoDeleteDelay:
                    description: Delay (Milliseconds) before auto-delete the queue
                    format: int64
                    type: integer
                  autoDeleteMessageCount:
                    description: Message count of the queue to allow auto delete
                    format: int64
                    type: integer
                  configurationManaged:
                    description: ' If the queue is configuration managed'
                    type: boolean
                  consumerPriority:
                    description: Consumer Priority
                    format: int32
                    type: integer
                  consumersBeforeDispatch:
                    description: Number of consumers required before dispatching messages
                    format: int32
                    type: integer
                  delayBeforeDispatch:
                    description: Milliseconds to wait for `consumers-before-dispatch`
                      to be met before dispatching messages anyway
                    format: int64
                    type: integer
                  durable:
                    description: If the queue is durable or not
                    type: boolean
                  enabled:
                    description: If the queue is enabled
                    type: boolean
                  exclusive:
                    description: If the queue is exclusive
                    type: boolean
                  filterString:
                    description: The filter string for the queue
                    type: string
                  groupBuckets:
                    description: Number of messaging group buckets
                    format: int32
                    type: integer
                  groupFirstKey:
                    description: Header set on the first group message
                    type: string
                  groupRebalance:
                    description: If rebalance the message group
                    type: boolean
                  groupRebalancePauseDispatch:
                    description: If pause message dispatch when rebalancing groups
                    type: boolean
                  ignoreIfExists:
                    description: If ignore if the target queue already exists
                    type: boolean
                  lastValue:
                    description: If it is a last value queue
                    type: boolean
                  lastValueKey:
                    description: The property used for last value queue to identify
                      last values
                    type: string
                  maxConsumers:
                    description: Max number of consumers allowed on this queue
                    format: int32
                    type: integer
                  nonDestructive:
                    description: If force non-destructive consumers on the queue
                    type: boolean
                  purgeOnNoConsumers:
                    description: Whether to delete all messages when no consumers
                      connected to the queue
                    type: boolean
                  ringSize:
                    description: The size the queue should maintain according to ring
                      semantics
                    format: int64
                    type: integer
                  routingType:
                    description: The routing type of the queue
                    type: string
                  temporary:
                    description: If the queue is temporary
                    type: boolean
                  user:
                    description: The user associated with the queue
                    type: string
                required:
                - maxConsumers
                - purgeOnNoConsumers
                type: object
              queueName:
                description: Queue Name
                type: string
              removeFromBrokerOnDelete:
                description: Whether or not delete the queue from broker when CR is
                  undeployed(default false)
                type: boolean
              routingType:
                description: The Routing Type
                type: string
              user:
                description: User name for creating the queue or address
                type: string
            type: object
          status:
            description: ActiveMQArtemisAddressStatus defines the observed state of
              ActiveMQArtemisAddress
            type: object
        type: object
    served: true
    storage: true
    subresources:
      status: {}
status:
  acceptedNames:
    kind: ""
    plural: ""
  conditions: []
  storedVersions: []
```
### Security Custom Resource configuration reference
A CR instance based on the Security CRD enables you to define security for the brokers in your deployment. 
The following is the full CRD yaml

```yaml
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  annotations:
    controller-gen.kubebuilder.io/version: v0.7.0
  name: activemqartemissecurities.broker.amq.io
spec:
  group: broker.amq.io
  names:
    kind: ActiveMQArtemisSecurity
    listKind: ActiveMQArtemisSecurityList
    plural: activemqartemissecurities
    singular: activemqartemissecurity
  scope: Namespaced
  versions:
  - name: v1alpha1
    schema:
      openAPIV3Schema:
        description: ActiveMQArtemisSecurity is the Schema for the activemqartemissecurities
          API
        properties:
          apiVersion:
            description: 'APIVersion defines the versioned schema of this representation
              of an object. Servers should convert recognized schemas to the latest
              internal value, and may reject unrecognized values. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources'
            type: string
          kind:
            description: 'Kind is a string value representing the REST resource this
              object represents. Servers may infer this from the endpoint the client
              submits requests to. Cannot be updated. In CamelCase. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds'
            type: string
          metadata:
            type: object
          spec:
            description: ActiveMQArtemisSecuritySpec defines the desired state of
              ActiveMQArtemisSecurity
            properties:
              applyToCrNames:
                items:
                  type: string
                type: array
              loginModules:
                properties:
                  guestLoginModules:
                    items:
                      properties:
                        guestRole:
                          type: string
                        guestUser:
                          type: string
                        name:
                          type: string
                      type: object
                    type: array
                  keycloakLoginModules:
                    items:
                      properties:
                        configuration:
                          properties:
                            allowAnyHostName:
                              type: boolean
                            alwaysRefreshToken:
                              type: boolean
                            authServerUrl:
                              type: string
                            autoDetectBearerOnly:
                              type: boolean
                            bearerOnly:
                              type: boolean
                            clientKeyPassword:
                              type: string
                            clientKeyStore:
                              type: string
                            clientKeyStorePassword:
                              type: string
                            confidentialPort:
                              format: int32
                              type: integer
                            connectionPoolSize:
                              format: int64
                              type: integer
                            corsAllowedHeaders:
                              type: string
                            corsAllowedMethods:
                              type: string
                            corsExposedHeaders:
                              type: string
                            corsMaxAge:
                              format: int64
                              type: integer
                            credentials:
                              items:
                                properties:
                                  key:
                                    type: string
                                  value:
                                    type: string
                                type: object
                              type: array
                            disableTrustManager:
                              type: boolean
                            enableBasicAuth:
                              type: boolean
                            enableCors:
                              type: boolean
                            exposeToken:
                              type: boolean
                            ignoreOauthQueryParameter:
                              type: boolean
                            minTimeBetweenJwksRequests:
                              format: int64
                              type: integer
                            principalAttribute:
                              type: string
                            proxyUrl:
                              type: string
                            publicClient:
                              type: boolean
                            publicKeyCacheTtl:
                              format: int64
                              type: integer
                            realm:
                              type: string
                            realmPublicKey:
                              type: string
                            redirectRewriteRules:
                              items:
                                properties:
                                  key:
                                    type: string
                                  value:
                                    type: string
                                type: object
                              type: array
                            registerNodeAtStartup:
                              type: boolean
                            registerNodePeriod:
                              format: int64
                              type: integer
                            resource:
                              type: string
                            scope:
                              type: string
                            sslRequired:
                              type: string
                            tokenCookiePath:
                              type: string
                            tokenMinimumTimeToLive:
                              format: int64
                              type: integer
                            tokenStore:
                              type: string
                            trustStore:
                              type: string
                            trustStorePassword:
                              type: string
                            turnOffChangeSessionIdOnLogin:
                              type: boolean
                            useResourceRoleMappings:
                              type: boolean
                            verifyTokenAudience:
                              type: boolean
                          required:
                          - enableBasicAuth
                          type: object
                        moduleType:
                          type: string
                        name:
                          type: string
                      type: object
                    type: array
                  propertiesLoginModules:
                    items:
                      properties:
                        name:
                          type: string
                        users:
                          items:
                            properties:
                              name:
                                type: string
                              password:
                                type: string
                              roles:
                                items:
                                  type: string
                                type: array
                            type: object
                          type: array
                      type: object
                    type: array
                type: object
              securityDomains:
                properties:
                  brokerDomain:
                    properties:
                      loginModules:
                        items:
                          properties:
                            debug:
                              type: boolean
                            flag:
                              type: string
                            name:
                              type: string
                            reload:
                              type: boolean
                          type: object
                        type: array
                      name:
                        type: string
                    type: object
                  consoleDomain:
                    properties:
                      loginModules:
                        items:
                          properties:
                            debug:
                              type: boolean
                            flag:
                              type: string
                            name:
                              type: string
                            reload:
                              type: boolean
                          type: object
                        type: array
                      name:
                        type: string
                    type: object
                type: object
              securitySettings:
                properties:
                  broker:
                    items:
                      properties:
                        match:
                          type: string
                        permissions:
                          items:
                            properties:
                              operationType:
                                type: string
                              roles:
                                items:
                                  type: string
                                type: array
                            required:
                            - operationType
                            type: object
                          type: array
                      type: object
                    type: array
                  management:
                    properties:
                      authorisation:
                        properties:
                          allowedList:
                            items:
                              properties:
                                domain:
                                  type: string
                                key:
                                  type: string
                              type: object
                            type: array
                          defaultAccess:
                            items:
                              properties:
                                method:
                                  type: string
                                roles:
                                  items:
                                    type: string
                                  type: array
                              type: object
                            type: array
                          roleAccess:
                            items:
                              properties:
                                accessList:
                                  items:
                                    properties:
                                      method:
                                        type: string
                                      roles:
                                        items:
                                          type: string
                                        type: array
                                    type: object
                                  type: array
                                domain:
                                  type: string
                                key:
                                  type: string
                              type: object
                            type: array
                        type: object
                      connector:
                        properties:
                          authenticatorType:
                            type: string
                          host:
                            type: string
                          jmxRealm:
                            type: string
                          keyStorePassword:
                            type: string
                          keyStorePath:
                            type: string
                          keyStoreProvider:
                            type: string
                          objectName:
                            type: string
                          passwordCodec:
                            type: string
                          port:
                            format: int32
                            type: integer
                          rmiRegistryPort:
                            format: int32
                            type: integer
                          secured:
                            type: boolean
                          trustStorePassword:
                            type: string
                          trustStorePath:
                            type: string
                          trustStoreProvider:
                            type: string
                        type: object
                      hawtioRoles:
                        items:
                          type: string
                        type: array
                    type: object
                type: object
            type: object
          status:
            description: ActiveMQArtemisSecurityStatus defines the observed state
              of ActiveMQArtemisSecurity
            type: object
        type: object
    served: true
    storage: false
    subresources:
      status: {}
status:
  acceptedNames:
    kind: ""
    plural: ""
  conditions: []
  storedVersions: []
```

### Message Migration Custom Resource configuration reference
A CR instance based on the address CRD enables you to define message migration for the brokers in your deployment. 
The following is the full CRD yaml

```yaml
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  annotations:
    controller-gen.kubebuilder.io/version: v0.7.0
  name: activemqartemisscaledowns.broker.amq.io
spec:
  group: broker.amq.io
  names:
    kind: ActiveMQArtemisScaledown
    listKind: ActiveMQArtemisScaledownList
    plural: activemqartemisscaledowns
    singular: activemqartemisscaledown
  scope: Namespaced
  versions:
  - name: v2alpha1
    schema:
      openAPIV3Schema:
        description: ActiveMQArtemisScaledown is the Schema for the activemqartemisscaledowns
          API
        properties:
          apiVersion:
            description: 'APIVersion defines the versioned schema of this representation
              of an object. Servers should convert recognized schemas to the latest
              internal value, and may reject unrecognized values. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources'
            type: string
          kind:
            description: 'Kind is a string value representing the REST resource this
              object represents. Servers may infer this from the endpoint the client
              submits requests to. Cannot be updated. In CamelCase. More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds'
            type: string
          metadata:
            type: object
          spec:
            description: ActiveMQArtemisScaledownSpec defines the desired state of
              ActiveMQArtemisScaledown
            properties:
              localOnly:
                description: Triggered by main ActiveMQArtemis CRD messageMigration
                  entry
                type: boolean
            required:
            - localOnly
            type: object
          status:
            description: ActiveMQArtemisScaledownStatus defines the observed state
              of ActiveMQArtemisScaledown
            type: object
        type: object
    served: true
    storage: true
    subresources:
      status: {}
status:
  acceptedNames:
    kind: ""
    plural: ""
  conditions: []
  storedVersions: []
```
