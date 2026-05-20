# 升级相关接口及规格梳理

## 一、升级相关 API 接口一览

所有升级相关路由注册在 [register.go: registerUpgradeRoutes](file:///d:\code\github\installer-service\pkg\api\clustermanage\register.go#L130-L154)，基础路径为 `/rest/cluster/v1`。

| # | HTTP 方法 | 路径 | Handler 方法 | 功能描述 |
|---|-----------|------|-------------|---------|
| 1 | **POST** | `/clusters/{cluster-name}/upgrade` | [upgradeCluster](file:///d:\code\github\installer-service\pkg\api\clustermanage\handler.go#L261-L283) | 升级集群（修改 openFuyaoVersion） |
| 2 | **POST** | `/patches` | [uploadPatchFile](file:///d:\code\github\installer-service\pkg\api\clustermanage\handler.go#L286-L300) | 上传升级 patch 文件 |
| 3 | **GET** | `/versions` | [getOpenFuyaoVersions](file:///d:\code\github\installer-service\pkg\api\clustermanage\handler.go#L302-L312) | 获取可安装的 openFuyao 版本列表 |
| 4 | **GET** | `/clusters/{cluster-name}/upgrade-versions` | [getUpgradeOpenFuyaoVersions](file:///d:\code\github\installer-service\pkg\api\clustermanage\handler.go#L314-L351) | 获取可升级的 openFuyao 版本列表 |
| 5 | **POST** | `/clusters/{cluster-name}/auto-upgrade` | [autoUpgrade](file:///d:\code\github\installer-service\pkg\api\clustermanage\handler.go#L354-L381) | 执行自动升级补丁准备 |

## 二、各接口详细规格

### 1. POST `/clusters/{cluster-name}/upgrade` — 升级集群

**请求体**：
```json
{
  "version": "v1.2.0"   // 目标升级版本号
}
```

**请求类型定义** ([types.go: UpgradeRequest](file:///d:\code\github\installer-service\pkg\installer\types.go#L188-L190))：
```go
type UpgradeRequest struct {
    Version string `json:"version"`
}
```

**处理逻辑**：
1. 读取请求体，校验 `version` 非空
2. 调用 `UpgradeOpenFuyao(clusterName, version)`
3. 底层通过 Dynamic Client Patch BKECluster CR 的 `spec.clusterConfig.cluster.openFuyaoVersion` 字段

**响应**：
- 200：升级成功
- 400：参数为空
- 500：升级失败

**底层实现** ([cluster.go: UpgradeOpenFuyao](file:///d:\code\github\installer-service\pkg\installer\cluster.go#L685-L700))：
- 使用 `dynamicClient.Resource(gvr).Namespace(clusterName).Patch()` 更新 BKECluster CR
- Patch 字段路径：`spec.clusterConfig.cluster.openFuyaoVersion`

### 2. POST `/patches` — 上传升级 Patch 文件

**请求体**：
```json
{
  "PatchFileName": "v1.2.0",
  "PatchFileContent": "openFuyaoVersion: v1.2.0\nkubernetesVersion: v1.28.0\n..."
}
```

**请求类型定义** ([types.go: PatchFileData](file:///d:\code\github\installer-service\pkg\installer\types.go#L172-L175))：
```go
type PatchFileData struct {
    FileName    string `json:"PatchFileName"`
    FileContent string `json:"PatchFileContent"`
}
```

**Patch 文件内容结构** ([types.go: PatchVersionInfo](file:///d:\code\github\installer-service\pkg\installer\types.go#L196-L202))：
```yaml
openFuyaoVersion: v1.2.0
kubernetesVersion: v1.28.0
containerdVersion: "1.7.0"
etcdVersion: "3.5.9"
```

**Patch Addon 结构** ([types.go: PatchConfig / AddonInfo](file:///d:\code\github\installer-service\pkg\installer\types.go#L205-L216))：
```yaml
addons:
  - name: coredns
    version: "1.9.3"
    param:
      EnableAntiAffinity: "true"
    block: true
```

**处理逻辑**：
1. **在线模式**：直接拒绝，返回错误 "online mode detected, upload patch file not supported"
2. **离线模式**：
   - 解析 YAML 内容，校验 `PatchVersionInfo` 必填字段
   - 将 patch 内容写入 ConfigMap（namespace: `openfuyao-patch`，key 格式：`cm.<version>`）
   - 更新 BKE 配置 ConfigMap，添加 `patch.<version>` → `cm.<version>` 映射

**响应**：
- 200：上传成功
- 400：参数为空
- 500：上传失败

### 3. GET `/versions` — 获取可安装版本列表

**请求参数**：无

**处理逻辑** ([cluster.go: GetOpenFuyaoVersions](file:///d:\code\github\installer-service\pkg\installer\cluster.go#L1544-L1576))：
1. 调用 `GetDeployVersions()` 获取所有部署版本
2. 过滤掉补丁版本（patch version > 0 的版本），只保留可直接安装的主版本
3. 特殊处理：`latest` 版本始终保留

**版本过滤规则**：
- 版本号格式：`major.minor.patch`
- `patch > 0` 的版本为补丁版本，不可直接安装
- `major.minor.0` 的版本为可直接安装版本

**响应**：
```json
{
  "code": 200,
  "message": "success",
  "data": {
    "versions": ["v1.0.0", "v1.1.0", "v2.0.0"]
  }
}
```

### 4. GET `/clusters/{cluster-name}/upgrade-versions` — 获取可升级版本列表

**请求参数**：
- 路径参数：`cluster-name`（集群名称）
- 查询参数（可选）：`currentVersion`（当前版本，若不传则从集群 CR 自动获取）

**处理逻辑** ([cluster.go: GetOpenFuyaoUpgradeVersions](file:///d:\code\github\installer-service\pkg\installer\cluster.go#L1578-L1618))：
1. 获取当前集群版本（优先从 BKECluster CR 读取 `spec.clusterConfig.cluster.openFuyaoVersion`）
2. 调用 `GetUpgradeVersions()` 获取所有升级版本
3. 按规则筛选可升级版本

**版本筛选规则**（设当前版本为 `curMajor.curMinor.curPatch`，候选版本为 `vMajor.vMinor.vPatch`）：

| 条件 | 规则 | 说明 |
|------|------|------|
| `curMajor == vMajor && curMinor == vMinor` | `vPatch > curPatch` → 可升级 | 同主版本的补丁升级 |
| `curMajor == vMajor && curMinor < vMinor` | `vPatch == 0` → 可升级 | 同 Major 的 Minor 升级，只允许非补丁版本 |
| `curMajor < vMajor` | `vPatch == 0` → 可升级 | 跨 Major 升级，只允许非补丁版本 |
| `currentVersion == "latest"` | 返回空列表 | latest 版本不可升级 |

**响应**：
```json
{
  "code": 200,
  "message": "success",
  "data": {
    "versions": ["v1.0.1", "v1.1.0", "v2.0.0"]
  }
}
```

### 5. POST `/clusters/{cluster-name}/auto-upgrade` — 自动升级补丁准备

**请求体**：
```json
{
  "patchDir": "/data/patches",
  "patchName": "patchV1"
}
```

**请求类型定义** ([types.go: AutoUpgradeRequest](file:///d:\code\github\installer-service\pkg\installer\types.go#L56-L59))：
```go
type AutoUpgradeRequest struct {
    PatchPath string `json:"patchDir"`   // 补丁文件所在目录路径
    PatchName string `json:"patchName"`  // 补丁文件名称（不含扩展名）
}
```

**处理逻辑** ([auto_upgrade.go: AutoUpgradePatchPrepare](file:///d:\code\github\installer-service\pkg\installer\auto_upgrade.go#L312-L352))：
1. **参数校验**：`patchPath` 和 `patchName` 不能为空
2. **文件验证**：
   - 路径标准化（防路径遍历攻击）
   - 文件名标准化（防命令注入）
   - 验证目录存在
   - 验证 `<patchPath>/<patchName>.tar.gz` 文件存在且为合法 tar.gz 格式
3. **获取 Bootstrap IP**：从 BKECluster CR 的 `spec.clusterConfig.cluster.imageRepo.ip` 提取
4. **判断在线/离线模式**：读取 ConfigMap 判断
5. **构建并执行升级脚本**：
   - 步骤1：检查离线包文件是否存在
   - 步骤2：解压离线包 (`tar -xzvf`)
   - 步骤3：**仅离线模式** — 同步镜像到本地仓库 (`bke registry patch --source --target`)
   - 步骤4：复制 containerd/kubelet/kubectl 二进制文件到 BKE 挂载目录

**升级脚本构建** ([auto_upgrade.go: buildUpgradeScript](file:///d:\code\github\installer-service\pkg\installer\auto_upgrade.go#L101-L105))：
```go
func buildUpgradeScript(patchPath, patchName, bootstrapIp string, online bool) string {
    header := fmt.Sprintf(bkeUpgradeScriptHeader, patchPath, patchName, bootstrapIp)
    ret := header + bkeUpgradeScriptFileCheck + bkeUpgradeScriptExtract
    if !online {
        ret += bkeUpgradeScriptSyncImages // 离线场景才需要镜像同步
    }
    return ret + bkeUpgradeScriptCopyFiles
}
```

**响应**：
- 200：升级准备成功
- 400：参数为空
- 500：升级准备失败（含具体错误信息）

## 三、升级相关数据结构汇总

| 结构体 | 位置 | 用途 |
|--------|------|------|
| `UpgradeRequest` | [types.go:188](file:///d:\code\github\installer-service\pkg\installer\types.go#L188) | 升级请求体（version 字段） |
| `PatchFileData` | [types.go:172](file:///d:\code\github\installer-service\pkg\installer\types.go#L172) | 上传 patch 文件请求体 |
| `PatchVersionInfo` | [types.go:196](file:///d:\code\github\installer-service\pkg\installer\types.go#L196) | Patch 文件中的版本信息 |
| `PatchConfig` | [types.go:205](file:///d:\code\github\installer-service\pkg\installer\types.go#L205) | Patch 文件中的 Addon 配置 |
| `AddonInfo` | [types.go:209](file:///d:\code\github\installer-service\pkg\installer\types.go#L209) | Addon 详情（name/version/param/block） |
| `AutoUpgradeRequest` | [types.go:56](file:///d:\code\github\installer-service\pkg\installer\types.go#L56) | 自动升级请求体（patchDir/patchName） |
| `AutoUpgradeResponse` | [types.go:62](file:///d:\code\github\installer-service\pkg\installer\types.go#L62) | 自动升级响应体（taskID） |
| `AutoUpgradeStatusResponse` | [types.go:67](file:///d:\code\github\installer-service\pkg\installer\types.go#L67) | 自动升级状态响应体（taskID/status/log） |
| `ClusterPatchInfo` | [types.go:178](file:///d:\code\github\installer-service\pkg\installer\types.go#L178) | 集群版本信息（clusterVersion） |
| `RemotePatchIndexResponse` | [types.go:193](file:///d:\code\github\installer-service\pkg\installer\types.go#L193) | 远程 patch 索引响应 |

## 四、升级相关 Operation 接口方法汇总

定义在 [interface.go: Operation](file:///d:\code\github\installer-service\pkg\installer\interface.go#L67-L80)：

| 方法签名 | 功能 |
|----------|------|
| `UploadPatchFile(fileName, fileContent string) error` | 上传升级 patch 文件到 ConfigMap |
| `GetOpenFuyaoVersions() ([]string, error)` | 获取可安装的版本列表（过滤补丁版本） |
| `GetOpenFuyaoUpgradeVersions(currentVersion string) ([]string, error)` | 获取可升级的版本列表（基于当前版本筛选） |
| `AutoUpgradePatchPrepare(clusterName string, req AutoUpgradeRequest) error` | 执行自动升级补丁准备脚本 |
| `UpgradeOpenFuyao(clusterName, version string) error` | Patch BKECluster CR 的 openFuyaoVersion |
| `PatchYaml(object string, isUpgrade bool) error` | 通用 YAML Patch（扩容/升级共用） |

## 五、升级流程总览

```
┌──────────────────────────────────────────────────────────────────┐
│                       升级完整流程                                │
├──────────────────────────────────────────────────────────────────┤
│                                                                  │
│  1. 查询可升级版本                                                │
│     GET /clusters/{name}/upgrade-versions                        │
│     └─► 从 BKECluster CR 读取当前版本                            │
│     └─► GetOpenFuyaoUpgradeVersions() 筛选可升级版本              │
│                                                                  │
│  2a. 离线模式：上传 Patch 文件                                    │
│     POST /patches                                                │
│     └─► 解析 PatchVersionInfo                                    │
│     └─► 写入 ConfigMap (openfuyao-patch namespace)               │
│     └─► 更新 BKE ConfigMap 添加 patch.<ver> → cm.<ver> 映射     │
│                                                                  │
│  2b. 在线模式：自动从远程仓库拉取                                  │
│     └─► getVersionsFromOnline() 从 OBS 拉取 index.yaml           │
│     └─► 自动同步 patch 信息到 ConfigMap                          │
│                                                                  │
│  3. 执行升级准备                                                  │
│     POST /clusters/{name}/auto-upgrade                           │
│     └─► 验证补丁包 (tar.gz 格式校验)                             │
│     └─► 获取 Bootstrap IP                                        │
│     └─► 构建升级脚本                                             │
│         ├─► 检查离线包文件                                        │
│         ├─► 解压离线包                                           │
│         ├─► [离线] 同步镜像到本地仓库                             │
│         └─► 复制二进制文件 (containerd/kubelet/kubectl)           │
│     └─► 执行升级脚本                                             │
│                                                                  │
│  4. 触发集群升级                                                 │
│     POST /clusters/{name}/upgrade                                │
│     └─► UpgradeOpenFuyao() Patch BKECluster CR                   │
│     └─► 修改 spec.clusterConfig.cluster.openFuyaoVersion         │
│     └─► cluster-api-provider-bke 控制器检测变更并执行实际升级     │
│                                                                  │
└──────────────────────────────────────────────────────────────────┘
```

## 六、关键常量与配置

定义在 [constant.go](file:///d:\code\github\installer-service\pkg\constant\constant.go#L33-L48)：

| 常量 | 值 | 用途 |
|------|-----|------|
| `PatchPath` | `/bke/mount/source_registry/files/patches` | 升级补丁文件存储路径 |
| `PatchKeyPrefix` | `patch.` | ConfigMap 中 patch 键前缀 |
| `PatchValuePrefix` | `cm.` | ConfigMap 中 patch 值前缀 |
| `PatchNameSpace` | `openfuyao-patch` | Patch ConfigMap 所在命名空间 |
| `DefaultRemoteDeployPath` | `https://openfuyao.obs.cn-north-4.myhuaweicloud.com/openFuyao/version-config/latest/` | 在线模式部署文件远程路径 |
| `DefaultRemotePatchPath` | `https://openfuyao.obs.cn-north-4.myhuaweicloud.com/openFuyao/version-config/` | 在线模式补丁文件远程路径 |

## 七、注意事项与潜在问题
1. **`AutoUpgradeResponse` 和 `AutoUpgradeStatusResponse` 已定义但未使用**：[types.go:62-71](file:///d:\code\github\installer-service\pkg\installer\types.go#L62-L71) 中定义了包含 `taskID` 的响应结构，但当前 `autoUpgrade` handler 是同步执行的，直接返回成功/失败，未实现异步任务机制。
2. **`PatchYaml(object, isUpgrade)` 方法在升级流程中未被直接调用**：当前的升级流程使用 `UpgradeOpenFuyao()` 直接 Patch BKECluster CR，而 `PatchYaml` 是更通用的 YAML Patch 方法，两者存在功能重叠。
3. **在线/离线模式判断**：通过 ConfigMap 中的 `otherRepo` 和 `onlineImage` 字段判断，在线模式下禁止上传 patch 文件，但会自动从远程仓库拉取。
4. **版本筛选逻辑**：`GetOpenFuyaoUpgradeVersions` 的筛选规则确保了升级路径的合理性——补丁版本只能从同主版本升级，跨 Minor/Major 版本只能升级到非补丁版本。
        

# 📡 `installer-service` RESTful API 规格说明书
pkg\api\clustermanage中的代码实现，并梳理每个restful的业务规格与功能

> **Base Path**: `/rest/cluster/v1` (REST) | `/ws/cluster/v1` (WebSocket)  
> **框架**: `go-restful/v3` | **风格**: RESTful + WebSocket

## 一、集群管理 API (`registerClusterRoutes`)

### 1.1 创建集群
| 规格项 | 说明 |
| :--- | :--- |
| **Method** | `POST` |
| **Path** | `/clusters` |
| **Handler** | `createCluster` |
| **功能** | 接收集群配置参数，构建 BKECluster CR YAML 并提交到 K8s 集群 |

**请求示例**:
```json
POST /rest/cluster/v1/clusters
{
  "cluster": {
    "name": "prod-cluster-01",
    "openFuyaoVersion": "v2.5.0",
    "imageRepo": {
      "url": "https://registry.example.com",
      "ip": "192.168.1.100"
    }
  },
  "controlPlaneEndpoint": "192.168.1.10:6443",
  "addons": [
    { "name": "calico", "params": { "version": "v3.26" } }
  ],
  "nodes": [
    {
      "hostname": "master-01",
      "ip": "192.168.1.11",
      "port": "22",
      "username": "root",
      "password": "***",
      "role": ["master", "etcd"]
    }
  ]
}
```

**响应示例**:
```json
HTTP/1.1 200 OK
{
  "code": 200,
  "message": "success",
  "data": null
}
```

### 1.2 获取集群列表
| 规格项 | 说明 |
| :--- | :--- |
| **Method** | `GET` |
| **Path** | `/clusters` |
| **Handler** | `listClusterGet` |
| **功能** | 获取所有已创建的集群摘要信息列表 |

**响应示例**:
```json
HTTP/1.1 200 OK
{
  "code": 200,
  "message": "success",
  "data": {
    "items": [
      {
        "name": "prod-cluster-01",
        "openFuyaoVersion": "v2.5.0",
        "kubernetesVersion": "v1.28.0",
        "containerdVersion": "v1.7.0",
        "status": "Running",
        "createTime": "2025-05-10T10:00:00Z",
        "nodeSum": 5,
        "isAmd": true,
        "isArm": false,
        "addons": ["calico", "coredns"],
        "osList": ["Ubuntu 22.04"],
        "containerRuntime": "containerd"
      }
    ]
  }
}
```

### 1.3 获取集群详情（含节点）
| 规格项 | 说明 |
| :--- | :--- |
| **Method** | `GET` |
| **Path** | `/clusters/{cluster-name}` |
| **Handler** | `getClusterFull` |
| **功能** | 获取指定集群的详细配置及关联的节点列表 |

**响应示例**:
```json
HTTP/1.1 200 OK
{
  "code": 200,
  "message": "success",
  "data": {
    "clusterName": "prod-cluster-01",
    "clusterStatus": "Running",
    "openFuyaoVersion": "v2.5.0",
    "createTime": "2025-05-10T10:00:00Z",
    "isHA": true,
    "imageRepo": { "url": "...", "ip": "..." },
    "httpRepo": { "url": "...", "ip": "..." },
    "nodes": [
      {
        "hostname": "master-01",
        "ip": "192.168.1.11",
        "role": ["master", "etcd"],
        "cpu": 8,
        "memory": 32.0,
        "architecture": "amd64",
        "status": "Ready",
        "os": "Ubuntu 22.04"
      }
    ]
  }
}
```

### 1.4 删除集群
| 规格项 | 说明 |
| :--- | :--- |
| **Method** | `DELETE` |
| **Path** | `/clusters/{cluster-name}` |
| **Handler** | `deleteCluster` |
| **功能** | 触发集群删除流程（设置 DeletionTimestamp，由 Controller 执行清理） |

**响应示例**:
```json
HTTP/1.1 200 OK
{
  "code": 200,
  "message": "success",
  "data": null
}
```

## 二、节点管理 API (`registerNodeRoutes`)

### 2.1 节点校验
| 规格项 | 说明 |
| :--- | :--- |
| **Method** | `POST` |
| **Path** | `/nodes/validate` |
| **Handler** | `judgeClusterNode` |
| **功能** | 校验节点信息是否符合集群部署要求（网络连通性、资源规格等） |

**请求示例**:
```json
POST /rest/cluster/v1/nodes/validate
{
  "nameSpace": "prod-cluster-01",
  "nodes": [
    {
      "hostname": "worker-01",
      "ip": "192.168.1.21",
      "port": "22",
      "username": "root",
      "password": "***",
      "role": ["worker"]
    }
  ],
  "balanceIp": "192.168.1.10"
}
```

**响应示例 (成功)**:
```json
HTTP/1.1 200 OK
{ "code": 200, "message": "success", "data": null }
```

**响应示例 (失败)**:
```json
HTTP/1.1 400 Bad Request
{
  "code": 400,
  "message": "node 192.168.1.21 ssh connection failed: timeout",
  "data": null
}
```

### 2.2 获取节点列表
| 规格项 | 说明 |
| :--- | :--- |
| **Method** | `GET` |
| **Path** | `/clusters/{cluster-name}/nodes` |
| **Handler** | `listNode` |
| **Query** | `nodeName` (可选，过滤指定节点) |
| **功能** | 获取集群内所有节点或指定节点的详细信息 |

**响应示例**:
```json
HTTP/1.1 200 OK
{
  "code": 200,
  "message": "success",
  "data": {
    "items": [
      {
        "hostname": "worker-01",
        "ip": "192.168.1.21",
        "role": ["worker"],
        "cpu": 16,
        "memory": 64.0,
        "architecture": "arm64",
        "status": "Ready",
        "os": "Kylin V10"
      }
    ]
  }
}
```

### 2.3 集群扩容
| 规格项 | 说明 |
| :--- | :--- |
| **Method** | `POST` |
| **Path** | `/clusters/{cluster-name}/scale-up` |
| **Handler** | `scaleUpCluster` |
| **功能** | 向集群添加新节点（创建 BKENode CR） |

**请求示例**:
```json
POST /rest/cluster/v1/clusters/prod-cluster-01/scale-up
{
  "nodes": [
    {
      "hostname": "worker-02",
      "ip": "192.168.1.22",
      "port": "22",
      "username": "root",
      "password": "***",
      "role": ["worker"]
    }
  ]
}
```

**响应示例**:
```json
HTTP/1.1 200 OK
{ "code": 200, "message": "success", "data": null }
```

### 2.4 集群缩容
| 规格项 | 说明 |
| :--- | :--- |
| **Method** | `POST` |
| **Path** | `/clusters/{cluster-name}/scale-down` |
| **Handler** | `scaleDownCluster` |
| **功能** | 从集群移除指定节点（删除 BKENode CR） |

**请求示例**:
```json
POST /rest/cluster/v1/clusters/prod-cluster-01/scale-down
{
  "nodes": ["worker-01", "worker-02"]
}
```

**响应示例**:
```json
HTTP/1.1 200 OK
{ "code": 200, "message": "success", "data": null }
```

## 三、升级管理 API (`registerUpgradeRoutes`)

### 3.1 升级集群
| 规格项 | 说明 |
| :--- | :--- |
| **Method** | `POST` |
| **Path** | `/clusters/{cluster-name}/upgrade` |
| **Handler** | `upgradeCluster` |
| **功能** | 触发集群升级，Patch BKECluster CR 的 `openFuyaoVersion` 字段 |

**请求示例**:
```json
POST /rest/cluster/v1/clusters/prod-cluster-01/upgrade
{
  "version": "v2.6.0"
}
```

**响应示例**:
```json
HTTP/1.1 200 OK
{ "code": 200, "message": "success", "data": null }
```

### 3.2 上传补丁文件
| 规格项 | 说明 |
| :--- | :--- |
| **Method** | `POST` |
| **Path** | `/patches` |
| **Handler** | `uploadPatchFile` |
| **功能** | 上传离线升级补丁 YAML 文件，存储到 ConfigMap |

**请求示例**:
```json
POST /rest/cluster/v1/patches
{
  "PatchFileName": "patch-v2.5.0-to-v2.6.0.yaml",
  "PatchFileContent": "openFuyaoVersion: v2.6.0\nkubernetesVersion: v1.29.0\netcdVersion: v3.5.12\n..."
}
```

**响应示例**:
```json
HTTP/1.1 200 OK
{ "code": 200, "message": "success", "data": null }
```

### 3.3 获取可用版本列表
| 规格项 | 说明 |
| :--- | :--- |
| **Method** | `GET` |
| **Path** | `/versions` |
| **Handler** | `getOpenFuyaoVersions` |
| **功能** | 获取所有可安装的 openFuyao 版本（过滤 Patch 版本） |

**响应示例**:
```json
HTTP/1.1 200 OK
{
  "code": 200,
  "message": "success",
  "data": {
    "versions": ["v2.4.0", "v2.5.0", "v2.6.0", "latest"]
  }
}
```

### 3.4 获取可升级版本
| 规格项 | 说明 |
| :--- | :--- |
| **Method** | `GET` |
| **Path** | `/clusters/{cluster-name}/upgrade-versions` |
| **Handler** | `getUpgradeOpenFuyaoVersions` |
| **Query** | `currentVersion` (可选) |
| **功能** | 根据当前版本计算合法的可升级目标版本列表 |

**响应示例**:
```json
HTTP/1.1 200 OK
{
  "code": 200,
  "message": "success",
  "data": {
    "versions": ["v2.6.0", "v2.7.0"]
  }
}
```

### 3.5 自动升级准备
| 规格项 | 说明 |
| :--- | :--- |
| **Method** | `POST` |
| **Path** | `/clusters/{cluster-name}/auto-upgrade` |
| **Handler** | `autoUpgrade` |
| **功能** | 同步执行升级前准备脚本（离线包解压、镜像导入、二进制替换） |

**请求示例**:
```json
POST /rest/cluster/v1/clusters/prod-cluster-01/auto-upgrade
{
  "patchDir": "/opt/patches",
  "patchName": "patch-v2.5.0-to-v2.6.0"
}
```

**响应示例**:
```json
HTTP/1.1 200 OK
{ "code": 200, "message": "success", "data": null }
```

**响应示例 (失败)**:
```json
HTTP/1.1 500 Internal Server Error
{
  "code": 500,
  "message": "Auto upgrade preparation failed: patch directory not found",
  "data": null
}
```

## 四、配置管理 API (`registerConfigRoutes`)

### 4.1 获取默认配置
| 规格项 | 说明 |
| :--- | :--- |
| **Method** | `GET` |
| **Path** | `/configs` |
| **Handler** | `getDefaultConfig` |
| **功能** | 获取离线安装所需的默认配置（镜像仓库、HTTP 仓库、IP 等） |

**响应示例**:
```json
HTTP/1.1 200 OK
{
  "code": 200,
  "message": "success",
  "data": {
    "imageRepo": { "url": "https://registry.example.com", "ip": "192.168.1.100" },
    "httpRepo": { "url": "https://packages.example.com", "ip": "192.168.1.101" },
    "ip": "192.168.1.10",
    "kubernetesVersion": "v1.28.0",
    "containerRuntime": "containerd",
    "agentHealthPort": "3377"
  }
}
```

## 五、WebSocket API

### 5.1 实时集群日志
| 规格项 | 说明 |
| :--- | :--- |
| **Protocol** | `WebSocket` |
| **Path** | `/ws/cluster/v1/clusters/{cluster-name}/logs` |
| **Handler** | `getClusterLog` |
| **功能** | 建立 WebSocket 连接，实时推送集群部署/升级事件日志 |

**连接示例**:
```
ws://installer-service:8080/ws/cluster/v1/clusters/prod-cluster-01/logs
```

**推送消息格式**:
```
Time:2025-05-10 10:05:00, Type: Normal, Reason: PhaseStarted, Message: EnsureMasterInit started 
Time:2025-05-10 10:06:30, Type: Normal, Reason: PhaseCompleted, Message: EnsureMasterInit completed 
Time:2025-05-10 10:10:00, Type: Warning, Reason: PhaseRetry, Message: EnsureWorkerJoin retrying... 
```
