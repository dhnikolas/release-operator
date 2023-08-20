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

package controllers

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/cluster-api/util/conditions"
	"sigs.k8s.io/cluster-api/util/patch"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	releasev1alpha1 "scm.x5.ru/dis.cloud/operators/release-operator/api/v1alpha1"
	"scm.x5.ru/dis.cloud/operators/release-operator/internal/app"
)

const OkCommitMessage = "okok"

// MergeReconciler reconciles a Merge object.
type MergeReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	App    *app.App
}

//+kubebuilder:rbac:groups=release.salt.x5.ru,resources=merges,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=release.salt.x5.ru,resources=merges/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=release.salt.x5.ru,resources=merges/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Merge object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.12.2/pkg/reconcile
func (r *MergeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, reterr error) {
	logger := log.FromContext(ctx)

	logger.Info("Reconcile Merge " + req.Name)

	merge := &releasev1alpha1.Merge{}
	err := r.Client.Get(ctx, req.NamespacedName, merge)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}
		return reconcile.Result{Requeue: true}, err
	}
	patchHelper, err := patch.NewHelper(merge, r.Client)
	if err != nil {
		return ctrl.Result{}, err
	}

	defer func() {
		if err := patchMerge(ctx, patchHelper, merge); err != nil {
			if reterr == nil {
				reterr = errors.Wrapf(err, "error patching Build %s/%s", merge.Namespace, merge.Name)
			}
			logger.Error(err, "Patch Merge error")
		}
	}()

	if !merge.GetDeletionTimestamp().IsZero() {
		return r.reconcileDelete(ctx, merge)
	}

	return r.reconcileNormal(ctx, merge)
}

type currentBranches struct {
	MainBranch  *releasev1alpha1.BranchStatus
	AliasBranch *releasev1alpha1.BranchStatus
}

func makeCurrentBranches(branch *releasev1alpha1.BranchStatus, merge *releasev1alpha1.Merge) *currentBranches {
	cb := &currentBranches{}
	if branch.ResolveBranch != "" && merge.Status.ResolveConflictBranch != nil &&
		merge.Status.ResolveConflictBranch.Name == branch.ResolveBranch {
		cb.MainBranch = merge.Status.ResolveConflictBranch
		cb.AliasBranch = branch
	} else {
		cb.MainBranch = branch
		cb.AliasBranch = branch
	}

	return cb
}

func (r *MergeReconciler) reconcileNormal(ctx context.Context, merge *releasev1alpha1.Merge) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	if !controllerutil.ContainsFinalizer(merge, releasev1alpha1.MergeFinalizer) {
		if controllerutil.AddFinalizer(merge, releasev1alpha1.MergeFinalizer) {
			logger.Info("Finalizer not add " + releasev1alpha1.MergeFinalizer)
			return reconcile.Result{Requeue: true}, nil
		}
	}

	projectURL, err := url.Parse(merge.Spec.Repo.URL)
	if err != nil {
		conditions.MarkFalse(merge, releasev1alpha1.RepositoriesReadyCondition, releasev1alpha1.RepositoriesReason,
			clusterv1.ConditionSeverityError,
			"Cannot parse repo URL %s", merge.Spec.Repo.URL)
		return ctrl.Result{}, err
	}
	projectPID := strings.Trim(projectURL.Path, "/")

	if projectPID == "" {
		conditions.MarkFalse(merge, releasev1alpha1.RepositoriesReadyCondition, releasev1alpha1.RepositoriesReason,
			clusterv1.ConditionSeverityError,
			"Wrong projectPID %s", projectPID)
		return ctrl.Result{}, err
	}

	merge.Status.ProjectPID = projectPID
	_, projectExits, err := r.App.GitClient.GetProject(projectPID)
	if err != nil {
		conditions.MarkFalse(merge, releasev1alpha1.RepositoriesReadyCondition, releasev1alpha1.RepositoriesReason,
			clusterv1.ConditionSeverityError,
			"Error get project by URL %s %s", merge.Spec.Repo.URL, err)
		return ctrl.Result{RequeueAfter: time.Second * 15}, err
	}
	if !projectExits {
		conditions.MarkFalse(merge, releasev1alpha1.RepositoriesReadyCondition, releasev1alpha1.RepositoriesReason,
			clusterv1.ConditionSeverityError,
			"Error Project not exit %s ", merge.Spec.Repo.URL)
		return ctrl.Result{}, fmt.Errorf("project not exit %s ", merge.Spec.Repo.URL)
	}
	conditions.MarkTrue(merge, releasev1alpha1.RepositoriesReadyCondition)
	buildName := merge.Labels[releasev1alpha1.BuildNameLabel]

	branches := getBranchesNames(merge.Spec.Repo.Branches)
	statusBranches := getStatusBranchesNames(merge.Status.Branches)
	add, remove, _ := FullDiff(branches, statusBranches)

	if len(remove) > 0 {
		err = r.App.GitClient.RemoveBranch(projectPID, buildName)
		if err != nil {
			conditions.MarkFalse(merge, releasev1alpha1.ReleaseBranchReadyCondition, releasev1alpha1.ReleaseBranchReason,
				clusterv1.ConditionSeverityError,
				"Recreate branch error: URL %s %s", merge.Spec.Repo.URL, err)
			return ctrl.Result{Requeue: true}, err
		}
		merge.Status.BuildBranch = buildName
		add = branches
		merge.Status.Branches = make([]releasev1alpha1.BranchStatus, 0)
		merge.Status.ResolveConflictBranch = nil
		merge.Status.HasConflict = false
	}

	if merge.Status.HasConflict {

	} else {
		r.addBranchesToStatus(merge, add)
	}

	notReadyYet := false
	for i := range merge.Status.Branches {
		b := &merge.Status.Branches[i]
		nmrSpec := releasev1alpha1.NativeMergeRequestSpec{
			ProjectID:    projectPID,
			Title:        "release-operator",
			SourceBranch: b.Name,
			TargetBranch: buildName,
			Labels:       []string{"release-operator"},
			AutoAccept:   true,
		}
		nmr, err := r.getOrCreateNativeMR(ctx, b.MergeRequestID, merge, nmrSpec)
		if err != nil {
			return ctrl.Result{Requeue: true}, err
		}
		b.MergeRequestID = nmr.Status.IID

		if nmr.Status.HasConflict {
			merge.Status.HasConflict = true
		}

		if !nmr.Status.Ready {
			notReadyYet = true
			b.IsMerged = False
		} else {
			b.IsMerged = True
		}
	}

	if notReadyYet {
		logger.Info("Wait for all branches merge " + projectPID)
		return reconcile.Result{RequeueAfter: time.Second * 3}, nil
	}

	logger.Info("MR successful merged " + projectPID)
	return reconcile.Result{}, nil

	if merge.Status.ResolveConflictBranch != nil && len(add) > 0 {
		r.addBranchesToStatus(merge, add)
		err = r.setResolveBranch(projectPID, merge)
		if err != nil {
			conditions.MarkFalse(merge, releasev1alpha1.ResolveConflictBranchesReadyCondition, releasev1alpha1.ResolveConflictBranchesReason,
				clusterv1.ConditionSeverityError,
				"Create new conflict branch error: %s %s %s", projectPID, buildName, err)
			return ctrl.Result{Requeue: true}, err
		}
	}

	if merge.Status.ResolveConflictBranch != nil && merge.Status.ResolveConflictBranch.IsValid == False {
		logger.Info("Wait for resolve conflict branch " + merge.Status.ResolveConflictBranch.ResolveBranch)

		b, exist, err := r.App.GitClient.GetBranch(projectPID, merge.Status.ResolveConflictBranch.Name)
		if err != nil {
			conditions.MarkFalse(merge, releasev1alpha1.ResolveConflictBranchesReadyCondition, releasev1alpha1.ResolveConflictBranchesReason,
				clusterv1.ConditionSeverityError,
				"Conflict branch error: %s %s %s %s", projectPID, buildName, merge.Status.ResolveConflictBranch.Name, err)
			return ctrl.Result{Requeue: true}, err
		}
		if !exist {
			conditions.MarkFalse(merge, releasev1alpha1.ResolveConflictBranchesReadyCondition, releasev1alpha1.ResolveConflictBranchesReason,
				clusterv1.ConditionSeverityError,
				"Conflict branch not exist: %s %s %s", projectPID, buildName, merge.Status.ResolveConflictBranch.Name)
			return ctrl.Result{Requeue: true}, nil
		}

		if !strings.Contains(b.Commit.Message, OkCommitMessage) {
			logger.Info("User not resolve conflicts yet " + merge.Status.ResolveConflictBranch.ResolveBranch)
			return ctrl.Result{RequeueAfter: time.Second * 3}, err
		}
		merge.Status.ResolveConflictBranch.IsValid = True
		merge.Status.ResolveConflictBranch.FailureMessage = nil
	}

	err = r.App.GitClient.GetOrCreateBranch(projectPID, buildName)
	if err != nil {
		conditions.MarkFalse(merge, releasev1alpha1.ReleaseBranchReadyCondition, releasev1alpha1.ReleaseBranchReason,
			clusterv1.ConditionSeverityError,
			"Get or create branch error: URL %s %s", merge.Spec.Repo.URL, err)
		return ctrl.Result{Requeue: true}, err
	}
	merge.Status.BuildBranch = buildName
	conditions.MarkTrue(merge, releasev1alpha1.ReleaseBranchReadyCondition)
	r.addBranchesToStatus(merge, add)

	hasConflict := false
	for i := range merge.Status.Branches {
		b := &merge.Status.Branches[i]
		currentBranch := makeCurrentBranches(b, merge)

		if currentBranch.MainBranch.IsValid != True {
			continue
		}
		if currentBranch.MainBranch.IsMerged == True {
			currentBranch.AliasBranch.IsMerged = True
			continue
		}

		mr, err := r.App.GitClient.GetOrCreateMR(projectPID, currentBranch.MainBranch.Name, buildName, parseIID(currentBranch.MainBranch.MergeRequestID))
		if err != nil {
			conditions.MarkFalse(merge, releasev1alpha1.AllBranchMergedCondition, releasev1alpha1.AllBranchMergedReason,
				clusterv1.ConditionSeverityWarning,
				"GetOrCreate MR Error: %s %s %s", currentBranch.MainBranch.Name, buildName, err)
			return ctrl.Result{Requeue: true}, err
		}
		currentBranch.MainBranch.MergeRequestID = fmt.Sprint(mr.IID)

		switch {
		//lint:ignore
		case mr.MergeStatus == "can_be_merged", mr.DetailedMergeStatus == "mergeable":

			err := r.App.GitClient.AcceptMR(projectPID, mr.IID)
			if err != nil {
				conditions.MarkFalse(merge, releasev1alpha1.AllBranchMergedCondition, releasev1alpha1.AllBranchMergedReason,
					clusterv1.ConditionSeverityWarning,
					"AcceptMR MR Error: %s %s %s %s", projectPID, buildName, mr.SourceBranch, err)
				return ctrl.Result{Requeue: true}, err
			}
			currentBranch.MainBranch.IsMerged = True

		//lint:ignore
		case mr.MergeStatus == "cannot_be_merged", mr.DetailedMergeStatus == "broken_status":
			hasConflict = true
		}
	}

	if hasConflict {
		for i := range merge.Status.Branches {
			b := &merge.Status.Branches[i]
			err := r.App.GitClient.RemoveMRIfExist(projectPID, parseIID(b.MergeRequestID))
			if err != nil {
				logger.Error(err, "Remove trash MR error")
			}
		}
		err = r.App.GitClient.RemoveBranch(projectPID, buildName)
		if err != nil {
			conditions.MarkFalse(merge, releasev1alpha1.ReleaseBranchReadyCondition, releasev1alpha1.ReleaseBranchReason,
				clusterv1.ConditionSeverityError,
				"Remove branch when conflict error: URL %s %s", merge.Spec.Repo.URL, err)
		}
		err := r.setResolveBranch(projectPID, merge)
		if err != nil {
			conditions.MarkFalse(merge, releasev1alpha1.ResolveConflictBranchesReadyCondition, releasev1alpha1.ResolveConflictBranchesReason,
				clusterv1.ConditionSeverityError,
				"Create conflict branch error: %s %s %s", projectPID, buildName, err)
			return ctrl.Result{Requeue: true}, err
		}
	}

	for i := range merge.Status.Branches {
		b := &merge.Status.Branches[i]
		if b.IsMerged == False {
			logger.Info("Wait for all branches merge " + projectPID)
			return reconcile.Result{RequeueAfter: time.Second * 3}, nil
		}
	}

	return reconcile.Result{}, nil
}

func (r *MergeReconciler) getOrCreateNativeMR(
	ctx context.Context,
	name string,
	merge *releasev1alpha1.Merge,
	spec releasev1alpha1.NativeMergeRequestSpec,
) (*releasev1alpha1.NativeMergeRequest, error) {

	nmr := new(releasev1alpha1.NativeMergeRequest)
	exist, err := r.getObject(ctx, name, nmr)
	if err != nil {
		return nil, err
	}
	if exist {
		return nmr, nil
	}
	newNmr := new(releasev1alpha1.NativeMergeRequest)
	newNmr.Namespace = merge.Namespace
	newNmr.GenerateName = fmt.Sprintf("%s-%s-%s-", spec.TargetBranch, merge.Name, spec.SourceBranch)
	newNmr.Spec = spec

	err = r.Create(ctx, newNmr)
	if err != nil {
		return nil, err
	}

	return newNmr, nil
}

func (r *MergeReconciler) getObject(ctx context.Context, name string, obj client.Object) (bool, error) {
	err := r.Get(ctx, client.ObjectKey{Name: name}, obj)
	if err != nil {
		switch v := err.(type) {
		case apierrors.APIStatus:
			if v.Status().Code == 404 {
				return false, nil
			}
			return false, err
		default:
			return false, err
		}
	}
	return true, nil
}

func parseIID(strVar string) int {
	intVar, err := strconv.Atoi(strVar)
	if err != nil {
		intVar = 0
	}
	return intVar
}

func (r *MergeReconciler) setResolveBranch(projectPID string, merge *releasev1alpha1.Merge) error {
	resolveBranchNameSlice := make([]string, 0, len(merge.Status.Branches))
	for i := range merge.Status.Branches {
		b := &merge.Status.Branches[i]
		resolveBranchNameSlice = append(resolveBranchNameSlice, b.Name)
	}
	resolveBranchName := strings.Join(resolveBranchNameSlice, "_")
	err := r.App.GitClient.GetOrCreateBranch(projectPID, resolveBranchName)
	if err != nil {
		return err
	}
	for i := range merge.Status.Branches {
		b := &merge.Status.Branches[i]
		b.ResolveBranch = resolveBranchName
		b.IsMerged = False
		b.MergeRequestID = fmt.Sprint(0)
	}
	msg := "Conflict not resolve yet. Need commit message - " + OkCommitMessage
	merge.Status.ResolveConflictBranch = &releasev1alpha1.BranchStatus{
		Name:           resolveBranchName,
		IsMerged:       False,
		ResolveBranch:  resolveBranchName,
		IsValid:        False,
		FailureMessage: &msg,
	}
	return nil
}

func (r *MergeReconciler) addBranchesToStatus(merge *releasev1alpha1.Merge, branches []string) {
	for _, b := range branches {
		merge.Status.Branches = append(merge.Status.Branches, releasev1alpha1.BranchStatus{
			Name:     b,
			IsMerged: False,
		})
	}

	for i := range merge.Status.Branches {
		b := &merge.Status.Branches[i]
		b.IsValid = False
		_, exist, err := r.App.GitClient.GetBranch(merge.Status.ProjectPID, b.Name)
		if err != nil {
			currentError := err.Error()
			b.FailureMessage = &currentError
		}
		if !exist {
			currentError := fmt.Sprintf("Branch %s not exist ", b.Name)
			b.FailureMessage = &currentError
		}

		if exist {
			b.IsValid = True
		}
	}
}

func getBranchesNames(branches []releasev1alpha1.Branch) []string {
	result := make([]string, 0, len(branches))
	for _, branch := range branches {
		result = append(result, branch.Name)
	}
	return result
}

func getStatusBranchesNames(branches []releasev1alpha1.BranchStatus) []string {
	result := make([]string, 0, len(branches))
	for _, branch := range branches {
		result = append(result, branch.Name)
	}
	return result
}

func (r *MergeReconciler) reconcileDelete(ctx context.Context, merge *releasev1alpha1.Merge) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("reconcileDelete")

	isRemoved := controllerutil.RemoveFinalizer(merge, releasev1alpha1.MergeFinalizer)
	if !isRemoved {
		return reconcile.Result{}, fmt.Errorf("connot remove finalizer")
	}
	logger.Info("Remove Finalizer")
	return reconcile.Result{}, nil
}

func patchMerge(ctx context.Context, patchHelper *patch.Helper, merge *releasev1alpha1.Merge, options ...patch.Option) error {
	// Always update the readyCondition by summarizing the state of other conditions.
	applicableConditions := []clusterv1.ConditionType{
		releasev1alpha1.RepositoriesReadyCondition,
		releasev1alpha1.ReleaseBranchReadyCondition,
		releasev1alpha1.AllBranchesExistCondition,
		releasev1alpha1.AllBranchMergedCondition,
	}

	if merge.Status.ResolveConflictBranch != nil {
		applicableConditions = append(applicableConditions, releasev1alpha1.ResolveConflictBranchesReadyCondition)
	}

	conditions.SetSummary(merge,
		conditions.WithConditions(applicableConditions...),
	)
	// Patch the object, ignoring conflicts on the conditions owned by this controller.
	// Also, if requested, we are adding additional options like e.g. Patch ObservedGeneration when issuing the
	// patch at the end of the reconcile loop.
	options = append(options,
		patch.WithOwnedConditions{
			Conditions: []clusterv1.ConditionType{},
		},
	)

	return patchHelper.Patch(ctx, merge, options...)
}

// SetupWithManager sets up the controller with the Manager.
func (r *MergeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&releasev1alpha1.Merge{}).
		Complete(r)
}
