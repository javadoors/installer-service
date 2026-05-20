# 代码解析
# 与**升级**相关的核心代码目录及其功能梳理
以下是 `installer-service` 中与**升级**相关的核心代码目录及其功能梳理：

### 1. API 接口层 (`pkg/api/clustermanage/`)
负责接收前端 HTTP 请求，进行参数校验并调用底层业务逻辑。

| 文件路径 | 核心函数 | 功能描述 |
| :--- | :--- | :--- |
| `handler.go` | `upgradeCluster` | **触发升级**：接收目标版本，调用 `UpgradeOpenFuyao` 修改 CR。 |
| | `getUpgradeOpenFuyaoVersions` | **查询可升级版本**：获取当前版本及可升级的目标版本列表。 |
| | `autoUpgrade` | **自动升级准备**：调用脚本执行升级前的环境准备（离线包解压等）。 |
| | `uploadPatchFile` | **上传补丁**：接收离线补丁 YAML 文件并存储。 |
| `register.go` | `RegisterUpgradeRoutes` | **路由注册**：定义 `/upgrade`, `/upgrade-versions`, `/auto-upgrade` 等 RESTful 路由。 |

### 2. 核心业务逻辑层 (`pkg/installer/`)
实现升级的具体业务逻辑，包括版本计算、CR 操作和补丁管理。

| 文件路径 | 核心函数 | 功能描述 |
| :--- | :--- | :--- |
| `cluster.go` | `UpgradeOpenFuyao` | **执行 Patch**：构造 JSON Patch 修改 `BKECluster` 的 `openFuyaoVersion` 字段，触发 Controller 调和。 |
| | `GetOpenFuyaoUpgradeVersions` | **版本过滤**：基于 SemVer 规则过滤出合法的升级目标版本（如排除 Patch 版本）。 |
| | `UploadPatchFile` | **存储补丁**：将补丁 YAML 内容写入 ConfigMap，建立版本映射。 |
| `auto_upgrade.go` | `AutoUpgradePatchPrepare` | **升级脚本编排**：生成并执行 Bash 脚本，完成离线镜像导入、二进制文件替换等操作。 |
| | `buildUpgradeScript` | **脚本构建**：拼接升级步骤（检查文件 -> 解压 -> 导入镜像 -> 替换二进制）。 |
| `utils.go` | `getPatchInfo` | **获取补丁信息**：从 ConfigMap 中读取特定版本的补丁配置。 |
| | `patchAddonsInfo` | **更新插件版本**：根据补丁配置更新集群 Addon 的版本信息。 |

### 3. 数据模型定义 (`pkg/installer/`)
定义升级相关的请求参数和响应结构。

| 文件路径 | 核心类型 | 功能描述 |
| :--- | :--- | :--- |
| `types.go` | `UpgradeRequest` | 升级请求体：包含目标 `Version`。 |
| | `AutoUpgradeRequest` | 自动升级请求体：包含补丁路径 `PatchPath` 和名称 `PatchName`。 |
| | `PatchFileData` | 补丁上传数据：包含文件名和内容。 |
| | `PatchVersionInfo` | 补丁版本信息：定义 K8s/Etcd/Containerd 的目标版本。 |
| `interface.go` | `Operation` 接口 | 声明 `UpgradeOpenFuyao`, `GetOpenFuyaoUpgradeVersions` 等方法签名。 |

### 4. 常量定义 (`pkg/constant/`)
存储升级相关的硬编码路径和键值。

| 文件路径 | 核心常量 | 功能描述 |
| :--- | :--- | :--- |
| `constant.go` | `PatchPath` | 补丁文件默认存储路径。 |
| | `PatchKeyPrefix` | ConfigMap 中存储补丁内容的 Key 前缀。 |
| | `DefaultRemotePatchPath` | 远程补丁下载路径。 |

### 5. 文档参考 (`code/`)
虽然不是代码，但包含升级逻辑的重要说明。

| 文件路径 | 内容描述 |
| :--- | :--- |
| `code/upgrade.md` | 详细的升级 API 文档、流程说明及补丁格式规范。 |
