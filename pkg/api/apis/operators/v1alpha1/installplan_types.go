package v1alpha1

import (
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	InstallPlanKind       = "InstallPlan"
	InstallPlanAPIVersion = GroupName + "/" + GroupVersion
)

// Approval is the user approval policy for an InstallPlan.
type Approval string

const (
	ApprovalAutomatic Approval = "Automatic"
	ApprovalManual    Approval = "Manual"
)

// InstallPlanSpec defines a set of Application resources to be installed
type InstallPlanSpec struct {
	CatalogSource              string   `json:"source"`
	CatalogSourceNamespace     string   `json:"sourceNamespace"`
	ClusterServiceVersionNames []string `json:"clusterServiceVersionNames"`
	Approval                   Approval `json:"approval"`
	Approved                   bool     `json:"approved"`
}

// InstallPlanPhase is the current status of a InstallPlan as a whole.
type InstallPlanPhase string

const (
	InstallPlanPhaseNone             InstallPlanPhase = ""
	InstallPlanPhasePlanning         InstallPlanPhase = "Planning"
	InstallPlanPhaseRequiresApproval InstallPlanPhase = "RequiresApproval"
	InstallPlanPhaseInstalling       InstallPlanPhase = "Installing"
	InstallPlanPhaseComplete         InstallPlanPhase = "Complete"
	InstallPlanPhaseFailed           InstallPlanPhase = "Failed"
)

// InstallPlanConditionType describes the state of an InstallPlan at a certain point as a whole.
type InstallPlanConditionType string

const (
	InstallPlanResolved  InstallPlanConditionType = "Resolved"
	InstallPlanInstalled InstallPlanConditionType = "Installed"
)

// ConditionReason is a camelcased reason for the state transition.
type InstallPlanConditionReason string

const (
	InstallPlanReasonPlanUnknown        InstallPlanConditionReason = "PlanUnknown"
	InstallPlanReasonInstallCheckFailed InstallPlanConditionReason = "InstallCheckFailed"
	InstallPlanReasonDependencyConflict InstallPlanConditionReason = "DependenciesConflict"
	InstallPlanReasonComponentFailed    InstallPlanConditionReason = "InstallComponentFailed"
)

// StepStatus is the current status of a particular resource an in
// InstallPlan
type StepStatus string

const (
	StepStatusUnknown    StepStatus = "Unknown"
	StepStatusNotPresent StepStatus = "NotPresent"
	StepStatusPresent    StepStatus = "Present"
	StepStatusCreated    StepStatus = "Created"
)

// ErrInvalidInstallPlan is the error returned by functions that operate on
// InstallPlans when the InstallPlan does not contain totally valid data.
var ErrInvalidInstallPlan = errors.New("the InstallPlan contains invalid data")

// InstallPlanStatus represents the information about the status of
// steps required to complete installation.
//
// Status may trail the actual state of a system.
type InstallPlanStatus struct {
	Phase          InstallPlanPhase       `json:"phase"`
	Conditions     []InstallPlanCondition `json:"conditions,omitempty"`
	CatalogSources []string               `json:"catalogSources"`
	Plan           []*Step                `json:"plan,omitempty"`

	// AttenuatedServiceAccountRef references the service account that is used
	// to do scoped operator install.
	AttenuatedServiceAccountRef *corev1.ObjectReference `json:"attenuatedServiceAccountRef,omitempty"`
}

// InstallPlanCondition represents the overall status of the execution of
// an InstallPlan.
type InstallPlanCondition struct {
	Type               InstallPlanConditionType   `json:"type,omitempty"`
	Status             corev1.ConditionStatus     `json:"status,omitempty"` // True, False, or Unknown
	LastUpdateTime     metav1.Time                `json:"lastUpdateTime,omitempty"`
	LastTransitionTime metav1.Time                `json:"lastTransitionTime,omitempty"`
	Reason             InstallPlanConditionReason `json:"reason,omitempty"`
	Message            string                     `json:"message,omitempty"`
}

// allow overwriting `now` function for deterministic tests
var now = metav1.Now

// GetCondition returns the InstallPlanCondition of the given type if it exists in the InstallPlanStatus' Conditions.
// Returns a condition of the given type with a ConditionStatus of "Unknown" if not found.
func (s InstallPlanStatus) GetCondition(conditionType InstallPlanConditionType) InstallPlanCondition {
	for _, cond := range s.Conditions {
		if cond.Type == conditionType {
			return cond
		}
	}

	return InstallPlanCondition{
		Type:   conditionType,
		Status: corev1.ConditionUnknown,
	}
}

// SetCondition adds or updates a condition, using `Type` as merge key.
func (s *InstallPlanStatus) SetCondition(cond InstallPlanCondition) InstallPlanCondition {
	for i, existing := range s.Conditions {
		if existing.Type != cond.Type {
			continue
		}
		if existing.Status == cond.Status {
			cond.LastTransitionTime = existing.LastTransitionTime
		}
		s.Conditions[i] = cond
		return cond
	}
	s.Conditions = append(s.Conditions, cond)
	return cond
}

func ConditionFailed(cond InstallPlanConditionType, reason InstallPlanConditionReason, message string, now *metav1.Time) InstallPlanCondition {
	return InstallPlanCondition{
		Type:               cond,
		Status:             corev1.ConditionFalse,
		Reason:             reason,
		Message:            message,
		LastUpdateTime:     *now,
		LastTransitionTime: *now,
	}
}

func ConditionMet(cond InstallPlanConditionType, now *metav1.Time) InstallPlanCondition {
	return InstallPlanCondition{
		Type:               cond,
		Status:             corev1.ConditionTrue,
		LastUpdateTime:     *now,
		LastTransitionTime: *now,
	}
}

// Step represents the status of an individual step in an InstallPlan.
type Step struct {
	Resolving string       `json:"resolving"`
	Resource  StepResource `json:"resource"`
	Status    StepStatus   `json:"status"`
}

// ManifestsMatch returns true if the CSV manifests in the StepResources of the given list of steps
// matches those in the InstallPlanStatus.
func (s *InstallPlanStatus) CSVManifestsMatch(steps []*Step) bool {
	if s.Plan == nil && steps == nil {
		return true
	}
	if s.Plan == nil || steps == nil {
		return false
	}

	manifests := make(map[string]struct{})
	for _, step := range s.Plan {
		resource := step.Resource
		if resource.Kind != ClusterServiceVersionKind {
			continue
		}
		manifests[resource.Manifest] = struct{}{}
	}

	for _, step := range steps {
		resource := step.Resource
		if resource.Kind != ClusterServiceVersionKind {
			continue
		}
		if _, ok := manifests[resource.Manifest]; !ok {
			return false
		}
		delete(manifests, resource.Manifest)
	}

	if len(manifests) == 0 {
		return true
	}

	return false
}

func (s *Step) String() string {
	return fmt.Sprintf("%s: %s (%s)", s.Resolving, s.Resource, s.Status)
}

// StepResource represents the status of a resource to be tracked by an
// InstallPlan.
type StepResource struct {
	CatalogSource          string `json:"sourceName"`
	CatalogSourceNamespace string `json:"sourceNamespace"`
	Group                  string `json:"group"`
	Version                string `json:"version"`
	Kind                   string `json:"kind"`
	Name                   string `json:"name"`
	Manifest               string `json:"manifest,omitempty"`
}

func (r StepResource) String() string {
	return fmt.Sprintf("%s[%s/%s/%s (%s/%s)]", r.Name, r.Group, r.Version, r.Kind, r.CatalogSource, r.CatalogSourceNamespace)
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +genclient

// InstallPlan defines the installation of a set of operators.
type InstallPlan struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	Spec   InstallPlanSpec   `json:"spec"`
	Status InstallPlanStatus `json:"status"`
}

// EnsureCatalogSource ensures that a CatalogSource is present in the Status
// block of an InstallPlan.
func (p *InstallPlan) EnsureCatalogSource(sourceName string) {
	for _, srcName := range p.Status.CatalogSources {
		if srcName == sourceName {
			return
		}
	}

	p.Status.CatalogSources = append(p.Status.CatalogSources, sourceName)
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// InstallPlanList is a list of InstallPlan resources.
type InstallPlanList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`

	Items []InstallPlan `json:"items"`
}
