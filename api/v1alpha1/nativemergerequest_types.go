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
	NativeMergeFinalizer = "nativemergerequest.release.salt.x5.ru/finalizer"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// NativeMergeRequestSpec defines the desired state of NativeMergeRequest.
type NativeMergeRequestSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Foo is an example field of NativeMergeRequest. Edit nativemergerequest_types.go to remove/update
	ProjectID                string   `json:"projectID"`
	Title                    string   `json:"title,omitempty"`
	SourceBranch             string   `json:"sourceBranch"`
	TargetBranch             string   `json:"targetBranch"`
	Labels                   []string `json:"labels,omitempty"`
	CheckSourceBranchMessage string   `json:"checkSourceBranchMessage,omitempty"`
	AutoAccept               bool     `json:"autoAccept,omitempty"`
}

// NativeMergeRequestStatus defines the observed state of NativeMergeRequest.
type NativeMergeRequestStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	IID         string `json:"IID,omitempty"`
	HasConflict bool   `json:"hasConflict,omitempty"`
	Ready       bool   `json:"ready,omitempty"`
	Deleted     bool   `json:"deleted,omitempty"`

	Conditions clusterv1.Conditions `json:"conditions,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// NativeMergeRequest is the Schema for the nativemergerequests API.
type NativeMergeRequest struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   NativeMergeRequestSpec   `json:"spec,omitempty"`
	Status NativeMergeRequestStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// NativeMergeRequestList contains a list of NativeMergeRequest.
type NativeMergeRequestList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []NativeMergeRequest `json:"items"`
}

// GetConditions returns the set of conditions for this object.
func (in *NativeMergeRequest) GetConditions() clusterv1.Conditions {
	return in.Status.Conditions
}

// SetConditions sets the conditions on this object.
func (in *NativeMergeRequest) SetConditions(conditions clusterv1.Conditions) {
	in.Status.Conditions = conditions
}

func init() {
	SchemeBuilder.Register(&NativeMergeRequest{}, &NativeMergeRequestList{})
}
