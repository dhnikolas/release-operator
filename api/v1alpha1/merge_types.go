/*
Copyright 2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
)

const (
	MergeFinalizer = "merge.release.salt.x5.ru/finalizer"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// MergeSpec defines the desired state of Merge.
type MergeSpec struct {
	Repo Repo `json:"repo"`
}

// MergeStatus defines the observed state of Merge.
type MergeStatus struct {
	URL                   string         `json:"URL,omitempty"`
	BuildBranch           string         `json:"buildBranch,omitempty"`
	ResolveConflictBranch string         `json:"resolveConflictBranch,omitempty"`
	Branches              []BranchStatus `json:"branches,omitempty"`
	FailureMessage        *string        `json:"failureMessage,omitempty"`

	Conditions clusterv1.Conditions `json:"conditions,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// Merge is the Schema for the merges API.
type Merge struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   MergeSpec   `json:"spec,omitempty"`
	Status MergeStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// MergeList contains a list of Merge.
type MergeList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Merge `json:"items"`
}

// GetConditions returns the set of conditions for this object.
func (in *Merge) GetConditions() clusterv1.Conditions {
	return in.Status.Conditions
}

// SetConditions sets the conditions on this object.
func (in *Merge) SetConditions(conditions clusterv1.Conditions) {
	in.Status.Conditions = conditions
}

func init() {
	SchemeBuilder.Register(&Merge{}, &MergeList{})
}
