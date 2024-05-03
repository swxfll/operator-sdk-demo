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

package main

import (
	"crypto/tls"
	"flag"
	"os"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	cachev1alpha1 "github.com/swxfll/operator-sdk-demo/api/v1alpha1"
	"github.com/swxfll/operator-sdk-demo/internal/controller"
	//+kubebuilder:scaffold:imports
)

var (
	// 在 Kubernetes 中，Scheme 是一种对象转换和编码/解码机制，
	// 它定义了 API 对象的类型和如何在不同格式之间进行转换。
	// 在 k8s.io/apimachinery/pkg/runtime 包中，
	// Scheme 是一个接口，它定义了 Kubernetes 资源对象（例如 Pod、Service 等）的类型信息和序列化/反序列化规则。
	//
	// runtime.NewScheme() 创建了一个新的 Scheme 对象。
	// 然后，utilruntime.Must(clientgoscheme.AddToScheme(scheme)) 通过
	// clientgoscheme.AddToScheme() 将 Kubernetes 客户端的核心资源（例如 Pod、Service 等）
	// 添加到了这个 Scheme 中。这样一来，你的控制器就可以识别和操作这些核心资源了。
	//
	//总的来说，scheme 变量用于注册 Kubernetes API 对象，以便控制器能够识别和操作这些对象。
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	// 用于插入自定义资源的 Scheme 相关的代码。
	utilruntime.Must(cachev1alpha1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
}

func main() {
	setupLog.Info("Hello World")

	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	var secureMetrics bool
	var enableHTTP2 bool
	// 解析命令行参数，并根据这些参数配置日志记录器
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.BoolVar(&secureMetrics, "metrics-secure", false,
		"If set the metrics endpoint is served securely")
	flag.BoolVar(&enableHTTP2, "enable-http2", false,
		"If set, HTTP/2 will be enabled for the metrics and webhook servers")
	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	// if the enable-http2 flag is false (the default), http/2 should be disabled
	// due to its vulnerabilities. More specifically, disabling http/2 will
	// prevent from being vulnerable to the HTTP/2 Stream Cancelation and
	// Rapid Reset CVEs. For more information see:
	// - https://github.com/advisories/GHSA-qppj-fm5r-hxr3
	// - https://github.com/advisories/GHSA-4374-p667-p6c8

	// 定义了一个名为 disableHTTP2 的匿名函数，这个函数用于禁用 HTTP/2。
	// 它将 NextProtos 字段设置为 []string{"http/1.1"}，即只使用 HTTP/1.1 协议。
	disableHTTP2 := func(c *tls.Config) {
		setupLog.Info("disabling http/2")
		c.NextProtos = []string{"http/1.1"}
	}

	// 创建了一个空的 tlsOpts 切片，用于存储 TLS 选项。
	tlsOpts := []func(*tls.Config){}
	if !enableHTTP2 {
		tlsOpts = append(tlsOpts, disableHTTP2)
	}

	// webhookServer 是一个 webhook 服务器的实例，它负责接收来自 Kubernetes API Server 的 webhook 请求，并将这些请求转发给相应的 webhook 处理程序。
	//具体来说，webhookServer 的作用是：
	//监听来自 Kubernetes API Server 的 webhook 请求。
	//将这些请求转发给相应的 webhook 处理程序。
	//处理 webhook 请求的 TLS 加密和解密，确保通信安全
	// 通过 webhookServer，你可以轻松地实现自定义的 webhook 逻辑，并与 Kubernetes API Server 进行交互。

	// webhook.NewServer() 函数用于创建一个新的 webhook 服务器， 并接受一个 webhook.Options 结构体作为参数。
	// tlsOpts 切片中的函数会传递给 webhook server，用于配置 TLS 选项。
	webhookServer := webhook.NewServer(webhook.Options{
		TLSOpts: tlsOpts,
	})

	// ctrl.GetConfigOrDie() 用于获取 Kubernetes 集群的配置信息。
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		// 用于指定控制器管理器使用的 Kubernetes 资源 Scheme。
		Scheme: scheme,
		//用于配置指标服务器的选项，包括绑定地址、是否安全服务等
		Metrics: metricsserver.Options{
			BindAddress:   metricsAddr,
			SecureServing: secureMetrics,
			TLSOpts:       tlsOpts,
		},
		// 用于指定 webhook 服务器的实例，即 webhookServer。
		WebhookServer: webhookServer,
		//HealthProbeBindAddress：用于指定健康探针绑定地址。
		HealthProbeBindAddress: probeAddr,
		// LeaderElection：用于指定是否启用控制器管理器的 Leader 选举机制。
		LeaderElection: enableLeaderElection,
		// LeaderElectionID：用于指定 Leader 选举的标识符。
		LeaderElectionID: "9f665a0e.swxfll.com",
		// LeaderElectionReleaseOnCancel defines if the leader should step down voluntarily
		// when the Manager ends. This requires the binary to immediately end when the
		// Manager is stopped, otherwise, this setting is unsafe. Setting this significantly
		// speeds up voluntary leader transitions as the new leader don't have to wait
		// LeaseDuration time first.
		//
		// In the default scaffold provided, the program ends immediately after
		// the manager stops, so would be fine to enable this option. However,
		// if you are doing or is intended to do any operation such as perform cleanups
		// after the manager stops then its usage might be unsafe.
		// LeaderElectionReleaseOnCancel: true,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	// 是一种标记，用于告诉 operator-sdk 在生成的代码中插入一些必要的构建器代码。这些构建器代码通常用于创建控制器的主要逻辑。
	if err = (&controller.SwxfllReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Swxfll")
		os.Exit(1)
	}
	//+kubebuilder:scaffold:builder

	// 添加健康探针（Healthz Check）
	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}

	// 就绪探针（Readiness Check）
	// 注意点
	// healthz.Ping 函数是在代码中编写的就绪探针逻辑，它可以在控制器的 main 函数中调用，并与控制器一起运行。
	// YAML 文件中的就绪探针是通过 Kubernetes 对象的配置来定义的，它与应用程序的部署和管理分开。
	// Kubernetes 将根据 YAML 文件中定义的就绪探针配置来监视和管理应用程序的就绪状态。(实际工作的是kubelet)
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	// mgr.Start(ctrl.SetupSignalHandler()) 用于启动控制器管理器，
	// 并且当接收到信号时（例如 SIGTERM 或 SIGINT）优雅地关闭控制器管理器。
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}

	setupLog.Info("Hello World End")
}
