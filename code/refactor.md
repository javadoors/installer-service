# 基于 KEP-5 的 installer-service 升级接口重构方案

## 基于 KEP-5 的 installer-service 升级接口重构方案

### 一、现状分析

当前 installer-service 的升级机制为**命令式**模式：

| 维度 | 现状 | KEP-5 目标 |
|------|------|-----------|
| **升级触发** | 直接 Patch `BKECluster.Spec.ClusterConfig.Cluster.OpenFuyaoVersion` | 通过 `ClusterVersion` CR 声明目标版本 |
| **升级路径** | 前端调用 `GET /upgrade-versions` 获取可升级版本列表，硬编码 semver 规则 | `UpgradePath` CR 定义 DAG 图，支持多跳升级 |
| **版本清单** | Patch YAML 上传到 ConfigMap，字段散落在 `PatchVersionInfo` | `ReleaseImage` CR 聚合组件清单，通过 OCI 分发 |
| **升级执行** | 依赖 `cluster-api-provider-bke` 控制器检测 CR 变化后执行 | `BKEClusterReconciler` 构建 DAG，按拓扑顺序调度 Phase |
| **兼容性校验** | 无集中校验 | `ReleaseImageReconciler` 执行 CSP 求解，拦截不兼容升级 |

### 二、重构目标

1. **保持 API 向后兼容**：现有 `POST /clusters/{cluster-name}/upgrade` 接口签名不变
2. **引入声明式升级**：内部实现从命令式 Patch 转为 `ClusterVersion` CR 驱动
3. **支持多跳升级**：通过 `UpgradePath` 图自动计算路径，逐跳执行
4. **升级前预检**：路径合法性校验 + 兼容性校验，失败则拦截
5. **升级状态可观测**：返回升级任务 ID，支持异步查询进度

### 三、重构方案

#### 3.1 API 层变更

**3.1.1 现有接口增强**

```
POST /rest/cluster/v1/clusters/{cluster-name}/upgrade
```

**请求体扩展**（向后兼容，新增字段可选）：

```go
type UpgradeRequest struct {
    Version         string `json:"version"`                    // 必填：目标版本
    SkipPreCheck    bool   `json:"skipPreCheck,omitempty"`     // 可选：跳过预检
    MultiHop        *bool  `json:"multiHop,omitempty"`         // 可选：是否允许多跳升级（默认 true）
    FailurePolicy   string `json:"failurePolicy,omitempty"`    // 可选：FailFast | Continue | Rollback
}
```

**响应体变更**：

```go
type UpgradeResponse struct {
    TaskID      string `json:"taskID"`           // 升级任务 ID（ClusterVersion 名称）
    CurrentVer  string `json:"currentVersion"`   // 当前版本
    TargetVer   string `json:"targetVersion"`    // 目标版本
    HopCount    int    `json:"hopCount"`         // 升级跳数
    Path        []string `json:"path,omitempty"` // 升级路径 [v2.4.0, v2.5.0, v2.6.0]
    PreCheck    *PreCheckResult `json:"preCheck,omitempty"`
}

type PreCheckResult struct {
    PathValid       bool     `json:"pathValid"`
    CompatibilityOK bool     `json:"compatibilityOK"`
    BlockedReason   string   `json:"blockedReason,omitempty"`
}
```

**3.1.2 新增接口**

```
GET /rest/cluster/v1/clusters/{cluster-name}/upgrade-status?taskID={taskID}
```

返回升级进度（对应 `ClusterVersion.Status.UpgradeProgress`）：

```go
type UpgradeStatusResponse struct {
    TaskID        string   `json:"taskID"`
    Phase         string   `json:"phase"`         // Pending / PreChecking / Upgrading / Succeeded / Failed / Blocked
    CurrentHop    int      `json:"currentHop"`
    TotalHops     int      `json:"totalHops"`
    CurrentStep   string   `json:"currentStep"`   // 当前执行的组件名
    CompletedSteps []string `json:"completedSteps"`
    StartedAt     string   `json:"startedAt"`
    CompletedAt   string   `json:"completedAt,omitempty"`
    ErrorMessage  string   `json:"errorMessage,omitempty"`
}
```

```
POST /rest/cluster/v1/clusters/{cluster-name}/upgrade/cancel
```

取消升级任务（设置 `ClusterVersion` 的取消 Annotation）。

#### 3.2 核心实现层重构

**3.2.1 新增服务层：UpgradeService**

```
pkg/installer/
├── upgrade_service.go      # 升级服务主入口
├── upgrade_precheck.go     # 预检：路径查找 + 兼容性校验
├── upgrade_status.go       # 升级状态查询
├── clusterversion_client.go # ClusterVersion CR 操作封装
```

**3.2.2 UpgradeService 核心接口**

```go
type UpgradeService interface {
    // SubmitUpgrade 提交升级请求（替代原 UpgradeOpenFuyao）
    SubmitUpgrade(ctx context.Context, clusterName string, req UpgradeRequest) (*UpgradeResponse, error)
    
    // GetUpgradeStatus 查询升级状态
    GetUpgradeStatus(ctx context.Context, clusterName, taskID string) (*UpgradeStatusResponse, error)
    
    // CancelUpgrade 取消升级
    CancelUpgrade(ctx context.Context, clusterName, taskID string) error
    
    // GetUpgradePaths 获取可用升级路径（替代原 GetOpenFuyaoUpgradeVersions）
    GetUpgradePaths(ctx context.Context, clusterName, currentVersion string) ([]UpgradePathOption, error)
}
```

**3.2.3 SubmitUpgrade 流程**

```
SubmitUpgrade(clusterName, req)
  │
  ├─ 1. 获取 BKECluster 实例，读取当前版本
  │     └─ 若未关联 ClusterVersion，自动创建（平滑迁移）
  │
  ├─ 2. 预检阶段（除非 skipPreCheck=true）
  │     ├─ 2.1 调用 UpgradePathGraph.FindPath(current, target)
  │     │     └─ 无路径 → 返回 Blocked
  │     │     └─ 路径中有 blocked edge → 返回 Blocked + 原因
  │     ├─ 2.2 调用 CheckCompatibility(components)
  │     │     └─ 冲突 → 返回 CompatibilityFailed + 冲突详情
  │     └─ 预检结果写入响应
  │
  ├─ 3. 创建/更新 ClusterVersion CR
  │     └─ cv.Spec.DesiredVersion = req.Version
  │     └─ cv.Annotations["cvo.openfuyao.cn/failure-policy"] = req.FailurePolicy
  │     └─ 设置 OwnerReference 指向 BKECluster
  │
  └─ 4. 返回 UpgradeResponse（含 TaskID、路径、预检结果）
```

**3.2.4 handler.go 变更**

```go
func (h *Handler) upgradeCluster(request *restful.Request, response *restful.Response) {
    req := installer.UpgradeRequest{}
    if err := request.ReadEntity(&req); err != nil {
        // 错误处理...
    }

    clusterName := request.PathParameter("cluster-name")
    
    // 调用新的 UpgradeService
    resp, err := h.upgradeService.SubmitUpgrade(request.Request.Context(), clusterName, req)
    if err != nil {
        // 区分预检失败与系统错误
        if isPreCheckError(err) {
            response.WriteHeaderAndEntity(http.StatusBadRequest, httputil.GetResponseJson(constant.PreCheckFailed, err.Error(), resp))
        } else {
            response.WriteHeaderAndEntity(http.StatusInternalServerError, httputil.GetDefaultServerFailureResponseJson())
        }
        return
    }

    response.WriteHeaderAndEntity(http.StatusOK, resp)
}
```

**3.2.5 新增 handler**

```go
func (h *Handler) getUpgradeStatus(request *restful.Request, response *restful.Response) {
    clusterName := request.PathParameter("cluster-name")
    taskID := request.QueryParameter("taskID")
    
    status, err := h.upgradeService.GetUpgradeStatus(request.Request.Context(), clusterName, taskID)
    if err != nil {
        // 错误处理...
    }
    response.WriteHeaderAndEntity(http.StatusOK, status)
}

func (h *Handler) cancelUpgrade(request *restful.Request, response *restful.Response) {
    clusterName := request.PathParameter("cluster-name")
    req := struct { TaskID string `json:"taskID"` }{}
    if err := request.ReadEntity(&req); err != nil {
        // 错误处理...
    }
    
    if err := h.upgradeService.CancelUpgrade(request.Request.Context(), clusterName, req.TaskID); err != nil {
        // 错误处理...
    }
    response.WriteHeaderAndEntity(http.StatusOK, httputil.GetDefaultSuccessResponseJson())
}
```

#### 3.3 路由注册变更

```go
// pkg/api/clustermanage/register.go

func (h *Handler) RegisterUpgradeRoutes(ws *restful.WebService) {
    // 原有接口（增强）
    ws.POST("/clusters/{cluster-name}/upgrade").
        To(h.upgradeCluster).
        Doc("升级集群").
        Reads(installer.UpgradeRequest{}).
        Writes(installer.UpgradeResponse{})

    // 新增接口
    ws.GET("/clusters/{cluster-name}/upgrade-status").
        To(h.getUpgradeStatus).
        Doc("查询升级状态").
        Writes(installer.UpgradeStatusResponse{})

    ws.POST("/clusters/{cluster-name}/upgrade/cancel").
        To(h.cancelUpgrade).
        Doc("取消升级任务").
        Reads(struct { TaskID string }{})
}
```

#### 3.4 平滑迁移策略

**Feature Gate 控制**：

```go
// pkg/installer/featuregate.go
var featureGate = featuregate.NewFeatureGate()

func init() {
    featureGate.Add(map[featuregate.Feature]featuregate.FeatureSpec{
        "DeclarativeUpgrade": {Default: false, PreRelease: featuregate.Beta},
    })
}

func DeclarativeUpgradeEnabled() bool {
    return featureGate.Enabled("DeclarativeUpgrade")
}
```

**双模式运行**：

```go
func (c *installerClient) UpgradeOpenFuyao(clusterName string, version string) error {
    if !DeclarativeUpgradeEnabled() {
        // 旧模式：直接 Patch BKECluster
        return c.legacyUpgrade(clusterName, version)
    }
    // 新模式：通过 ClusterVersion CR 驱动
    return c.declarativeUpgrade(clusterName, version)
}
```

**自动创建 ClusterVersion**：

当 `BKEClusterReconciler` 检测到未关联 `ClusterVersion` 的集群时，自动创建：

```go
func (r *BKEClusterReconciler) ensureClusterVersion(bc *bkev1beta1.BKECluster) error {
    cvList := &cvoapi.ClusterVersionList{}
    if err := r.List(ctx, cvList, client.InNamespace(bc.Namespace)); err != nil {
        return err
    }
    
    for _, cv := range cvList.Items {
        if isOwnerOf(&cv, bc) {
            return nil // 已关联
        }
    }
    
    // 自动创建
    cv := &cvoapi.ClusterVersion{
        ObjectMeta: metav1.ObjectMeta{
            Name:      fmt.Sprintf("%s-version", bc.Name),
            Namespace: bc.Namespace,
            OwnerReferences: []metav1.OwnerReference{
                *metav1.NewControllerRef(bc, bkev1beta1.GVK),
            },
        },
        Spec: cvoapi.ClusterVersionSpec{
            DesiredVersion:  bc.Spec.ClusterConfig.Cluster.OpenFuyaoVersion,
        },
        Status: cvoapi.ClusterVersionStatus{
            CurrentVersion: bc.Spec.ClusterConfig.Cluster.OpenFuyaoVersion,
            Phase:          cvoapi.PhaseReady,
        },
    }
    return r.Create(ctx, cv)
}
```

#### 3.5 版本查询接口重构

**原 `GetOpenFuyaoUpgradeVersions` 改为从 UpgradePath 图查询**：

```go
func (s *upgradeServiceImpl) GetUpgradePaths(ctx context.Context, clusterName, currentVersion string) ([]UpgradePathOption, error) {
    // 从 UpgradePathGraph 获取所有从 currentVersion 出发的可达路径
    edges := s.upgradePathGraph.GetEdgesFrom(currentVersion)
    
    var options []UpgradePathOption
    for _, edge := range edges {
        if edge.Blocked || edge.Deprecated {
            continue
        }
        
        // 计算完整路径到最新版本
        fullPath, _ := s.upgradePathGraph.FindPath(currentVersion, edge.To)
        
        options = append(options, UpgradePathOption{
            TargetVersion: edge.To,
            HopCount:      len(fullPath),
            Path:          buildPathVersions(fullPath),
            PreChecks:     edge.PreCheck,
            Deprecated:    edge.Deprecated,
        })
    }
    
    return options, nil
}
```

#### 3.6 目录结构变更

```
pkg/installer/
├── interface.go              # 新增 UpgradeService 接口
├── types.go                  # 新增 UpgradeRequest/Response/Status 类型
├── cluster.go                # 保留旧方法，新增 declarativeUpgrade
├── upgrade_service.go        # 新增：UpgradeService 实现
├── upgrade_precheck.go       # 新增：预检逻辑
├── upgrade_status.go         # 新增：状态查询
├── clusterversion_client.go  # 新增：ClusterVersion CR 操作
├── featuregate.go            # 新增：Feature Gate 控制
└── auto_upgrade.go           # 保留（patch 准备逻辑）

pkg/api/clustermanage/
├── handler.go                # 修改 upgradeCluster，新增 getUpgradeStatus/cancelUpgrade
└── register.go               # 新增路由注册
```

### 四、关键变更点总结

| 变更项 | 文件 | 变更类型 | 说明 |
|--------|------|----------|------|
| `UpgradeRequest` 扩展 | `types.go` | 修改 | 新增可选字段 |
| `UpgradeResponse` | `types.go` | 新增 | 结构化响应 |
| `UpgradeStatusResponse` | `types.go` | 新增 | 状态查询响应 |
| `UpgradeService` 接口 | `interface.go` | 新增 | 升级服务抽象 |
| `upgrade_service.go` | 新增文件 | 新增 | 核心实现 |
| `upgrade_precheck.go` | 新增文件 | 新增 | 预检逻辑 |
| `upgrade_status.go` | 新增文件 | 新增 | 状态查询 |
| `clusterversion_client.go` | 新增文件 | 新增 | CR 操作封装 |
| `featuregate.go` | 新增文件 | 新增 | Feature Gate |
| `upgradeCluster` handler | `handler.go` | 修改 | 调用新服务 |
| `getUpgradeStatus` handler | `handler.go` | 新增 | 状态查询 handler |
| `cancelUpgrade` handler | `handler.go` | 新增 | 取消升级 handler |
| 路由注册 | `register.go` | 修改 | 新增 2 个端点 |
| `UpgradeOpenFuyao` | `cluster.go` | 修改 | 双模式分发 |
| `GetOpenFuyaoUpgradeVersions` | `cluster.go` | 修改 | 改为从图查询 |

### 五、迁移步骤

1. **Phase 1**：新增 `UpgradeService` 接口与类型定义，Feature Gate 默认关闭
2. **Phase 2**：实现 `SubmitUpgrade` 核心流程（创建 ClusterVersion CR），旧逻辑 fallback
3. **Phase 3**：实现预检（路径查找 + 兼容性校验），集成 `UpgradePathGraph`
4. **Phase 4**：实现状态查询与取消接口
5. **Phase 5**：开启 Feature Gate Beta，生产灰度验证
6. **Phase 6**：GA 切换，移除旧 Patch 逻辑
