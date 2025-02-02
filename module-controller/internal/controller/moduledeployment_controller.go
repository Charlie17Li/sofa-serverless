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

package controller

import (
	"context"
	"fmt"
	moduledeploymentv1alpha1 "github.com/sofastack/sofa-serverless/api/v1alpha1"
	v1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// ModuleDeploymentReconciler reconciles a ModuleDeployment object
type ModuleDeploymentReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=module-deployment.serverless.alipay.com,resources=moduledeployments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=module-deployment.serverless.alipay.com,resources=moduledeployments/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=module-deployment.serverless.alipay.com,resources=moduledeployments/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the ModuleDeployment object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.15.0/pkg/reconcile
func (r *ModuleDeploymentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("start reconcile for moduleDeployment")
	defer logger.Info("finish reconcile for moduleDeployment")

	// get the moduleDeployment
	moduleDeployment := &moduledeploymentv1alpha1.ModuleDeployment{}
	err := r.Client.Get(ctx, req.NamespacedName, moduleDeployment)
	if err != nil {
		log.Log.Error(err, "Failed to get moduleDeployment", "moduleDeploymentName", moduleDeployment.Name)
		return ctrl.Result{}, nil
	}

	// update moduleDeployment owner reference
	moduleDeploymentOwnerReferenceExist := false
	for _, ownerReference := range moduleDeployment.GetOwnerReferences() {
		if moduleDeployment.Spec.DeploymentName == ownerReference.Name {
			moduleDeploymentOwnerReferenceExist = true
		}
	}

	if !moduleDeploymentOwnerReferenceExist {
		deployment := &v1.Deployment{}
		err := r.Client.Get(ctx, types.NamespacedName{Namespace: req.Namespace, Name: moduleDeployment.Spec.DeploymentName}, deployment)
		if err != nil {
			log.Log.Error(err, "Failed to get deployment", "deploymentName", deployment.Name)
			return ctrl.Result{}, err
		}
		ownerReference := moduleDeployment.GetOwnerReferences()
		ownerReference = append(ownerReference, metav1.OwnerReference{
			APIVersion:         deployment.APIVersion,
			Kind:               deployment.Kind,
			UID:                deployment.UID,
			Name:               deployment.Name,
			BlockOwnerDeletion: pointer.Bool(true),
			Controller:         pointer.Bool(true),
		})
		moduleDeployment.SetOwnerReferences(ownerReference)
		err = r.Client.Update(ctx, moduleDeployment)
		if err != nil {
			log.Log.Error(err, "Failed to update moduleDeployment", "moduleDeploymentName", moduleDeployment.Name)
			return ctrl.Result{}, err
		}
	}

	// get moduleReplicaSet
	moduleSpec := moduleDeployment.Spec.Template.Spec
	moduleReplicaSetList := &moduledeploymentv1alpha1.ModuleReplicaSetList{}
	err = r.Client.List(ctx, moduleReplicaSetList, &client.ListOptions{Namespace: req.Namespace, LabelSelector: labels.SelectorFromSet(map[string]string{
		moduledeploymentv1alpha1.ModuleNameLabel:       moduleSpec.Module.Name,
		moduledeploymentv1alpha1.ModuleDeploymentLabel: moduleDeployment.Name,
	})})
	if err != nil {
		log.Log.Error(err, "Failed to list moduleDeployment", "name", moduleDeployment.Name, "module", moduleSpec.Module.Name)
		return ctrl.Result{}, err
	}

	if len(moduleReplicaSetList.Items) == 0 {
		// module replicas not exist, create a new one.
		log.Log.Info("module replicas not exist, prepare to create a new one")
		deployment := &v1.Deployment{}
		err := r.Client.Get(ctx, types.NamespacedName{Namespace: req.Namespace, Name: moduleDeployment.Spec.DeploymentName}, deployment)
		if err != nil {
			log.Log.Error(err, "Failed to get deployment", "deploymentName", deployment.Name)
			return ctrl.Result{}, err
		}

		moduleReplicaSet := r.generateModuleReplicas(moduleDeployment, deployment)
		err = r.Client.Create(ctx, moduleReplicaSet)
		if err != nil {
			log.Log.Error(err, "Failed to create moduleReplicaSet", "moduleReplicaSetName", moduleReplicaSet.Name)
			return ctrl.Result{}, err
		}
		log.Log.Info("finish to create a new one", "moduleReplicaSetName", moduleReplicaSet.Name)
	} else {
		// TODO 兼容多个moduleReplicaSet
		moduleReplicaSet := moduleReplicaSetList.Items[0]
		log.Log.Info("prepare to update moduleReplicaSet", "moduleReplicaSetName", moduleReplicaSet.Name)
		if moduleDeployment.Spec.Replicas != moduleReplicaSet.Spec.Replicas || isModuleChanges(moduleSpec.Module, moduleReplicaSet.Spec.Template.Spec.Module) {
			moduleReplicaSet.Spec.Replicas = moduleDeployment.Spec.Replicas
			moduleReplicaSet.Spec.Template.Spec.Module = moduleSpec.Module
			err := r.Client.Update(ctx, &moduleReplicaSet)
			if err != nil {
				log.Log.Error(err, "Failed to update moduleReplicaSet", "moduleReplicaSetName", moduleReplicaSet.Name)
				return ctrl.Result{}, err
			}
			log.Log.Info("finish to update moduleReplicaSet", "moduleReplicaSetName", moduleReplicaSet.Name)
			return ctrl.Result{}, nil
		}
	}
	return ctrl.Result{}, nil
}

func isModuleChanges(module1, module2 moduledeploymentv1alpha1.ModuleInfo) bool {
	return module1.Name != module2.Name || module1.Version != module2.Version
}

func (r *ModuleDeploymentReconciler) generateModuleReplicas(moduleDeployment *moduledeploymentv1alpha1.ModuleDeployment, deployment *v1.Deployment) *moduledeploymentv1alpha1.ModuleReplicaSet {
	newLabels := moduleDeployment.Labels
	newLabels[moduledeploymentv1alpha1.ModuleNameLabel] = moduleDeployment.Spec.Template.Spec.Module.Name
	newLabels[moduledeploymentv1alpha1.ModuleDeploymentLabel] = moduleDeployment.Name
	moduleReplicaSet := &moduledeploymentv1alpha1.ModuleReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Annotations:  map[string]string{},
			Labels:       newLabels,
			GenerateName: fmt.Sprintf("%s-", moduleDeployment.Name),
			Namespace:    moduleDeployment.Namespace,
		},
		Spec: moduledeploymentv1alpha1.ModuleReplicaSetSpec{
			Selector:        *deployment.Spec.Selector,
			Replicas:        moduleDeployment.Spec.Replicas,
			Template:        moduleDeployment.Spec.Template,
			MinReadySeconds: moduleDeployment.Spec.MinReadySeconds,
		},
	}
	owner := []metav1.OwnerReference{
		{
			APIVersion:         moduleDeployment.APIVersion,
			Kind:               moduleDeployment.Kind,
			UID:                moduleDeployment.UID,
			Name:               moduleDeployment.Name,
			BlockOwnerDeletion: pointer.Bool(true),
			Controller:         pointer.Bool(true),
		},
	}
	moduleReplicaSet.SetOwnerReferences(owner)

	return moduleReplicaSet
}

// SetupWithManager sets up the controller with the Manager.
func (r *ModuleDeploymentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&moduledeploymentv1alpha1.ModuleDeployment{}).
		Complete(r)
}
