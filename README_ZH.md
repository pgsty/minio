<h1 align="center">
  <img src=".github/silo.svg" alt="" height="80">
  <img src=".github/silo-word.svg" alt="SILO" height="80">
</h1>

<p align="center">
  <strong>审慎维护的 S3 兼容对象存储</strong><br>
  为现有部署提供安全维护、带版本的发行产物与持续运维支持。
</p>

<p align="center">
  <a href="https://silo.pgsty.com/zh/">官网</a> ·
  <a href="https://silo.pgsty.com/zh/docs/">文档</a> ·
  <a href="https://silo.pgsty.com/zh/download/">下载</a> ·
  <a href="https://silo.pgsty.com/zh/blog/">博客</a> ·
  <a href="https://github.com/pgsty/silo/releases">版本发布</a> ·
  <a href="SECURITY.md">安全策略</a> ·
  <a href="README.md">English</a>
</p>

<p align="center">
  <a href="https://github.com/pgsty/silo/releases"><img alt="GitHub Release" src="https://img.shields.io/github/v/release/pgsty/silo?include_prereleases&label=release&logo=github"></a>
  <a href="https://hub.docker.com/r/pgsty/silo"><img alt="Docker Pulls" src="https://img.shields.io/docker/pulls/pgsty/silo?logo=docker"></a>
  <a href="go.mod"><img alt="Go Version" src="https://img.shields.io/github/go-mod/go-version/pgsty/silo?logo=go"></a>
  <a href="LICENSE"><img alt="License" src="https://img.shields.io/badge/license-AGPLv3-blue"></a>
</p>

> [!IMPORTANT]
> Silo 是由 [Pigsty](https://pigsty.cc) 独立维护、从 [`pgsty/silo`](https://github.com/pgsty/silo) 发布的开源 MinIO 社区分支。本项目与 MinIO, Inc. 不存在隶属、背书或赞助关系；文中使用 “MinIO” 仅用于说明上游项目及兼容谱系。

> [!NOTE]
> 2026-08-06，本仓库由 `pgsty/minio` 更名为 `pgsty/silo`，默认分支由 `master` 更名为 `main`。如果你需要以原本 MinIO 形态维持的归档构件，它们位于归档的 [`minio`](https://github.com/pgsty/silo/tree/minio) 分支，以及截止到 [`RELEASE.2026-08-04T00-00-00Z`](https://github.com/pgsty/silo/releases/tag/RELEASE.2026-08-04T00-00-00Z) 的历次发布中；这些资产与 `docker.io/pgsty/minio` 镜像保持已发布状态且不做改动。本次更名只改变产品与交付物名称 —— `MINIO_*` 变量、`minio_*` 指标、`x-minio-*` 头、`/minio/*` 路由与磁盘格式均保持不变。

## 概述

Silo 维护一条源自开源 MinIO 服务端的下游版本线，为上游停止社区发行后仍在运行 MinIO 兼容部署的用户提供持续构建与发行产物。

Pigsty 使用本分支提供对象存储，包括 PostgreSQL 备份存储。

项目统一门户为 [silo.pgsty.com](https://silo.pgsty.com/zh/)，集中提供文档、下载安装、版本与安全动态及项目背景。中文内容位于 `/zh/`，英文内容位于站点根路径。

## 按需求选择入口

| 需求 | 权威入口 |
| :-- | :-- |
| 项目概览与全站导航 | [Silo 中文门户](https://silo.pgsty.com/zh/)（[English](https://silo.pgsty.com/)） |
| 安装方式与软件下载 | [下载与安装](https://silo.pgsty.com/zh/download/)（[English](https://silo.pgsty.com/download/)） |
| 运维、管理、开发与参考指南 | [中文文档](https://silo.pgsty.com/zh/docs/)（[English](https://silo.pgsty.com/docs/)） |
| 项目动态、版本说明与安全通告 | [博客](https://silo.pgsty.com/zh/blog/)，包括[版本发布](https://silo.pgsty.com/zh/blog/release/)与[安全通告](https://silo.pgsty.com/zh/blog/security/) |
| 带版本的二进制、校验和与源码归档 | [GitHub Releases](https://github.com/pgsty/silo/releases) |
| 缺陷报告与功能讨论 | [GitHub Issues](https://github.com/pgsty/silo/issues) |
| 私密漏洞报告 | [`SECURITY.md`](SECURITY.md) 与 [`VULNERABILITY_REPORT.md`](VULNERABILITY_REPORT.md) |
| 许可证、署名与商标信息 | [许可证](https://silo.pgsty.com/zh/about/license/)、[署名归属](https://silo.pgsty.com/zh/about/attribution/)与[商标政策](https://silo.pgsty.com/zh/about/trademark/) |

## 维护政策

活跃版本线的维护范围包括：

- 构建与依赖项维护；
- 适用的安全修复与公告；
- 针对可复现缺陷的范围明确的修复；
- 带版本的二进制、软件包、校验和与多架构镜像；
- Web Console、客户端、文档与 Pigsty 集成。

改动保持克制，并在可行时提供测试。所有维护均为尽力而为，不承诺固定的响应、修复或发布时间。

### 范围之外

- 独立产品路线图、新存储引擎或假设性的 S3 新特性；
- 大规模重写或显著扩大下游差异的改动；
- 历史版本或多条支持分支；
- 商业支持、SLA、7×24 服务或 SUBNET 服务；
- 部署设计、访问控制、监控、备份与恢复。

## 兼容策略

Silo 尽量保留：

- `github.com/minio/minio` module path 与 `github.com/minio/*` 导入路径；
- MinIO 兼容的 S3 API、线协议、`MINIO_*` 环境变量、指标、协议头、保留路由与存储元数据；
- `RELEASE.YYYY-MM-DDTHH-MM-SSZ` 标签，以及容器入口对旧式 `minio …` 参数的转换。

Silo 自有交付面统一使用 `silo` 可执行文件、软件包、服务、Helm Chart 与 `pgsty/silo` 容器镜像；原生交付物不会安装 `minio` 二进制别名。

兼容性是默认约束。只要不会留下安全问题，Silo 就保留既有的协议、客户端、配置与运维行为；只有在修复重大安全问题确有必要时才会打破兼容，并在版本说明中明确受影响行为与迁移方式。每个版本仍应视为下游升级：锁定版本，阅读[版本说明](https://silo.pgsty.com/zh/blog/release/)与[安全公告](docs/security/advisories.md)，保留回滚路径，并在生产使用前完成测试。

## 下载与发行产物

请先在[下载与安装](https://silo.pgsty.com/zh/download/)页面选择合适的安装方式；GitHub Releases 仍是带版本服务端二进制、校验和与源码归档的获取位置。

| 产物 | 位置 |
| :-- | :-- |
| 源码 | [`github.com/pgsty/silo`](https://github.com/pgsty/silo) |
| 容器镜像 | [`pgsty/silo`](https://hub.docker.com/r/pgsty/silo)，支持 `linux/amd64` 与 `linux/arm64` 多架构清单 |
| 服务端二进制与校验和 | [GitHub Releases](https://github.com/pgsty/silo/releases)，覆盖 Linux、macOS、Windows 的 `amd64` 与 `arm64` |
| Linux 软件包 | RPM、DEB、APK，并通过 [Pigsty 软件仓库](https://pigsty.cc/docs/repo/) 分发 |
| 客户端 | [`pgsty/mc`](https://github.com/pgsty/mc)，容器内以 `mcli` 提供，并保留 `mc` 兼容别名 |
| 管理控制台 | [`pgsty/silo-console`](https://github.com/pgsty/silo-console)，通过兼容导入路径 `github.com/minio/console` 嵌入服务端构建 |
| 共享库 | [`pgsty/silo-pkg`](https://github.com/pgsty/silo-pkg) v3.11.0，通过 `replace` 指令使用，同时保留 `github.com/minio/pkg/v3` 导入路径 |

每个新版本都会为各平台归档和软件包发布 SPDX JSON SBOM。归档与软件包的校验和清单分别带有无长期密钥的 Sigstore 签名包；GitHub 制品证明则为全部可下载产物及多架构容器镜像记录已签名的构建来源。

下载归档及配套文件后，请分别验证完整性、已发布 SBOM、签名清单与构建来源：

```bash
# 完整性：选择与你下载产物相符的一行。
grep -F '  silo_<version>_linux_amd64.tar.gz' \
  silo_<version>_checksums.txt | sha256sum --check

# 归档 SBOM 是另一个独立校验的 Release 产物。
grep -F '  silo_<version>_linux_amd64.tar.gz.sbom.json' \
  silo_<version>_checksums.txt | sha256sum --check

# 验证归档/SBOM 校验和清单的签名。
cosign verify-blob \
  --bundle silo_<version>_checksums.txt.sigstore.json \
  --certificate-identity-regexp \
    '^https://github.com/pgsty/(minio|silo)/\.github/workflows/release\.yml@refs/(tags/RELEASE\..+|heads/(master|main))$' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  silo_<version>_checksums.txt

# 在线验证本仓库签发的构建来源。
gh attestation verify silo_<version>_linux_amd64.tar.gz \
  --repo pgsty/silo
```

软件包使用 `silo_<version>_packages_checksums.txt` 及其相邻的 Sigstore
签名包，并使用相同的 identity 与 issuer 约束；请用 SPDX 工具检查已验证的
SPDX JSON SBOM。按 digest 验证多架构容器清单的构建来源：

```bash
gh attestation verify \
  oci://index.docker.io/pgsty/silo@sha256:<manifest-digest> \
  --repo pgsty/silo
```

分架构 SBOM 证明附在 `amd64` 与 `arm64` 平台镜像的 digest 上，而非多架构
清单上，需要分别显式验证：

```bash
gh attestation verify \
  oci://index.docker.io/pgsty/silo@sha256:<platform-digest> \
  --repo pgsty/silo \
  --predicate-type https://spdx.dev/Document/v2.3
```

按 digest 验证可避免信任可变镜像标签。

### 原生软件包迁移

`silo` RPM、DEB 与 APK 不针对 `minio` 声明 `Provides`、`Obsoletes`、
`Replaces` 或包级 `Conflicts`，因此可以与已有 MinIO 软件包并存安装，
不会被普通升级静默替换。两个 systemd unit 在运行时互斥，应由管理员显式
切换，不能同时启动。

切换前请记录旧 unit 的 enabled/active 状态与 `User`/`Group`，并备份
`/etc/default/minio`。Silo 先读取该旧配置，再读取 `/etc/default/silo`；后者
中由管理员设置的同名变量优先。如现有数据必须继续使用原 UID/GID，请创建
`/etc/systemd/system/silo.service.d/10-legacy-user.conf`：

```ini
[Service]
User=<legacy-user>
Group=<legacy-group>
```

执行 `systemctl daemon-reload`，先停用并停止 `minio.service`，再启用并启动
`silo.service`。确认健康检查、S3、Admin API、指标与日志后，才 mask 或卸载
旧服务。软件包迁移期间不要递归修改数据属主；在回滚观察窗口内保留旧包与
旧 unit。

## 快速开始

本地体验：

```bash
mkdir -p data

export MINIO_ROOT_USER=minioadmin
export MINIO_ROOT_PASSWORD=change-me-long-password

docker run -d --name silo \
  -p 9000:9000 \
  -p 9001:9001 \
  -e MINIO_ROOT_USER \
  -e MINIO_ROOT_PASSWORD \
  -v "$PWD/data:/data" \
  docker.io/pgsty/silo:latest server /data --console-address ":9001"
```

管理控制台位于 <http://localhost:9001>，S3 API 位于 <http://localhost:9000>。

镜像内置兼容客户端 `mcli`：

```bash
docker exec silo mcli alias set local http://127.0.0.1:9000 \
  "$MINIO_ROOT_USER" "$MINIO_ROOT_PASSWORD"
docker exec silo mcli mb local/demo
docker exec silo mcli ls local
```

> [!WARNING]
> 生产环境应锁定版本，使用独立凭据与 TLS，配置监控，保留独立备份，并验证恢复流程。

从源码构建服务端：

```bash
go build -o silo .
./silo --version
```

其他安装方式（包括原生软件包、二进制、Podman、Kubernetes、源码构建与 Pigsty Ansible）请前往[下载与安装](https://silo.pgsty.com/zh/download/)；生产部署与管理请从 [Silo 中文文档](https://silo.pgsty.com/zh/docs/)开始。Pigsty 用户也可以直接使用 [Pigsty MinIO 模块](https://pigsty.cc/docs/minio/)。

## 安全

安全修复面向当前活跃开发分支，并记录在仓库[安全公告](docs/security/advisories.md)与门户[安全通告](https://silo.pgsty.com/zh/blog/security/)中。请按照 [`SECURITY.md`](SECURITY.md) 与 [`VULNERABILITY_REPORT.md`](VULNERABILITY_REPORT.md) 私密报告漏洞；同时影响上游 MinIO 的问题也应向上游报告。

## 参与贡献

欢迎安全与依赖项更新、可复现缺陷修复、测试、发布自动化、打包与文档改进。

Issue 与 Pull Request 应说明受影响版本、复现步骤、影响、预期行为、测试与兼容性说明。大型改动请先提交 Issue 讨论。

本项目不要求签署 CLA：贡献按项目许可证（AGPL-3.0-or-later，inbound=outbound）接收，贡献者保留自己的版权。每个提交都必须按照 [DCO](https://developercertificate.org/) 签署（`git commit -s`），详见 [`CONTRIBUTING.md`](CONTRIBUTING.md)。

## 背景

本项目源于上游社区发行与维护模式的变化。维护者对相关变化的分析、替代方案评估与早期维护记录见以下文章：

| 文章 | 主题 |
| :-- | :-- |
| [MinIO已死](https://silo.pgsty.com/zh/blog/post/minio-is-dead/) | 上游项目与发行模式的变化 |
| [MinIO已死，谁能接盘？](https://silo.pgsty.com/zh/blog/post/minio-alternative/) | 可选替代方案评估 |
| [MinIO 已死，MinIO 复生](https://silo.pgsty.com/zh/blog/post/minio-resurrect/) | 建立分支及其发行流水线 |
| [续命 MinIO：承诺兑现](https://silo.pgsty.com/zh/blog/post/minio-promise-kept/) | 初期安全与维护工作 |

## 许可证与商标

服务端继续采用 [GNU Affero General Public License v3.0](LICENSE) 发布。上游作者与署名信息见 [`CREDITS`](CREDITS) 与 [`NOTICE`](NOTICE)。

MinIO 是 MinIO, Inc. 的商标。Silo 是独立社区项目，与 MinIO, Inc. 不存在隶属或背书关系。
