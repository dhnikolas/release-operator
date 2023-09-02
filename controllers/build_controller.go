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
	"path"

	"github.com/pkg/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/cluster-api/util/conditions"
	"sigs.k8s.io/cluster-api/util/patch"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	releasev1alpha1 "github.com/dhnikolas/release-operator/api/v1alpha1"
	"github.com/dhnikolas/release-operator/internal/app"
)

const (
	True  = "True"
	False = "False"
)

// BuildReconciler reconciles a Build object.
type BuildReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	App    *app.App
}

//+kubebuilder:rbac:groups=release.salt.x5.ru,resources=builds,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=release.salt.x5.ru,resources=builds/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=release.salt.x5.ru,resources=builds/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Build object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.12.2/pkg/reconcile
func (r *BuildReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, reterr error) {
	logger := log.FromContext(ctx)

	logger.Info("Start reconcile Build")

	build := &releasev1alpha1.Build{}
	err := r.Client.Get(ctx, req.NamespacedName, build)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}
		return reconcile.Result{Requeue: true}, err
	}

	patchHelper, err := patch.NewHelper(build, r.Client)
	if err != nil {
		return ctrl.Result{}, err
	}
	defer func() {
		if err := patchBuild(ctx, patchHelper, build); err != nil {
			if reterr == nil {
				reterr = errors.Wrapf(err, "error patching Build %s/%s", build.Namespace, build.Name)
			}
			logger.Error(err, "Patch Build error")
		}
	}()

	labelSelector := &v1.LabelSelector{}
	labelSelector.MatchLabels = map[string]string{}
	labelSelector.MatchLabels[releasev1alpha1.BuildNameLabel] = build.Name
	allMerges := &releasev1alpha1.MergeList{}
	err = r.Client.List(ctx,
		allMerges,
		client.InNamespace(build.Namespace),
		client.MatchingLabels(labelSelector.MatchLabels),
	)
	if err != nil {
		conditions.MarkFalse(build,
			releasev1alpha1.BuildSyncedCondition,
			releasev1alpha1.NotSyncedReason,
			clusterv1.ConditionSeverityError,
			"Cannot get MergeList")
		return ctrl.Result{Requeue: true}, err
	}

	if !build.GetDeletionTimestamp().IsZero() {
		return r.reconcileDelete(ctx, build)
	}

	return r.reconcileNormal(ctx, build, allMerges)
}

func (r *BuildReconciler) reconcileNormal(ctx context.Context, build *releasev1alpha1.Build, allMerges *releasev1alpha1.MergeList) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	if !controllerutil.ContainsFinalizer(build, releasev1alpha1.BuildFinalizer) {
		if controllerutil.AddFinalizer(build, releasev1alpha1.BuildFinalizer) {
			logger.Info("Finalizer not add")
			return reconcile.Result{Requeue: true}, nil
		}
	}

	logger.Info("Reconcile normal ")
	toCreate, remove, toUpdate := getFullReposDiff(build.Spec.Repos, allMerges)

	if len(toCreate) > 0 {
		for _, repo := range toCreate {
			newMerge := &releasev1alpha1.Merge{
				Spec: releasev1alpha1.MergeSpec{Repo: repo},
			}
			newMerge.Namespace = build.Namespace
			newMerge.Labels = map[string]string{}
			newMerge.Labels[releasev1alpha1.BuildNameLabel] = build.Name
			newMerge.Name = mergeName(build.Name, repo.URL)
			err := controllerutil.SetControllerReference(build, newMerge, r.Scheme)
			if err != nil {
				logger.Error(err, "Ref error ")
			}
			err = r.Client.Create(ctx, newMerge)
			if err != nil {
				conditions.MarkFalse(build,
					releasev1alpha1.BuildSyncedCondition,
					releasev1alpha1.NotSyncedReason,
					clusterv1.ConditionSeverityError,
					"Cannot create Merge")
				return ctrl.Result{Requeue: true}, err
			}
		}
	}

	if len(remove) > 0 {
		for _, toDelete := range remove {
			mergeToDelete := &releasev1alpha1.Merge{}
			mergeToDelete.Name = mergeName(build.Name, toDelete)
			mergeToDelete.Namespace = build.Namespace
			err := r.Client.Delete(ctx, mergeToDelete)
			if err != nil {
				conditions.MarkFalse(build,
					releasev1alpha1.BuildSyncedCondition,
					releasev1alpha1.NotSyncedReason,
					clusterv1.ConditionSeverityError,
					"Cannot delete Merge")
				return ctrl.Result{Requeue: true}, err
			}
		}
	}

	mergePatchHelper, err := patch.NewHelper(&releasev1alpha1.Merge{}, r.Client)
	if err != nil {
		return ctrl.Result{}, err
	}

	if len(toUpdate) > 0 {
		for _, update := range toUpdate {
			mergeToUpdate := &releasev1alpha1.Merge{}
			mergeToUpdate.Spec.Repo = update
			mergeToUpdate.Name = mergeName(build.Name, update.URL)
			mergeToUpdate.Namespace = build.Namespace
			err := mergePatchHelper.Patch(ctx, mergeToUpdate)
			if err != nil {
				conditions.MarkFalse(build,
					releasev1alpha1.BuildSyncedCondition,
					releasev1alpha1.NotSyncedReason,
					clusterv1.ConditionSeverityError,
					"Cannot update Merge")
				return ctrl.Result{Requeue: true}, err
			}
		}
	}

	conditions.MarkTrue(build, releasev1alpha1.BuildSyncedCondition)

	for i := range allMerges.Items {
		if !conditions.IsTrue(&allMerges.Items[i], clusterv1.ReadyCondition) {
			conditions.MarkFalse(build,
				releasev1alpha1.BuildReadyCondition,
				releasev1alpha1.MergesNotReadyReason,
				clusterv1.ConditionSeverityInfo,
				"Merges is not ready yet")
			return ctrl.Result{}, nil
		}
	}

	conditions.MarkTrue(build, releasev1alpha1.BuildReadyCondition)
	logger.Info("Reconcile Build success")

	return reconcile.Result{}, nil
}

func (r *BuildReconciler) reconcileDelete(ctx context.Context, build *releasev1alpha1.Build) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("reconcileDelete")

	isRemoved := controllerutil.RemoveFinalizer(build, releasev1alpha1.BuildFinalizer)
	if !isRemoved {
		return reconcile.Result{}, fmt.Errorf("connot remove finalizer")
	}
	logger.Info("Remove Finalizer")
	return reconcile.Result{}, nil
}

func patchBuild(ctx context.Context, patchHelper *patch.Helper, build *releasev1alpha1.Build, options ...patch.Option) error {
	// Always update the readyCondition by summarizing the state of other conditions.
	applicableConditions := []clusterv1.ConditionType{
		releasev1alpha1.BuildReadyCondition,
		releasev1alpha1.BuildSyncedCondition,
	}

	conditions.SetSummary(build,
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
	return patchHelper.Patch(ctx, build, options...)
}

func getFullReposDiff(
	currentRepos []releasev1alpha1.Repo,
	mergeList *releasev1alpha1.MergeList,
) ([]releasev1alpha1.Repo, []string, []releasev1alpha1.Repo) {
	currentReposMap := map[string]releasev1alpha1.Repo{}
	currentReposSlice := make([]string, 0, len(currentRepos))
	for _, repo := range currentRepos {
		currentReposMap[repo.URL] = repo
		currentReposSlice = append(currentReposSlice, repo.URL)
	}

	mergeListMap := map[string]releasev1alpha1.Merge{}
	currentListSlice := make([]string, 0, len(mergeList.Items))
	for _, item := range mergeList.Items {
		mergeListMap[item.Spec.Repo.URL] = item
		currentListSlice = append(currentListSlice, item.Spec.Repo.URL)
	}

	add, remove, intersection := FullDiff(currentReposSlice, currentListSlice)

	toCreate := make([]releasev1alpha1.Repo, 0)
	for _, s := range add {
		toCreate = append(toCreate, currentReposMap[s])
	}

	toUpdate := make([]releasev1alpha1.Repo, 0)
	for _, s := range intersection {
		if !currentReposMap[s].Equal(mergeListMap[s].Spec.Repo) {
			toUpdate = append(toUpdate, currentReposMap[s])
		}
	}

	return toCreate, remove, toUpdate
}

func Diff(slice1, slice2 []string) ([]string, []string) {
	m := make(map[string]bool)
	for _, v := range slice1 {
		m[v] = true
	}
	result := make([]string, 0)
	same := make([]string, 0)
	for _, v := range slice2 {
		if !m[v] {
			result = append(result, v)
		} else {
			same = append(same, v)
		}
	}

	return result, same
}

func FullDiff(slice1, slice2 []string) ([]string, []string, []string) {
	resultTo, intersection := Diff(slice1, slice2)
	resultFrom, _ := Diff(slice2, slice1)
	return resultFrom, resultTo, intersection
}

func mergeName(buildName, repoURL string) string {
	repoName := path.Base(repoURL)
	return buildName + "-" + repoName
}

func (r *BuildReconciler) MergeToBuildMapFunc(ctx context.Context, o client.Object) []ctrl.Request {
	result := make([]ctrl.Request, 0, 1)
	m, ok := o.(*releasev1alpha1.Merge)
	if !ok {
		return result
	}

	owners := m.GetOwnerReferences()
	if len(owners) > 0 && owners[0].Kind == "Build" {
		name := client.ObjectKey{Namespace: m.Namespace, Name: owners[0].Name}
		result = append(result, ctrl.Request{NamespacedName: name})
	}
	return result
}

// SetupWithManager sets up the controller with the Manager.
func (r *BuildReconciler) SetupWithManager(mgr ctrl.Manager) error {
	_, err := ctrl.NewControllerManagedBy(mgr).
		For(&releasev1alpha1.Build{}).Watches(&releasev1alpha1.Merge{},
		handler.EnqueueRequestsFromMapFunc(r.MergeToBuildMapFunc),
		builder.WithPredicates(predicate.Funcs{
			CreateFunc: func(event event.CreateEvent) bool {
				return false
			},
			DeleteFunc: func(deleteEvent event.DeleteEvent) bool {
				return true
			},
			UpdateFunc: func(updateEvent event.UpdateEvent) bool {
				return true
			},
			GenericFunc: func(genericEvent event.GenericEvent) bool {
				return false
			},
		})).
		Build(r)
	if err != nil {
		return err
	}

	return nil
}
