# installer-service 全面重构方案

基于 **KEP-5 (声明式集群版本管理)** 架构，结合 `kep-refactor.md`、`api-refactor.md` 与 `upgrade.md` 的详细梳理，本方案覆盖 `installer-service` 中所有受影响的接口，旨在将系统从**命令式运维**彻底转型为**声明式任务管理**。

## 1. 核心重构接口 (升级管理)

这是重构的重中之重，涉及从底层逻辑到 API 响应的全面升级。

### 1.1 升级集群 (核心变更)
*   **接口**: `POST /clusters/{cluster-name}/upgrade`
*   **现状**: 直接 Patch `BKECluster` 的 `openFuyaoVersion` 字段。
*   **重构目标**: 创建/更新 `ClusterVersion` CR，触发声明式调和。
*   **变更详情**:
    *   **请求体扩展**: 增加 `skipPreCheck`, `failurePolicy` 等字段。
    *   **逻辑变更**: 调用 `UpgradeService.SubmitUpgrade`，执行路径查找与兼容性预检。
    *   **响应变更**: 返回结构化数据，包含 `taskID` (CV 名称), `path` (升级路径), `preCheck` (预检结果)。

### 1.2 获取可升级版本 (逻辑替换)
*   **接口**: `GET /clusters/{cluster-name}/upgrade-versions`
*   **现状**: 基于硬编码 SemVer 规则过滤 ConfigMap/OBS 中的版本。
*   **重构目标**: 基于 `UpgradePath` CR 的图算法计算合法路径。
*   **变更详情**:
    *   **逻辑变更**: 查询 `UpgradePath` 资源，从当前版本节点出发进行 BFS 搜索，过滤 `blocked` 边。
    *   **响应变更**: 返回的不仅是版本号，建议包含 `hopCount` (跳数) 和 `path` (路径详情)。

### 1.3 获取可安装版本 (数据源迁移)
*   **接口**: `GET /versions`
*   **现状**: 获取所有主版本，用于新建集群。
*   **重构目标**: 从 `UpgradePath` CR 的 `spec.versions` 列表中获取。
*   **变更详情**:
    *   **逻辑变更**: 筛选 `spec.versions` 中 `installable: true` 且 `deprecated: false` 的版本。
    *   **优势**: 集中管理版本生命周期，支持更精细的控制（如标记某些版本不可新装）。

## 2. 新增接口 (全生命周期管理)

为了支持声明式架构带来的异步任务特性，必须新增状态查询与控制接口。

### 2.1 查询升级状态 (新增)
*   **接口**: `GET /clusters/{cluster-name}/upgrade-status`
*   **功能**: 查询 `ClusterVersion` 的实时状态。
*   **参数**: `taskID` (可选，默认为当前活跃的升级任务)。
*   **响应**: 返回 `phase` (Upgrading/Succeeded/Failed), `currentStep` (当前组件), `progress` (进度百分比/跳数)。

### 2.2 取消升级任务 (新增)
*   **接口**: `POST /clusters/{cluster-name}/upgrade/cancel`
*   **功能**: 中断正在进行的升级。
*   **逻辑**: 给 `ClusterVersion` 添加取消 Annotation，控制器检测到后停止 DAG 执行并尝试回滚。

## 3. 废弃与适配接口 (离线/补丁处理)

KEP-5 引入了 OCI 镜像分发和 DAG 调度，部分旧的离线处理接口需要调整。

### 3.1 上传补丁文件 (建议废弃或重构)
*   **接口**: `POST /patches`
*   **现状**: 上传 YAML 到 ConfigMap。
*   **重构建议**: **废弃**。
    *   **替代方案**: 新版本通过 `ReleaseImage` CR 和 OCI 镜像管理版本清单。如果是离线环境，应提供导入 OCI 镜像或手动创建 `ReleaseImage` CR 的管理接口，而不是上传简单的 YAML。

### 3.2 自动升级准备 (建议重构)
*   **接口**: `POST /clusters/{cluster-name}/auto-upgrade`
*   **现状**: 同步执行 Shell 脚本（解压、导镜像）。
*   **重构建议**: **重构为异步任务**。
    *   **替代方案**: 升级前的准备工作（如二进制替换）应作为 DAG 中的一个 `PreUpgrade` Phase 或 `BinaryInstaller` 任务，由 Agent 在节点上执行。此接口可改为触发一个准备任务并返回 Task ID，或者完全移除，由升级流程自动触发。

---

## 4. 关联影响接口 (集群管理)

集群的基础增删改查接口需要适配新的版本模型。

### 4.1 创建集群
*   **接口**: `POST /clusters`
*   **适配**: 确保在创建 `BKECluster` 的同时，自动创建关联的 `ClusterVersion` 资源（初始版本）。

### 4.2 获取集群详情
*   **接口**: `GET /clusters/{cluster-name}`
*   **适配**: 响应中应合并展示 `ClusterVersion` 的状态信息（如 `currentVersion`, `desiredVersion`, `upgradePhase`），而不仅仅是 `BKECluster` 的静态字段。

### 4.3 删除集群
*   **接口**: `DELETE /clusters/{cluster-name}`
*   **适配**: 确保删除 `BKECluster` 时，级联删除关联的 `ClusterVersion` 资源（通过 OwnerReference 机制）。

## 5. 重构实施路线图

| 阶段 | 任务 | 涉及接口 |
| :--- | :--- | :--- |
| **Phase 1** | **基础服务层构建** | 无 (内部代码) |
| | 定义 `UpgradeService`, `VersionService` 接口 | |
| | 实现 `ClusterVersion` Client 封装 | |
| **Phase 2** | **核心接口重构** | `/upgrade`, `/upgrade-versions`, `/versions` |
| | 接入 `UpgradePath` 图查询逻辑 | |
| | 实现预检引擎 (CSP Solver) | |
| **Phase 3** | **新增管理接口** | `/upgrade-status`, `/upgrade/cancel` |
| | 实现状态映射与进度追踪 | |
| **Phase 4** | **旧接口清理/适配** | `/patches` (废弃), `/auto-upgrade` (重构) |
| | 集群管理接口适配 (`GET /clusters`) | |
| **Phase 5** | **平滑迁移** | 全局 |
| | 引入 Feature Gate (`DeclarativeUpgrade`) | |
| | 支持双模式运行 (新旧逻辑并存) | |

通过此方案，`installer-service` 将从一个简单的"CR 修改器"升级为**智能升级网关**，为 openFuyao 提供安全、可控、可观测的集群版本管理能力。
