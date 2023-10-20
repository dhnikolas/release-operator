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

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"fmt"
	releasev1alpha1 "github.com/dhnikolas/release-operator/api/v1alpha1"
	"github.com/dhnikolas/release-operator/internal/app"
	"github.com/pkg/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/cluster-api/util/conditions"
	"sigs.k8s.io/cluster-api/util/patch"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"strings"
	"time"
)

// NativeMergeRequestReconciler reconciles a NativeMergeRequest object
type NativeMergeRequestReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	App    *app.App
}

//+kubebuilder:rbac:groups=release.salt.x5.ru,resources=nativemergerequests,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=release.salt.x5.ru,resources=nativemergerequests/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=release.salt.x5.ru,resources=nativemergerequests/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the NativeMergeRequest object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.12.2/pkg/reconcile
func (r *NativeMergeRequestReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, reterr error) {
	logger := log.FromContext(ctx)

	logger.Info("Reconcile NativeMergeRequest " + req.Name)

	nativeMerge := &releasev1alpha1.NativeMergeRequest{}
	err := r.Client.Get(ctx, req.NamespacedName, nativeMerge)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}
		return reconcile.Result{Requeue: true}, err
	}
	patchHelper, err := patch.NewHelper(nativeMerge, r.Client)
	if err != nil {
		return ctrl.Result{}, err
	}

	defer func() {
		if err := patchNativeMerge(ctx, patchHelper, nativeMerge); err != nil {
			if reterr == nil {
				reterr = errors.Wrapf(err, "error patching Build %s/%s", nativeMerge.Namespace, nativeMerge.Name)
			}
			logger.Error(err, "Patch Merge error")
		}
	}()

	if !nativeMerge.GetDeletionTimestamp().IsZero() {
		return r.reconcileDelete(ctx, nativeMerge)
	}

	return r.reconcileNormal(ctx, nativeMerge)
}

func (r *NativeMergeRequestReconciler) reconcileNormal(ctx context.Context, nativeMerge *releasev1alpha1.NativeMergeRequest) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	if !controllerutil.ContainsFinalizer(nativeMerge, releasev1alpha1.NativeMergeFinalizer) {
		if controllerutil.AddFinalizer(nativeMerge, releasev1alpha1.NativeMergeFinalizer) {
			logger.Info("Finalizer not add " + releasev1alpha1.NativeMergeFinalizer)
			return reconcile.Result{Requeue: true}, nil
		}
	}

	if nativeMerge.Status.Deleted {
		logger.Info("Already deleted")
		return ctrl.Result{}, nil
	}

	sourceBranch, branchExist, err := r.App.GitClient.GetBranch(nativeMerge.Spec.ProjectID, nativeMerge.Spec.SourceBranch)
	if err != nil {
		conditions.MarkFalse(nativeMerge, releasev1alpha1.BranchExistCondition, releasev1alpha1.BranchExistReason,
			clusterv1.ConditionSeverityError,
			"Get source branch branch error: URL %s %s", nativeMerge.Spec.SourceBranch, err)
		return ctrl.Result{Requeue: true}, err
	}
	if !branchExist {
		conditions.MarkFalse(nativeMerge, releasev1alpha1.BranchExistCondition, releasev1alpha1.BranchExistReason,
			clusterv1.ConditionSeverityError,
			"source branch not exist %s", nativeMerge.Spec.SourceBranch)
		return ctrl.Result{RequeueAfter: time.Second * 20}, err
	}
	conditions.MarkTrue(nativeMerge, releasev1alpha1.BranchExistCondition)

	if nativeMerge.Spec.CheckSourceBranchMessage != "" {
		if !strings.Contains(sourceBranch.Commit.Message, nativeMerge.Spec.CheckSourceBranchMessage) {
			conditions.MarkFalse(nativeMerge, releasev1alpha1.BranchCommitCondition, releasev1alpha1.BranchCommitReady,
				clusterv1.ConditionSeverityError,
				"Branch not have special commit message yet %s", nativeMerge.Spec.SourceBranch)
			return ctrl.Result{RequeueAfter: time.Second * 3}, err
		}
		conditions.MarkTrue(nativeMerge, releasev1alpha1.BranchCommitCondition)

	}

	mr, err := r.App.GitClient.GetOrCreateMR(
		nativeMerge.Spec.ProjectID,
		nativeMerge.Spec.SourceBranch,
		nativeMerge.Spec.TargetBranch,
		parseIID(nativeMerge.Status.IID))
	if err != nil {
		conditions.MarkFalse(nativeMerge, releasev1alpha1.NativeMergeRequestCondition, releasev1alpha1.AllBranchMergedReason,
			clusterv1.ConditionSeverityWarning,
			"GetOrCreate MR Error: %s %s %s", nativeMerge.Spec.SourceBranch, nativeMerge.Spec.TargetBranch, err)
		return ctrl.Result{RequeueAfter: time.Second * 5}, err
	}
	nativeMerge.Status.IID = fmt.Sprint(mr.IID)

	if !nativeMerge.Spec.AutoAccept {
		return ctrl.Result{}, nil
	}

	switch {
	case mr.State == "merged", mr.State == "closed":
		conditions.MarkTrue(nativeMerge, releasev1alpha1.AllBranchMergedCondition)
		return ctrl.Result{}, nil
	//lint:ignore
	case mr.MergeStatus == "can_be_merged", mr.DetailedMergeStatus == "mergeable":
		err := r.App.GitClient.AcceptMR(nativeMerge.Spec.ProjectID, mr.IID)
		if err != nil {
			conditions.MarkFalse(nativeMerge, releasev1alpha1.NativeMergeRequestCondition, releasev1alpha1.AllBranchMergedReason,
				clusterv1.ConditionSeverityWarning,
				"AcceptMR MR Error: %s %s %s %s",
				nativeMerge.Spec.ProjectID,
				mr.SourceBranch,
				mr.TargetBranch, err)
			return ctrl.Result{RequeueAfter: time.Second * 5}, err
		}
		nativeMerge.Status.Ready = true

	//lint:ignore
	case mr.MergeStatus == "cannot_be_merged", mr.DetailedMergeStatus == "broken_status":
		nativeMerge.Status.HasConflict = true
		err := r.App.GitClient.RemoveMR(nativeMerge.Spec.ProjectID, mr.IID)
		if err != nil {
			return reconcile.Result{Requeue: true}, err
		}
		nativeMerge.Status.Deleted = true
		conditions.MarkTrue(nativeMerge, releasev1alpha1.NativeMergeRequestCondition)
		return ctrl.Result{}, nil
	}

	return ctrl.Result{RequeueAfter: time.Second * 5}, nil
}

func (r *NativeMergeRequestReconciler) reconcileDelete(ctx context.Context, nativeMerge *releasev1alpha1.NativeMergeRequest) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("reconcileDelete")

	logger.Info("Remove Finalizer")

	mr, exist, err := r.App.GitClient.GetMergeRequest(nativeMerge.Spec.ProjectID, parseIID(nativeMerge.Status.IID))
	if err != nil {
		return reconcile.Result{Requeue: true}, err
	}

	if exist && mr.State != "closed" && mr.State != "merged" {
		err := r.App.GitClient.RemoveMR(nativeMerge.Spec.ProjectID, mr.IID)
		if err != nil {
			return reconcile.Result{Requeue: true}, err
		}
	}

	isRemoved := controllerutil.RemoveFinalizer(nativeMerge, releasev1alpha1.NativeMergeFinalizer)
	if !isRemoved {
		return reconcile.Result{}, fmt.Errorf("connot remove finalizer")
	}

	return reconcile.Result{}, nil
}

func patchNativeMerge(ctx context.Context, patchHelper *patch.Helper, nativeMerge *releasev1alpha1.NativeMergeRequest, options ...patch.Option) error {
	// Always update the readyCondition by summarizing the state of other conditions.
	applicableConditions := []clusterv1.ConditionType{
		releasev1alpha1.BranchExistCondition,
		releasev1alpha1.NativeMergeRequestCondition,
		releasev1alpha1.BranchCommitCondition,
	}

	conditions.SetSummary(nativeMerge,
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

	return patchHelper.Patch(ctx, nativeMerge, options...)
}

// SetupWithManager sets up the controller with the Manager.
func (r *NativeMergeRequestReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&releasev1alpha1.NativeMergeRequest{}).
		Complete(r)
}
