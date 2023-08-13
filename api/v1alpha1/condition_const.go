package v1alpha1

import "sigs.k8s.io/cluster-api/api/v1beta1"

const (
	BuildReadyCondition  v1beta1.ConditionType = "BuildReady"
	BuildSyncedCondition v1beta1.ConditionType = "BuildSyncedReady"
	NotSyncedReason                            = "NotSynced"

	MergesNotReadyReason = "MergesNotReady"

	RepositoriesReadyCondition v1beta1.ConditionType = "RepositoriesReady"
	RepositoriesReason                               = "WrongRepository"

	ReleaseBranchReadyCondition           v1beta1.ConditionType = "ReleaseBranchReady"
	ReleaseBranchReason                                         = "Can't create release branch"
	AllBranchesExistCondition             v1beta1.ConditionType = "AllBranchesExist"
	AllBranchMergedCondition              v1beta1.ConditionType = "AllBranchMerged"
	AllBranchMergedReason                                       = "Merge conflict"
	ResolveConflictBranchesReadyCondition v1beta1.ConditionType = "ResolveConflictsBranchesReady"
	ResolveConflictBranchesReason                               = "Conflict branch"
)
