package v1alpha1

import "sigs.k8s.io/cluster-api/api/v1beta1"

const (
	BuildReadyCondition                   v1beta1.ConditionType = "BuildReady"
	RepositoriesReadyCondition            v1beta1.ConditionType = "RepositoriesReady"
	ReleaseBranchReadyCondition           v1beta1.ConditionType = "ReleaseBranchReady"
	AllBranchMergedCondition              v1beta1.ConditionType = "AllBranchMerged"
	ResolveConflictBranchesReadyCondition v1beta1.ConditionType = "ResolveConflictsBranchesReady"
)
