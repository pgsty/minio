<h1 align="center">
  <a href="https://silo.pgsty.com/zh/">
    <img src=".github/silo-logo.svg" alt="Silo" width="160">
  </a>
</h1>


<p align="center">
  <strong>S3 兼容对象存储 —— 由 PGSTY 维护的 MinIO 社区分支</strong>
</p>


<p align="center">
  <a href="https://silo.pgsty.com/zh/">官网</a> ·
  <a href="https://silo.pgsty.com/zh/docs/">文档</a> ·
  <a href="https://silo.pgsty.com/zh/download/">下载</a> ·
  <a href="https://silo.pgsty.com/zh/tags/silo/">版本说明</a> ·
  <a href="https://silo.pgsty.com/zh/compatibility/server/">兼容性</a> ·
  <a href="https://silo.pgsty.com/zh/about/manifesto/">宣言</a> ·
  <a href="SECURITY.md">安全策略</a> ·
  <a href="README.md">English</a>
</p>

<p align="center">
  <a href="https://silo.pgsty.com/zh/"><img alt="官网" src="https://img.shields.io/badge/%E5%AE%98%E7%BD%91-silo.pgsty.com%2Fzh-1d588c"></a>
  <a href="https://github.com/pgsty/silo/releases"><img alt="GitHub Release" src="https://img.shields.io/github/v/release/pgsty/silo?include_prereleases&label=release&logo=github"></a>
  <a href="https://hub.docker.com/r/pgsty/silo"><img alt="Docker Pulls" src="https://img.shields.io/docker/pulls/pgsty/minio?logo=docker"></a>
  <a href="go.mod"><img alt="Go Version" src="https://img.shields.io/github/go-mod/go-version/pgsty/silo?logo=go"></a>
  <a href="LICENSE"><img alt="License" src="https://img.shields.io/badge/license-AGPLv3-blue"></a>
</p>

> [!IMPORTANT]
> **PGSTY Silo**（以下简称 Silo）是由 [Pigsty](https://pigsty.cc) 独立维护、从 [`pgsty/silo`](https://github.com/pgsty/silo) 发布的开源 MinIO 社区分支。本项目与 MinIO, Inc. 不存在隶属、背书或赞助关系；文中使用 “MinIO” 仅用于说明上游项目及兼容谱系。

> [!NOTE]
> 2026-08-06，本仓库由 `pgsty/minio` 更名为 `pgsty/silo`，默认分支由 `master` 更名为 `main`。以原 MinIO 形态维持的归档构件仍位于归档的 [`minio`](https://github.com/pgsty/silo/tree/minio) 分支，以及截止 [`RELEASE.2026-08-04T00-00-00Z`](https://github.com/pgsty/silo/releases/tag/RELEASE.2026-08-04T00-00-00Z) 的历次发布中。

## 概述

上游停止社区发行后，Silo 为开源 MinIO 服务端维护一条持续可用的版本线：构建、软件包、多架构镜像、安全修复与完整 Web 控制台。Pigsty 在生产环境中用它承载 PostgreSQL 备份存储。

它只遵循一条原则：**改名的是产品与交付物，不是协议与你的数据。** 其余内容都在 [silo.pgsty.com](https://silo.pgsty.com/zh/)。

**相关项目：**[`pgsty/mc`](https://github.com/pgsty/mc) 客户端（以 `mcli` 发行） · [`pgsty/silo-console`](https://github.com/pgsty/silo-console) · [`pgsty/silo-pkg`](https://github.com/pgsty/silo-pkg) · [`pgsty/pigsty`](https://github.com/pgsty/pigsty)

<p align="center">
  <img src="https://silo.pgsty.com/images/silo-console/console-metrics-simple.webp" alt="Silo 控制台">
</p>

## 快速上手

```bash
docker run -d --name silo -p 9000:9000 -p 9001:9001 \
  -e MINIO_ROOT_USER=minioadmin \
  -e MINIO_ROOT_PASSWORD=change-me-long-password \
  -v "$PWD/data:/data" \
  docker.io/pgsty/silo:latest server /data --console-address ":9001"
```

<p align="center">
  <img src="https://silo.pgsty.com/images/silo-console/console-login.webp" alt="Silo 控制台">
</p>

控制台位于 <http://localhost:9001>，S3 API 位于 <http://localhost:9000>。镜像内置客户端 `mcli`：

```bash
docker exec silo mcli alias set local http://127.0.0.1:9000 minioadmin change-me-long-password
docker exec silo mcli mb local/demo && docker exec silo mcli ls local
```

> [!WARNING]
> 生产环境应锁定版本，使用独立凭据与 TLS，配置监控，保留独立备份，并验证恢复流程。请从[文档](https://silo.pgsty.com/zh/docs/)开始。

## 安装

| 方式 | 位置 |
| :-- | :-- |
| 容器镜像 | [`pgsty/silo`](https://hub.docker.com/r/pgsty/silo)，支持 `linux/amd64` 与 `linux/arm64` |
| 二进制 | [GitHub Releases](https://github.com/pgsty/silo/releases)，覆盖 Linux、macOS、Windows 的 `amd64` 与 `arm64` |
| 软件包 | RPM、DEB、APK，也可通过 [Pigsty 软件仓库](https://pigsty.cc/docs/repo/) 安装 |
| Kubernetes | Helm Chart，参见[下载与安装](https://silo.pgsty.com/zh/download/) |
| 源码构建 | `go build -o silo . && ./silo --version` |

每个版本都附带校验和、SPDX SBOM、Sigstore 签名清单与 GitHub 构建证明。完整安装方式与验证命令见[下载与安装](https://silo.pgsty.com/zh/download/)；从上游 MinIO 迁移 —— 接管既有 `minio.service` 与 `/etc/default/minio`，并用 `/etc/systemd/system/silo.service.d/10-legacy-user.conf` drop-in 保持数据属主不变 —— 见[迁移指南](https://silo.pgsty.com/zh/compatibility/migration/)与[二进制与服务说明](https://silo.pgsty.com/zh/compatibility/binary/)。

## 兼容性

S3 API、`MINIO_*` 环境变量、`minio_*` 指标、`x-minio-*` 头、`/minio/*` 路由、`github.com/minio/*` 导入路径与磁盘格式（含 `.minio.sys`）原样保留，并由 CI 兼容性门禁冻结。只有 Silo 自有交付面改名：`silo` 可执行文件、软件包、服务、Helm Chart 与容器镜像 —— 原生交付物不会安装 `minio` 二进制别名。

与上游的全部分歧，以逐项核验代码的[兼容性审计](https://silo.pgsty.com/zh/compatibility/server/)形式维护。每个版本仍应视为下游升级：锁定版本，阅读[版本说明](https://silo.pgsty.com/zh/tags/silo/)，并保留回滚路径。

## 安全与贡献

请按照 [`SECURITY.md`](SECURITY.md) 私密报告漏洞；每项修复都会发布公开[安全公告](https://silo.pgsty.com/zh/blog/security/)。本项目不要求签署 CLA：贡献按 AGPL-3.0-or-later（inbound=outbound）接收，只需 DCO 签署（`git commit -s`），详见 [`CONTRIBUTING.md`](CONTRIBUTING.md)。

## 贡献者

<table>
  <tr>
    <td align="center" width="150">
      <a href="https://github.com/ZouhairCharef"><img src="https://github.com/ZouhairCharef.png?size=100" width="72" alt="ZouhairCharef"><br><sub><b>@ZouhairCharef</b></sub></a><br><sub>CVE-2026-34986</sub>
    </td>
    <td align="center" width="150">
      <a href="https://github.com/mfredenhagen"><img src="https://github.com/mfredenhagen.png?size=100" width="72" alt="mfredenhagen"><br><sub><b>@mfredenhagen</b></sub></a><br><sub>CVE-2026-39883</sub>
    </td>
    <td align="center" width="150">
      <a href="https://github.com/pinginfo"><img src="https://github.com/pinginfo.png?size=100" width="72" alt="pinginfo"><br><sub><b>@pinginfo</b></sub></a><br><sub>桶通知流式输出</sub>
    </td>
    <td align="center" width="150">
      <a href="https://github.com/waterkip"><img src="https://github.com/waterkip.png?size=100" width="72" alt="waterkip"><br><sub><b>@waterkip</b></sub></a><br><sub>文档链接修正</sub>
    </td>
  </tr>
</table>
<p>
<a href="https://github.com/magicxor"><img src="https://github.com/magicxor.png?size=64" width="44" alt="magicxor" title="@magicxor"></a>
<a href="https://github.com/ycjlin"><img src="https://github.com/ycjlin.png?size=64" width="44" alt="ycjlin" title="@ycjlin"></a>
<a href="https://github.com/h5vx"><img src="https://github.com/h5vx.png?size=64" width="44" alt="h5vx" title="@h5vx"></a>
<a href="https://github.com/Dansyuqri"><img src="https://github.com/Dansyuqri.png?size=64" width="44" alt="Dansyuqri" title="@Dansyuqri"></a>
<a href="https://github.com/davinkevin"><img src="https://github.com/davinkevin.png?size=64" width="44" alt="davinkevin" title="@davinkevin"></a>
<a href="https://github.com/lem21h"><img src="https://github.com/lem21h.png?size=64" width="44" alt="lem21h" title="@lem21h"></a>
<a href="https://github.com/sulin37392"><img src="https://github.com/sulin37392.png?size=64" width="44" alt="sulin37392" title="@sulin37392"></a>
<a href="https://github.com/mosesdd"><img src="https://github.com/mosesdd.png?size=64" width="44" alt="mosesdd" title="@mosesdd"></a>
<a href="https://github.com/Xavier-777"><img src="https://github.com/Xavier-777.png?size=64" width="44" alt="Xavier-777" title="@Xavier-777"></a>
<a href="https://github.com/jiadzh"><img src="https://github.com/jiadzh.png?size=64" width="44" alt="jiadzh" title="@jiadzh"></a>
<a href="https://github.com/TLINDEN"><img src="https://github.com/TLINDEN.png?size=64" width="44" alt="TLINDEN" title="@TLINDEN"></a>
<a href="https://github.com/AntonOfTheWoods"><img src="https://github.com/AntonOfTheWoods.png?size=64" width="44" alt="AntonOfTheWoods" title="@AntonOfTheWoods"></a>
<a href="https://github.com/zylpsrs"><img src="https://github.com/zylpsrs.png?size=64" width="44" alt="zylpsrs" title="@zylpsrs"></a>
<a href="https://github.com/nsanitate"><img src="https://github.com/nsanitate.png?size=64" width="44" alt="nsanitate" title="@nsanitate"></a>
<a href="https://github.com/makinikm"><img src="https://github.com/makinikm.png?size=64" width="44" alt="makinikm" title="@makinikm"></a>
<a href="https://github.com/spaceg00se-r"><img src="https://github.com/spaceg00se-r.png?size=64" width="44" alt="spaceg00se-r" title="@spaceg00se-r"></a>
<a href="https://github.com/heroes1412"><img src="https://github.com/heroes1412.png?size=64" width="44" alt="heroes1412" title="@heroes1412"></a>
<a href="https://github.com/vampywiz17"><img src="https://github.com/vampywiz17.png?size=64" width="44" alt="vampywiz17" title="@vampywiz17"></a>
<a href="https://github.com/chalukyaj"><img src="https://github.com/chalukyaj.png?size=64" width="44" alt="chalukyaj" title="@chalukyaj"></a>
<a href="https://github.com/cbornet"><img src="https://github.com/cbornet.png?size=64" width="44" alt="cbornet" title="@cbornet"></a>
<a href="https://github.com/jvasile"><img src="https://github.com/jvasile.png?size=64" width="44" alt="jvasile" title="@jvasile"></a>
<a href="https://github.com/Kesavaambati"><img src="https://github.com/Kesavaambati.png?size=64" width="44" alt="Kesavaambati" title="@Kesavaambati"></a>
<a href="https://github.com/redfoxfox"><img src="https://github.com/redfoxfox.png?size=64" width="44" alt="redfoxfox" title="@redfoxfox"></a>
<a href="https://github.com/kuldeep-link11"><img src="https://github.com/kuldeep-link11.png?size=64" width="44" alt="kuldeep-link11" title="@kuldeep-link11"></a>
<a href="https://github.com/meesudzu"><img src="https://github.com/meesudzu.png?size=64" width="44" alt="meesudzu" title="@meesudzu"></a>
<a href="https://github.com/pmezhuev"><img src="https://github.com/pmezhuev.png?size=64" width="44" alt="pmezhuev" title="@pmezhuev"></a>
<a href="https://github.com/kh0mka"><img src="https://github.com/kh0mka.png?size=64" width="44" alt="kh0mka" title="@kh0mka"></a>
</p>

GitHub 不为 fork 仓库生成贡献者图表，因此 [`CONTRIBUTORS.md`](CONTRIBUTORS.md)（而非 Insights 页面）才是本项目的署名记录，其中逐一记录了每个人对应的改动或报告。

## 背景

本项目因上游收缩社区版而生：Web 控制台被削减为残桩、社区预编译制品停发、社区仓库被归档。Silo 的存在就是让这些部署继续跑下去。Fork 是手段，不是身份 —— 若上游恢复社区版承诺，我们乐意收缩范围，并把修复回馈上游。

[**宣言**](https://silo.pgsty.com/zh/about/manifesto/)是项目的公开承诺，共十一条，通篇遵循一项纪律：**每一条，要么是已经在做且有公开证据的事实，要么是刻意拒绝的承诺。** 摘要：

- **兼容性合同** —— 协议与数据不改，每个版本都标注经过测试的回滚目标与路径。
- **许可证无法变更** —— AGPLv3、无 CLA、不做版权聚合；包括我们自己在内，没有人握有足够版权代表所有贡献者重新授权。
- **永不清单**（只增不减）—— 永不将既有功能移入付费墙、永不给下载设注册墙、永不加入遥测（上游回连路径已整体移除）、永不引入 CLA、永不变更许可证、永不以商标追究正常使用。
- **安全与发布纪律** —— 每项安全修复配一篇公开公告；通常每一到两个月发布一版，最长不超过一个季度。请拿公开记录检验这两条。

延伸阅读：[MinIO已死](https://silo.pgsty.com/zh/blog/post/minio-is-dead/) · [谁能接盘？](https://silo.pgsty.com/zh/blog/post/minio-alternative/) · [MinIO 复生](https://silo.pgsty.com/zh/blog/post/minio-resurrect/) · [承诺兑现](https://silo.pgsty.com/zh/blog/post/minio-promise-kept/)

## 许可证与商标

Silo 采用 [AGPL-3.0-or-later](LICENSE)，衍生自 [`minio/minio`](https://github.com/minio/minio)，上游版权与第三方声明完整保留于 [`NOTICE`](NOTICE) 与 [`CREDITS`](CREDITS)。MinIO 是 MinIO, Inc. 的商标，此处使用仅为标识上游项目与兼容谱系。

详见：[许可证](https://silo.pgsty.com/zh/about/license/) · [署名归属](https://silo.pgsty.com/zh/about/attribution/) · [商标声明](https://silo.pgsty.com/zh/about/trademark/)
