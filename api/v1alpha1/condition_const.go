package v1alpha1

import "sigs.k8s.io/cluster-api/api/v1beta1"

const (
	BuildReadyCondition                   v1beta1.ConditionType = "BuildReady"
	BuildSyncedCondition                  v1beta1.ConditionType = "BuildSyncedReady"
	RepositoriesReadyCondition            v1beta1.ConditionType = "RepositoriesReady"
	ReleaseBranchReadyCondition           v1beta1.ConditionType = "ReleaseBranchReady"
	AllBranchesExistCondition             v1beta1.ConditionType = "AllBranchesExist"
	AllBranchMergedCondition              v1beta1.ConditionType = "AllBranchMerged"
	ResolveConflictBranchesReadyCondition v1beta1.ConditionType = "ResolveConflictsBranchesReady"

	BranchExistCondition        v1beta1.ConditionType = "BranchExistReady"
	NativeMergeRequestCondition v1beta1.ConditionType = "NativeMergeRequestReady"
	BranchCommitCondition       v1beta1.ConditionType = "BranchCommitReady"
)

const (
	NotSyncedReason               = "NotSynced"
	MergesNotReadyReason          = "MergesNotReady"
	RepositoriesReason            = "WrongRepository"
	ReleaseBranchReason           = "Can't create release branch"
	AllBranchMergedReason         = "Merge conflict"
	ResolveConflictBranchesReason = "Conflict branch"

	BranchExistReason        = "Branch not exist"
	NativeMergeRequestReason = "Native merge request is error"
	BranchCommitReady        = "Branch not ready yet"
)
