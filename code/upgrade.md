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
        
