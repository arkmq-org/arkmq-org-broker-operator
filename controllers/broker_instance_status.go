package controllers

// This file defines internal types for broker instance status data reported by
// a background status script (broker-status.sh) running inside the broker
// container via Pod annotations. These types are intentionally NOT part of the
// CRD API (api/v1beta1 or api/v1beta2) — the script writes directly to Pod
// annotations, keeping the CRD schema unchanged.
//
// Data flow:
//   status script → Pod annotation (BrokerStatusAnnotationKey) → operator
//   parses into BrokerInstanceStatus → used by CheckBrokerInstanceStatuses,
//   aggregateSyncStatus, AssertBrokerImageVersion, checkProjectionStatus,
//   and scale-down logic.
//
// The operator reads annotations from the pod cache on its regular resync
// cycle (default 30s). No dedicated pod watch is needed — Owns(&Pod{}) on
// the controller already populates the pod cache, and listBrokerInstanceStatuses
// reads from it on every reconcile.

// BrokerStatusAnnotationKey is the Pod annotation key where the broker-status
// script writes the raw Jolokia Status JSON. The script updates this annotation
// on AMQ221087 reload-completed events (Artemis 2.54+) and at startup.
const BrokerStatusAnnotationKey = "broker.arkmq.org/broker-status"

// BrokerInstanceStatus holds the observed runtime state of a single broker pod,
// derived from the raw Jolokia Status attribute stored in the pod annotation.
// The status script writes the raw JSON; listBrokerInstanceStatuses converts
// it to this struct. All checksum comparison and sync logic stays in the
// operator -- identical to how main worked with direct Jolokia access.
type BrokerInstanceStatus struct {
	PodName          string                          `json:"-"`
	PropertiesStatus map[string]PropertiesFileStatus `json:"-"`
	JaasStatus       map[string]PropertiesFileStatus `json:"-"`
	ServerState      string                          `json:"-"`
	ServerVersion    string                          `json:"-"`
	NodeID           string                          `json:"-"`
	Uptime           string                          `json:"-"`
	// ReloadInProgress is set by the operator when the annotation is present
	// but empty, indicating the status script cleared it after detecting a
	// reload event and is currently polling Jolokia for the new status.
	ReloadInProgress bool `json:"-"`
}

// PropertiesFileStatus tracks the checksum and apply state of a single broker
// properties file. Alder32 is the checksum of the file content as loaded by
// the broker; FileAlder32 is the checksum of the file on disk (from the
// projected volume). When they match, the broker has successfully reloaded.
type PropertiesFileStatus struct {
	Alder32     string               `json:"alder32,omitempty"`
	FileAlder32 string               `json:"fileAlder32,omitempty"`
	ReloadTime  string               `json:"reloadTime,omitempty"`
	ApplyErrors []PropertyApplyError `json:"applyErrors,omitempty"`
}

// PropertyApplyError describes a single property that the broker rejected
// during a reload attempt.
type PropertyApplyError struct {
	Value  string `json:"value"`
	Reason string `json:"reason"`
}
