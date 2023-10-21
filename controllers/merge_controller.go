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
	"sigs.k8s.io/controller-runtime/pkg/controller"
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

	releasev1alpha1 "github.com/dhnikolas/release-operator/api/v1alpha1"
	"github.com/dhnikolas/release-operator/internal/app"
	"github.com/blang/semver"
)

const OkCommitMessage = "okok"
const MainBranchName = "main"

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

	if len(add) > 0 || len(remove) > 0 {
		err := r.removeConflictStateIfExist(ctx, merge)
		if err != nil {
			logger.Error(err, "Cannot remove conflict state")
			return ctrl.Result{Requeue: true}, err
		}
		err = r.deleteFinalMRIfExist(ctx, merge)
		if err != nil {
			logger.Error(err, "Cannot remove final MR")
			return ctrl.Result{Requeue: true}, err
		}
	}

	if len(remove) > 0 {
		err = r.App.GitClient.RemoveBranch(projectPID, buildName)
		if err != nil {
			conditions.MarkFalse(merge, releasev1alpha1.ReleaseBranchReadyCondition, releasev1alpha1.ReleaseBranchReason,
				clusterv1.ConditionSeverityError,
				"Recreate branch error: URL %s %s", merge.Spec.Repo.URL, err)
			return ctrl.Result{Requeue: true}, err
		}
		r.deleteBranchesNativeMR(ctx, merge)
		merge.Status.BuildBranch = buildName
		add = branches
		merge.Status.Branches = make([]releasev1alpha1.BranchStatus, 0)
	}

	r.addBranchesToStatus(merge, add)
	err = r.App.GitClient.GetOrCreateBranch(projectPID, buildName)
	if err != nil {
		conditions.MarkFalse(merge, releasev1alpha1.ReleaseBranchReadyCondition, releasev1alpha1.ReleaseBranchReason,
			clusterv1.ConditionSeverityError,
			"Create branch error: URL %s %s", merge.Spec.Repo.URL, err)
		return ctrl.Result{Requeue: true}, err
	}

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
		nmr, err := r.getOrCreateNativeMR(ctx, b.MergeRequestName, merge, nmrSpec)
		if err != nil {
			return ctrl.Result{Requeue: true}, err
		}
		b.MergeRequestID = nmr.Status.IID
		b.MergeRequestName = nmr.Name
		b.IsConflict = nmr.Status.HasConflict
		b.IsMerged = nmr.Status.Ready
		b.Processed = conditions.IsTrue(nmr, clusterv1.ReadyCondition)
	}

	notValidBranches := merge.Status.NotValidBranches()
	if len(notValidBranches) > 0 {
		logger.Info("Wait for all branches ready " + projectPID)
		conditions.MarkFalse(merge, releasev1alpha1.AllBranchesExistCondition, releasev1alpha1.BranchExistReason,
			clusterv1.ConditionSeverityInfo,
			"Branches not valid %s ", strings.Join(notValidBranches, ", "))
		return reconcile.Result{RequeueAfter: time.Second * 3}, nil
	}
	conditions.MarkTrue(merge, releasev1alpha1.AllBranchesExistCondition)

	if !merge.Status.BranchesAlreadyProcessed() {
		logger.Info("Wait for all branches processed " + projectPID)
		conditions.MarkFalse(merge, releasev1alpha1.AllBranchMergedCondition, releasev1alpha1.MergesNotReadyReason,
			clusterv1.ConditionSeverityInfo,
			"Wait for all branches merge %s ", projectPID)
		return reconcile.Result{RequeueAfter: time.Second * 3}, nil
	}

	if merge.Status.HasConflict() {
		logger.Info("Reconcile conflict " + projectPID)
		conflictResolved, err := r.reconcileConflict(ctx, merge)
		if err != nil {
			return reconcile.Result{}, err
		}
		if !conflictResolved {
			return reconcile.Result{RequeueAfter: time.Second * 5}, nil
		}
		logger.Info("Conflict resolved " + projectPID)
	}

	tagCreated, err := r.createTag(ctx, merge)
	if err != nil {
		return reconcile.Result{}, err
	}
	if !tagCreated {
		logger.Info("Tag not created yet " + projectPID)
		return reconcile.Result{RequeueAfter: time.Second * 3}, nil
	}

	err = r.createFinalMR(ctx, merge)
	if err != nil {
		return reconcile.Result{RequeueAfter: time.Second * 3}, err
	}

	conditions.MarkTrue(merge, releasev1alpha1.AllBranchMergedCondition)
	logger.Info("MR successful merged " + projectPID)
	return reconcile.Result{}, nil
}

func (r *MergeReconciler) reconcileConflict(ctx context.Context, merge *releasev1alpha1.Merge) (bool, error) {
	projectPID := merge.Status.ProjectPID
	buildName := merge.Labels[releasev1alpha1.BuildNameLabel]
	if !merge.Status.ConflictState.RecreatedBuildBranch {
		err := r.App.GitClient.RecreateBranch(projectPID, buildName)
		if err != nil {
			conditions.MarkFalse(merge, releasev1alpha1.ReleaseBranchReadyCondition, releasev1alpha1.ReleaseBranchReason,
				clusterv1.ConditionSeverityError,
				"Recreate branch error: URL %s %s", merge.Spec.Repo.URL, err)
			return false, err
		}
		merge.Status.ConflictState.RecreatedBuildBranch = true
	}
	currentResolveBranchName := r.getResolveBranchName(merge)
	err := r.App.GitClient.GetOrCreateBranch(projectPID, currentResolveBranchName)
	if err != nil {
		conditions.MarkFalse(merge, releasev1alpha1.ReleaseBranchReadyCondition, releasev1alpha1.ReleaseBranchReason,
			clusterv1.ConditionSeverityError,
			"Get or create resolve branch error: URL %s %s", merge.Spec.Repo.URL, err)
		return false, err
	}

	nmrSpec := releasev1alpha1.NativeMergeRequestSpec{
		ProjectID:                projectPID,
		Title:                    "release-operator",
		SourceBranch:             currentResolveBranchName,
		TargetBranch:             buildName,
		Labels:                   []string{"release-operator"},
		CheckSourceBranchMessage: OkCommitMessage,
		AutoAccept:               true,
	}
	nmr, err := r.getOrCreateNativeMR(ctx, merge.Status.ConflictState.ResolveBranch.MergeRequestName, merge, nmrSpec)
	if err != nil {
		conditions.MarkFalse(merge, releasev1alpha1.ResolveConflictBranchesReadyCondition, releasev1alpha1.ResolveConflictBranchesReason,
			clusterv1.ConditionSeverityError,
			"Create MR for conflict resolve branch error: URL %s %s", merge.Spec.Repo.URL, err)
		return false, err
	}
	merge.Status.ConflictState.ResolveBranch.Name = currentResolveBranchName
	merge.Status.ConflictState.ResolveBranch.MergeRequestID = nmr.Status.IID
	merge.Status.ConflictState.ResolveBranch.MergeRequestName = nmr.Name
	merge.Status.ConflictState.ResolveBranch.IsConflict = nmr.Status.HasConflict
	merge.Status.ConflictState.ResolveBranch.IsMerged = nmr.Status.Ready
	merge.Status.ConflictState.ResolveBranch.Processed = conditions.IsTrue(nmr, clusterv1.ReadyCondition)

	if !merge.Status.ConflictState.ResolveBranch.IsMerged {
		conditions.MarkFalse(merge, releasev1alpha1.ResolveConflictBranchesReadyCondition, releasev1alpha1.ResolveConflictBranchesReason,
			clusterv1.ConditionSeverityWarning,
			"Wait for resolve conflict: URL %s %s", merge.Spec.Repo.URL, err)
		return false, nil
	}
	conditions.MarkTrue(merge, releasev1alpha1.ResolveConflictBranchesReadyCondition)

	return true, nil
}

func (r *MergeReconciler) removeConflictStateIfExist(ctx context.Context, merge *releasev1alpha1.Merge) error {
	projectPID := merge.Status.ProjectPID
	if len(merge.Status.ConflictState.ResolveBranch.Name) > 0 {
		err := r.App.GitClient.RemoveBranch(projectPID, merge.Status.ConflictState.ResolveBranch.Name)
		if err != nil {
			return err
		}
	}
	if len(merge.Status.ConflictState.ResolveBranch.MergeRequestName) > 0 {
		err := r.deleteNativeMR(ctx, merge, merge.Status.ConflictState.ResolveBranch.MergeRequestName)
		if err != nil {
			return err
		}
	}
	merge.Status.ConflictState = releasev1alpha1.ConflictState{}

	return nil
}

func (r *MergeReconciler) getResolveBranchName(merge *releasev1alpha1.Merge) string {
	resolveBranchNameSlice := make([]string, 0, len(merge.Status.Branches))
	for i := range merge.Status.Branches {
		b := &merge.Status.Branches[i]
		resolveBranchNameSlice = append(resolveBranchNameSlice, b.Name)
	}

	return "c_" + strings.Join(resolveBranchNameSlice, "_")
}

func (r *MergeReconciler) deleteBranchesNativeMR(ctx context.Context, merge *releasev1alpha1.Merge) {
	for i := range merge.Status.Branches {
		b := &merge.Status.Branches[i]
		if b.MergeRequestName == "" {
			continue
		}
		r.deleteNativeMR(ctx, merge, b.MergeRequestName)
		b.MergeRequestName = ""
		b.MergeRequestID = ""
	}
}

func (r *MergeReconciler) setNewResolveBranch(merge *releasev1alpha1.Merge, name, id string) {
	for i := range merge.Status.Branches {
		b := &merge.Status.Branches[i]
		b.MergeRequestName = name
		b.MergeRequestID = id
	}
}

func (r *MergeReconciler) getOrCreateNativeMR(
	ctx context.Context,
	name string,
	merge *releasev1alpha1.Merge,
	spec releasev1alpha1.NativeMergeRequestSpec,
) (*releasev1alpha1.NativeMergeRequest, error) {

	logger := log.FromContext(ctx)
	nmr := new(releasev1alpha1.NativeMergeRequest)
	nmr.Name = name
	nmr.Namespace = merge.Namespace
	if name != "" {
		exist, err := r.getObject(ctx, name, nmr)
		if err != nil {
			return nil, err
		}
		if exist {
			return nmr, nil
		}
	}
	if name != "" {
		logger.Info("NOT EXIST MR  " + nmr.Name)
	}
	newNmr := new(releasev1alpha1.NativeMergeRequest)
	newNmr.Namespace = merge.Namespace
	newNmr.GenerateName = fmt.Sprintf("%s-%s-", spec.TargetBranch, merge.Name)
	newNmr.Spec = spec
	err := controllerutil.SetControllerReference(merge, newNmr, r.Scheme)
	if err != nil {
		return nil, err
	}

	err = r.Create(ctx, newNmr)
	if err != nil {
		return nil, err
	}
	logger.Info("CREATE MR NOW " + newNmr.Name)

	return newNmr, nil
}

func (r *MergeReconciler) createTag(ctx context.Context, merge *releasev1alpha1.Merge) (bool, error) {
	projectPID := merge.Status.ProjectPID
	buildName := merge.Labels[releasev1alpha1.BuildNameLabel]

	buildBranch, exist, err := r.App.GitClient.GetBranch(projectPID, buildName)
	if !exist || err != nil {
		conditions.MarkFalse(merge, releasev1alpha1.TagCondition, releasev1alpha1.TagReason,
			clusterv1.ConditionSeverityError,
			"Build branch error: URL %s %s", merge.Spec.Repo.URL, err)
		return false, err
	}

	mainBranch, exist, err := r.App.GitClient.GetBranch(projectPID, MainBranchName)
	if !exist || err != nil {
		conditions.MarkFalse(merge, releasev1alpha1.TagCondition, releasev1alpha1.TagReason,
			clusterv1.ConditionSeverityError,
			"Main branch error: URL %s %s", merge.Spec.Repo.URL, err)
		return false, err
	}

	if mainBranch.Commit.ID == buildBranch.Commit.ID {
		conditions.MarkFalse(merge, releasev1alpha1.TagCondition, releasev1alpha1.TagReason,
			clusterv1.ConditionSeverityWarning,
			"The main branch and the build branch are still the same %s", merge.Spec.Repo.URL)

		return false, nil
	}

	err = r.createFinalTag(ctx, merge)
	if err != nil {
		conditions.MarkFalse(merge, releasev1alpha1.TagCondition, releasev1alpha1.TagReason,
			clusterv1.ConditionSeverityError,
			"Create tag error: URL %s %s", merge.Spec.Repo.URL, err)
		return false, err
	}
	conditions.MarkTrue(merge, releasev1alpha1.TagCondition)

	return true, nil
}

func (r *MergeReconciler) createFinalMR(ctx context.Context, merge *releasev1alpha1.Merge) error {
	projectPID := merge.Status.ProjectPID
	buildName := merge.Labels[releasev1alpha1.BuildNameLabel]

	nmrSpec := releasev1alpha1.NativeMergeRequestSpec{
		ProjectID:    projectPID,
		Title:        "release-operator",
		SourceBranch: buildName,
		TargetBranch: MainBranchName,
		Labels:       []string{"release-operator"},
		AutoAccept:   false,
	}
	nmr, err := r.getOrCreateNativeMR(ctx, merge.Status.FinalMR, merge, nmrSpec)
	if err != nil {
		conditions.MarkFalse(merge, releasev1alpha1.FinalMRCondition, releasev1alpha1.FinalMRReason,
			clusterv1.ConditionSeverityError,
			"Create final MR error: URL %s %s", merge.Spec.Repo.URL, err)
		return err
	}
	merge.Status.FinalMR = nmr.Name
	conditions.MarkTrue(merge, releasev1alpha1.FinalMRCondition)

	return nil
}

func (r *MergeReconciler) deleteFinalMRIfExist(ctx context.Context, merge *releasev1alpha1.Merge) error {
	if merge.Status.FinalMR == "" {
		return nil
	}

	err := r.deleteNativeMR(ctx, merge, merge.Status.FinalMR)
	if err != nil {
		return err
	}

	merge.Status.FinalMR = ""
	merge.Status.FinalTag = ""

	return nil
}
func (r *MergeReconciler) createFinalTag(ctx context.Context, merge *releasev1alpha1.Merge) error {
	buildName := merge.Labels[releasev1alpha1.BuildNameLabel]
	baseTag := "v0.0.1"
	projectPID := merge.Status.ProjectPID
	finalTag := merge.Status.FinalTag
	if finalTag == "" {
		tags, exist, err := r.App.GitClient.ListTags(projectPID)
		if err != nil {
			return err
		}
		if !exist {
			finalTag = baseTag
		} else {
			versions := make([]string, 0, len(tags))
			for _, tag := range tags {
				versions = append(versions, tag.Name)
			}
			currentVersion, err := getLatestVersion(versions)
			if err != nil {
				return err
			}
			version, err := semver.Parse(currentVersion)
			if err != nil {
				return err
			}
			version.Patch++
			finalTag = "v" + version.String()
		}
	}

	tag, err := r.App.GitClient.GetOrCreateTag(projectPID, finalTag, buildName)
	if err != nil {
		return err
	}

	merge.Status.FinalTag = tag.Name

	return err
}

func (r *MergeReconciler) deleteNativeMR(ctx context.Context, merge *releasev1alpha1.Merge, name string) error {
	newNmr := new(releasev1alpha1.NativeMergeRequest)
	newNmr.Namespace = merge.Namespace
	newNmr.Name = name
	exist, err := r.getObject(ctx, name, newNmr)
	if err != nil {
		return err
	}
	if !exist {
		return nil
	}
	err = r.Client.Delete(ctx, newNmr)
	if err != nil {
		return err
	}
	return nil
}

func getLatestVersion(versions []string) (string, error) {
	v := make([]semver.Version, 0, len(versions))
	for _, version := range versions {
		sv, err := semver.ParseTolerant(version)
		if err != nil {
			return "", err
		}
		v = append(v, sv)
	}
	if len(v) == 0 {
		return "", errors.New("no versions in slice ")
	}
	semver.Sort(v)
	latest := v[len(v)-1]

	return latest.String(), nil
}

func (r *MergeReconciler) getObject(ctx context.Context, name string, obj client.Object) (bool, error) {
	err := r.Get(ctx, client.ObjectKey{Name: name, Namespace: obj.GetNamespace()}, obj)
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

func (r *MergeReconciler) addBranchesToStatus(merge *releasev1alpha1.Merge, branches []string) {
	for _, b := range branches {
		merge.Status.Branches = append(merge.Status.Branches, releasev1alpha1.BranchStatus{
			Name:     b,
			IsMerged: false,
		})
	}

	for i := range merge.Status.Branches {
		b := &merge.Status.Branches[i]
		b.IsValid = false
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
			b.IsValid = true
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

	if merge.Status.HasConflict() {
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
		For(&releasev1alpha1.Merge{}).WithOptions(controller.Options{MaxConcurrentReconciles: 1}).
		Complete(r)
}
