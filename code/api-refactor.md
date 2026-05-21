# installer-service
## 升级管理 API (registerUpgradeRoutes)
### 升级集群
规格项	说明
Method	POST
Path	/clusters/{cluster-name}/upgrade
Handler	upgradeCluster
功能	触发集群升级，Patch BKECluster CR 的 openFuyaoVersion 字段

请求示例:
```txt
POST /rest/cluster/v1/clusters/prod-cluster-01/upgrade
{
  "version": "v2.6.0"
}
```
响应示例:
```txt
HTTP/1.1 200 OK
{ "code": 200, "message": "success", "data": null }
```
#### 重构
1. 先保持原功能：触发集群升级，Patch BKECluster CR 的 openFuyaoVersion 字段
2. 同时增加修改ClusterVersion的目标版本，从而触发升级调谐

### 上传补丁文件()

规格项	说明
Method	POST
Path	/patches
Handler	uploadPatchFile
功能	上传离线升级补丁 YAML 文件，存储到 ConfigMap

请求示例:
```txt
POST /rest/cluster/v1/patches
{
  "PatchFileName": "patch-v2.5.0-to-v2.6.0.yaml",
  "PatchFileContent": "openFuyaoVersion: v2.6.0\nkubernetesVersion: v1.29.0\netcdVersion: v3.5.12\n..."
}
```
响应示例:
```txt
HTTP/1.1 200 OK
{ "code": 200, "message": "success", "data": null }
```
#### 重构
> 删除此接口有没有问题？补丁问题还有没有其它用途？

### 获取可用版本列表
规格项	说明
Method	GET
Path	/versions
Handler	getOpenFuyaoVersions
功能	获取所有可安装的 openFuyao 版本（过滤 Patch 版本）
==》这个接口有什么用？
响应示例:
```txt
HTTP/1.1 200 OK
{
  "code": 200,
  "message": "success",
  "data": {
    "versions": ["v2.4.0", "v2.5.0", "v2.6.0", "latest"]
  }
}
```

这个接口主要用于 新建集群（安装）场景。
- 提供安装选项：当用户在前端创建新集群时，需要选择要安装的 openFuyao 版本。该接口返回所有可用的标准发行版本（如 v2.4.0, v2.5.0, latest）。
- 过滤补丁版本：它特意过滤掉了 Patch 版本（如 v2.5.1）。这是因为补丁版本通常是用于存量集群的升级修复，而不是作为新集群的初始安装基线。
- 数据源适配：根据环境（在线/离线），它会自动从远程仓库或本地 ConfigMap 获取版本列表。

总结：它是**“创建集群”页面的版本下拉框**的数据源。
#### 重构
从UpgradePath里查找可升级的直接路径：
- 从UpgradePath的CR里构建升级路径图


### 获取可升级版本
规格项	说明
Method	GET
Path	/clusters/{cluster-name}/upgrade-versions
Handler	getUpgradeOpenFuyaoVersions
Query	currentVersion (可选)
功能	根据当前版本计算合法的可升级目标版本列表

响应示例:
```txt
HTTP/1.1 200 OK
{
  "code": 200,
  "message": "success",
  "data": {
    "versions": ["v2.6.0", "v2.7.0"]
  }
}
```

#### 重构
从UpgradePath里查找可升级的直接路径：
- 从UpgradePath的CR里构建升级路径图

## 自动升级准备（TODO）
规格项	说明
Method	POST
Path	/clusters/{cluster-name}/auto-upgrade
Handler	autoUpgrade
功能	同步执行升级前准备脚本（离线包解压、镜像导入、二进制替换）
请求示例:
```txt
POST /rest/cluster/v1/clusters/prod-cluster-01/auto-upgrade
{
  "patchDir": "/opt/patches",
  "patchName": "patch-v2.5.0-to-v2.6.0"
}
```
响应示例:
```txt
HTTP/1.1 200 OK
{ "code": 200, "message": "success", "data": null }
```
响应示例 (失败):
```txt
HTTP/1.1 500 Internal Server Error
{
  "code": 500,
  "message": "Auto upgrade preparation failed: patch directory not found",
  "data": null
}
```

## 
