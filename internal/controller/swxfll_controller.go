/*
Copyright 2024.

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
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"os"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"strings"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	cachev1alpha1 "github.com/swxfll/operator-sdk-demo/api/v1alpha1"
)

const swxfllFinalizer = "cache.swxfll.com/finalizer"

// 用于管理状态条件的定义
const (
	// typeAvailableSwxfll 表示 Deployment 调和的状态
	typeAvailableSwxfll = "Available"
	// typeDegradedSwxfll 表示当自定义资源被删除并且必须执行 finalizer 操作时使用的状态。
	typeDegradedSwxfll = "Degraded"
)

// SwxfllReconciler 调和 Swxfll 对象
type SwxfllReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	//  EventRecorder 知道如何代表 EventSource 记录事件
	Recorder record.EventRecorder
}

//+kubebuilder:rbac:groups=cache.swxfll.com,resources=swxflls,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=cache.swxfll.com,resources=swxflls/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=cache.swxfll.com,resources=swxflls/finalizers,verbs=update
//+kubebuilder:rbac:groups=core,resources=events,verbs=create;patch
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch

// Reconcile 是 Kubernetes 主要调和循环的一部分，旨在将集群的当前状态移向期望的状态。
// 控制器的调和循环必须是幂等的是至关重要的。通过遵循 Operator 模式，您将创建控制器，
// 它们提供了一个调和函数负责在集群上同步资源直到达到期望的状态。
// 违反此建议与 controller-runtime 的设计原则相违背，
// 可能会导致意想不到的后果，例如资源被卡住，需要手动干预。
// 更多信息：
// - 关于 Operator 模式: https://kubernetes.io/docs/concepts/extend-kubernetes/operator/
// - 关于控制器: https://kubernetes.io/docs/concepts/architecture/controller/
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.16.3/pkg/reconcile
func (r *SwxfllReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	// 获取 Swxfll 实例
	// 目的是检查集群上是否已应用 Kind 为 Swxfll 的自定义资源
	// 如果没有应用，则返回 nil 以停止调和过程
	swxfll := &cachev1alpha1.Swxfll{}
	err := r.Get(ctx, req.NamespacedName, swxfll)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// 如果找不到自定义资源，则通常意味着它已被删除或尚未创建
			// 这样，我们将停止调和过程
			log.Info("swxfll 资源未找到。由于对象必须被删除，因此忽略。")
			return ctrl.Result{}, nil
		}
	}

	// 当没有可用的状态时，让我们将状态设置为 Unknown
	if swxfll.Status.Conditions == nil || len(swxfll.Status.Conditions) == 0 {
		meta.SetStatusCondition(&swxfll.Status.Conditions, metav1.Condition{
			Type:    typeAvailableSwxfll,
			Status:  metav1.ConditionUnknown,
			Reason:  "调和中",
			Message: "开始调和",
		})
		if err = r.Status().Update(ctx, swxfll); err != nil {
			log.Error(err, "无法更新 Swxfll 状态")
			return ctrl.Result{}, err
		}

		// 在更新状态后，让我们重新获取 Memcached 自定义资源，
		// 以便在集群上获取资源的最新状态，并且避免引发问题 "对象已被修改，请将您的更改应用
		// 到最新版本并重试"，这将在下一次尝试更新时重新触发调和过程。
		if err := r.Get(ctx, req.NamespacedName, swxfll); err != nil {
			log.Error(err, "重新获取 swxfll 失败")
			return ctrl.Result{}, err
		}
	}

	// 让我们添加一个 finalizer。然后，我们可以定义一些在自定义资源被删除之前应该执行的操作。
	// Kubernetes 中的 finalizers 是用于在资源被删除时执行清理操作的一种机制。
	// 当一个资源对象被标记为要删除时，Kubernetes 控制平面将检查该对象的 finalizers。
	// 如果存在 finalizers，则 Kubernetes 将等待相关的终结操作完成后再删除该对象，以确保对象被正确清理。
	// 更多信息请参阅：https://kubernetes.io/docs/concepts/overview/working-with-objects/finalizers
	if !controllerutil.ContainsFinalizer(swxfll, swxfllFinalizer) {
		log.Info("为 swxfll 添加 Finalizer")
		if ok := controllerutil.AddFinalizer(swxfll, swxfllFinalizer); !ok {
			log.Error(err, "无法将 finalizer 添加到自定义资源中")
			return ctrl.Result{Requeue: true}, nil
		}

		if err = r.Update(ctx, swxfll); err != nil {
			log.Error(err, "无法更新自定义资源以添加 finalizer")
			return ctrl.Result{}, err
		}
	}

	// 检查 swxfll 实例是否被标记为删除，这由删除时间戳是否被设置来指示。
	isSwxfllMarkedToBeDeleted := swxfll.GetDeletionTimestamp() != nil
	if isSwxfllMarkedToBeDeleted {
		if controllerutil.ContainsFinalizer(swxfll, swxfllFinalizer) {
			log.Info("在删除 CR 之前为 swxfll 执行 Finalizer 操作")

			// 在这里添加一个状态 "Downgrade"，以定义该资源开始其终止过程。
			meta.SetStatusCondition(
				&swxfll.Status.Conditions,
				metav1.Condition{
					Type:    typeAvailableSwxfll,
					Status:  metav1.ConditionUnknown,
					Reason:  "Finalizing",
					Message: fmt.Sprintf("执行自定义资源的 finalizer 操作: %s", swxfll.Name)})

			if err := r.Status().Update(ctx, swxfll); err != nil {
				log.Error(err, "无法更新 swxfll 状态")
				return ctrl.Result{}, err
			}

			// 在移除 finalizer 并允许 Kubernetes API 移除自定义资源之前执行所有必要的操作。
			r.doFinalizerOperationsForSwxfll(swxfll)

			// TODO（用户）：如果您在 doFinalizerOperationsForSwxfll 方法中添加操作，
			// 则需要确保在删除并更新 Downgrade 状态之前一切正常，
			// 否则，您应该在这里重新排队。

			// 在更新状态之前重新获取 swxfll 自定义资源，
			// 以便在集群上获取资源的最新状态，并且避免引发问题 "对象已被修改，请将您的更改应用
			// 到最新版本并重试"，这将重新触发调和过程。
			if err := r.Get(ctx, req.NamespacedName, swxfll); err != nil {
				log.Error(err, "重新获取 swxfll 失败")
				return ctrl.Result{}, err
			}

			meta.SetStatusCondition(&swxfll.Status.Conditions,
				metav1.Condition{
					Type:    typeDegradedSwxfll,
					Status:  metav1.ConditionTrue,
					Reason:  "Finalizing",
					Message: fmt.Sprintf("自定义资源 %s 的 finalizer 操作已成功完成", swxfll.Name)})

			if err := r.Status().Update(ctx, swxfll); err != nil {
				log.Error(err, "Failed to update swxfll status")
				return ctrl.Result{}, err
			}

			log.Info("成功执行操作后，删除 swxfll 的 Finalizer")
			if ok := controllerutil.RemoveFinalizer(swxfll, swxfllFinalizer); !ok {
				log.Error(err, "Failed to remove finalizer for swxfll")
				return ctrl.Result{Requeue: true}, nil
			}

			if err := r.Update(ctx, swxfll); err != nil {
				log.Error(err, "Failed to remove finalizer for swxfll")
				return ctrl.Result{}, err
			}
		}

		return ctrl.Result{}, nil
	}

	// 检查deployment是否已存在，如果不存在，则创建一个新的
	found := &appsv1.Deployment{}
	err = r.Get(ctx, types.NamespacedName{
		Name:      swxfll.Name,
		Namespace: swxfll.Namespace,
	}, found)
	if err != nil && apierrors.IsNotFound(err) {
		// Define a new deployment
		r.d
	}

	return ctrl.Result{}, nil
}

// finalizeSwxfll 将在删除 CR 之前执行所需的操作。
func (r *SwxfllReconciler) doFinalizerOperationsForSwxfll(cr *cachev1alpha1.Swxfll) {
	// TODO（用户）：在 CR 被删除之前，添加操作清理步骤。
	// 操作清理步骤的示例包括执行备份和删除不由此 CR 拥有的资源，比如 PVC。

	// 注意：不建议使用 finalizer 来删除在调和过程中创建和管理的资源。
	// 这些资源，例如在此调和过程中创建的 Deployment，被定义为依赖于自定义资源。
	// 请注意，我们使用 ctrl.SetControllerReference 方法来设置 ownerRef，
	// 这意味着 Deployment 将由 Kubernetes API 删除。
	// 更多信息请参阅：https://kubernetes.io/docs/tasks/administer-cluster/use-cascading-deletion/

	// 以下实现将会触发一个事件
	r.Recorder.Event(cr, "Warning", "Deleting",
		fmt.Sprintf("自定义资源 %s 正在从命名空间 %s 中删除", cr.Name, cr.Namespace))

}

// deploymentForSwxfll 返回一个 Swxfll Deployment 对象
func (r *SwxfllReconciler) deploymentForSwxfll(swxfll *cachev1alpha1.Swxfll) (
	*appsv1.Deployment, error) {
	ls := labelsForSwxfll(swxfll.Name)
	replicas := swxfll.Spec.Size

	// 获取 Operand 镜像
	image, err := imageForSwxfll()
	if err != nil {
		return nil, err
	}

	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      swxfll.Name,
			Namespace: swxfll.Namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: ls,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: ls,
				},
				Spec: corev1.PodSpec{
					// TODO（用户）：取消下面代码的注释以根据您的解决方案支持的平台配置 nodeAffinity 表达式。
					// 支持多种架构被认为是最佳实践。使用 makefile 目标 docker-buildx 构建您的 manager 镜像。
					// 您还可以使用 docker manifest inspect <image> 来检查支持的平台。
					// 更多信息请参阅：https://kubernetes.io/docs/concepts/scheduling-eviction/assign-pod-node/#node-affinity
					//Affinity: &corev1.Affinity{
					//	NodeAffinity: &corev1.NodeAffinity{
					//		RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
					//			NodeSelectorTerms: []corev1.NodeSelectorTerm{
					//				{
					//					MatchExpressions: []corev1.NodeSelectorRequirement{
					//						{
					//							Key:      "kubernetes.io/arch",
					//							Operator: "In",
					//							Values:   []string{"amd64", "arm64", "ppc64le", "s390x"},
					//						},
					//						{
					//							Key:      "kubernetes.io/os",
					//							Operator: "In",
					//							Values:   []string{"linux"},
					//						},
					//					},
					//				},
					//			},
					//		},
					//	},
					//},
					SecurityContext: &corev1.PodSecurityContext{
						RunAsNonRoot: &[]bool{true}[0],
						// 重要提示：seccomProfile 是在 Kubernetes 1.19 中引入的
						// 如果您希望生成支持较低版本的解决方案，您必须删除此选项。
						SeccompProfile: &corev1.SeccompProfile{
							Type: corev1.SeccompProfileTypeRuntimeDefault,
						},
					},
					Containers: []corev1.Container{
						{
							Image:           image,
							Name:            "swxfll",
							ImagePullPolicy: corev1.PullIfNotPresent,
							// 为容器确保严格的安全上下文
							// 更多信息请参阅：https://kubernetes.io/docs/concepts/security/pod-security-standards/#restricted
							SecurityContext: &corev1.SecurityContext{
								// 警告：确保在 Dockerfile 中定义了一个 UserID，
								// 否则 Pod 将无法运行，并显示 "container has runAsNonRoot and image has non-numeric user" 错误。
								// 如果您希望您的工作负载被允许在 OpenShift/OKD 供应商的强制执行受限模式的命名空间中运行，
								// 那么您必须确保 Dockerfile 定义了一个用户ID，或者您必须将 "RunAsNonRoot" 和 "RunAsUser" 字段留空。
								RunAsUser:                &[]int64{1001}[0],
								AllowPrivilegeEscalation: &[]bool{false}[0],
								Capabilities: &corev1.Capabilities{
									Drop: []corev1.Capability{
										"ALL",
									},
								},
							},
							Ports: []corev1.ContainerPort{{
								ContainerPort: swxfll.Spec.ContainerPort,
								Name:          "swxfll",
							}},
							Command: []string{"swxfll", "-m=64", "-o", "modern", "-v"},
						}},
				},
			},
		},
	}

	// 为 Deployment 设置 ownerRef
	// 更多信息请参阅：https://kubernetes.io/docs/concepts/overview/working-with-objects/owners-dependents/
	if err := ctrl.SetControllerReference(swxfll, dep, r.Scheme); err != nil {
		return nil, err
	}

	return dep, nil
}

// labelsForSwxfll 返回用于选择资源的标签
// 更多信息请参阅：https://kubernetes.io/docs/concepts/overview/working-with-objects/common-labels/
func labelsForSwxfll(name string) map[string]string {
	var imageTag string
	image, err := imageForSwxfll()
	if err == nil {
		imageTag = strings.Split(image, ":")[1]
	}
	return map[string]string{
		"app.kubernetes.io/name":       "Swxfll",
		"app.kubernetes.io/instance":   name,
		"app.kubernetes.io/version":    imageTag,
		"app.kubernetes.io/part-of":    "swxfll-operator",
		"app.kubernetes.io/created-by": "controller-manager",
	}
}

// imageForSwxfll 从 config/manager/manager.yaml
// 中定义的 MEMCACHED_IMAGE 环境变量中获取由此控制器管理的 Operand 镜像
func imageForSwxfll() (string, error) {
	var imageEnvVar = "SWXFLL_IMAGE"
	image, found := os.LookupEnv(imageEnvVar)
	if !found {
		return "", fmt.Errorf("无法找到 %s 环境变量与镜像", imageEnvVar)
	}
	return image, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *SwxfllReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&cachev1alpha1.Swxfll{}).
		Complete(r)
}
