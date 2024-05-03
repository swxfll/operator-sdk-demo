## 概述

// TODO（用户）：添加使用/目的的简要概述

## 描述

// TODO（用户）：关于您的项目和用途的详细描述

## 入门指南

### 先决条件

- go 版本 v1.20.0+
- docker 版本 17.03+.
- kubectl 版本 v1.11.3+.
- 访问 Kubernetes v1.11.3+ 集群。

### 在集群上部署

**构建并将您的镜像推送到 `IMG` 指定的位置：**

```sh
make docker-build docker-push IMG=<some-registry>/swxfll-operator:tag
```

**注意：** 该镜像应发布在您指定的个人注册表中。 并且需要有权限从工作环境拉取镜像。 如果上述命令无法工作，请确保您对注册表具有适当的权限。

**将 CRDs 安装到集群中：**

```
sh
Copy code
make install
```

**使用由 `IMG` 指定的镜像将 Manager 部署到集群中：**

```
sh
Copy code
make deploy IMG=<some-registry>/swxfll-operator:tag
```

> **注意：** 如果遇到 RBAC 错误，您可能需要授予自己集群管理员权限或以管理员身份登录。

**创建解决方案的实例** 您可以应用位于 config/sample 中的示例（examples）：

```
sh
Copy code
kubectl apply -k config/samples/
```

> **注意：** 确保示例具有默认值以进行测试。

### 卸载

**从集群中删除实例 (CRs)：**

```
sh
Copy code
kubectl delete -k config/samples/
```

**从集群中删除 API（CRDs）：**

```
sh
Copy code
make uninstall
```

**从集群中取消部署控制器：**

```
sh
Copy code
make undeploy
```

## 贡献

// TODO（用户）：添加有关其他人如何为此项目做出贡献的详细信息

**注意：** 运行 `make help` 获取有关所有潜在 `make` 目标的更多信息

可以通过 [Kubebuilder 文档](https://book.kubebuilder.io/introduction.html) 获取更多信息

```
Copy code
```


## License

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