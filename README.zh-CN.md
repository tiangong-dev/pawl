<p align="center">
  <img src="assets/banner.svg" alt="pawl — 只进不退的质量门禁" width="820">
</p>

<p align="center">
  <a href="./README.md">English</a> · <a href="./SPEC.md">行为契约</a> · <a href="./RECIPES.md">配置示例</a>
</p>

<p align="center">
  <a href="https://github.com/tiangong-dev/pawl/actions/workflows/ci.yml"><img src="https://img.shields.io/github/actions/workflow/status/tiangong-dev/pawl/ci.yml?branch=main&amp;label=CI&amp;logo=github" alt="CI"></a>
  <a href="https://scorecard.dev/viewer/?uri=github.com/tiangong-dev/pawl"><img src="https://api.scorecard.dev/projects/github.com/tiangong-dev/pawl/badge?v=1" alt="OpenSSF Scorecard"></a>
  <a href="https://www.npmjs.com/package/@pawl-tools/cli"><img src="https://img.shields.io/npm/v/@pawl-tools/cli?logo=npm&amp;color=cb3837" alt="npm"></a>
  <a href="./go.mod"><img src="https://img.shields.io/github/go-mod/go-version/tiangong-dev/pawl?logo=go" alt="Go version"></a>
  <a href="https://github.com/marketplace/actions/setup-pawl"><img src="https://img.shields.io/badge/GitHub%20Marketplace-setup--pawl-2ea44f?logo=github" alt="GitHub Marketplace: setup-pawl"></a>
  <a href="./LICENSE"><img src="https://img.shields.io/github/license/tiangong-dev/pawl?color=blue" alt="MIT license"></a>
</p>

Agent 写代码很快。它也特别会把仓库弄得稍微差一点，而且 review 一时半会儿看不出来：覆盖率掉一个点、多了个 800 行的文件、以前禁掉的 lint 又回来了。PR 看起来没问题。底线已经动了。

pawl 是一个语言无关的 CI 质量门禁，核心机制像棘轮一样只进不退：把仓库当前产出的数字——覆盖率、lint 问题数、失败测试数、超长文件数、构建体积——记成一份提交进 Git 的基线快照。之后任何让某个数字变差的改动都会让 `pawl check` 退出 1。**旧债可以先留着。新债不行。** 没有服务端，不用注册账号，也不上传任何数据。

```bash
pawl record                      # 记录当前基线
pawl check                       # 对比基线；有指标退化时退出 1（默认命令）
pawl record --only line-coverage # 只锁定一项改进，其余保持原样
pawl guard origin/main           # 检查基线有没有被调低
```

只要能产出具体数值，任何指标都可以定义为一个**维度（dimension）**：测试覆盖率、通过测试数、lint 问题数、超长文件数、打包体积、循环依赖，或是项目的自定义指标。pawl 内置常见工具与报告适配器，也支持执行任意命令，不绑定技术栈，更不需要推翻现有的工具链。

## 快速上手

### 1. 安装二进制

通过 npm、Go 或官方脚本获取免依赖的静态二进制：

```bash
npm install -D @pawl-tools/cli
# 或：go install github.com/tiangong-dev/pawl/cmd/pawl@latest
# 或：curl -fsSL https://raw.githubusercontent.com/tiangong-dev/pawl/main/install.sh | sh
```

pawl 的 GitHub Release 采用 [cosign](https://github.com/sigstore/cosign) 对发布的每个压缩包进行无密钥（keyless）签名。若系统已安装 cosign，`install.sh` 会自动执行校验；若从 [Releases 页面](https://github.com/tiangong-dev/pawl/releases) 手动下载归档包，可配合旁边的 `.sigstore.json` 文件进行校验（未引入签名机制的早期版本除外）：

```bash
cosign verify-blob --bundle <archive>.sigstore.json \
  --certificate-identity-regexp 'https://github.com/tiangong-dev/pawl/\.github/workflows/release\.yml@.*' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  <archive>
# <archive> 为下载的压缩包，如 pawl-<version>-linux-x64.tar.gz 或 pawl-<version>-win32-x64.zip
```

### 2. 生成配置并固化首份基线

```bash
pawl init
pawl record
git add pawl.yaml pawl.snapshot.json
git commit -m "chore: 接入 pawl 质量门禁"
```

### 3. 本地与 CI 门禁核验

日常在本地和 CI 中直接运行 `pawl check`。一旦出现退化，门禁立即失败并清晰列出恶化的具体维度：

```console
$ pawl check

metric         baseline    current       Δ  status
------------------------------------------------
file-length           3          4      +1  ❌ worse
panics                1          1      ±0  ✅ same
todo-markers         12         12      ±0  ✅ same

❌ regressions:
  • file-length (超过 500 行的文件)
      total 3 → 4
```

日常流程就这几步：测量、比较、修复退化、记录真正的改进。

## 为什么不直接把覆盖率卡在 80%？

因为仓库现在不是 80%。是 62%，还搁着三个 700 行的文件，而且这周就要上线。阈值定在理想值，每个 PR 全红，最后大家只会被迫选择跳过门禁；定在现状，又毫无约束力。

pawl 从仓库今天的现状起步。第一份快照就是底线。之后谁改差了，CI 就亮红灯。什么时候真正修好了一项，就单把那一项重新记入快照，锁死新的底线。在单文件（`per-file-count`）或分 key 比对时，修好 A 文件绝不能拿来抵消 B 文件新捅的娄子。

基线不过是保存在 Git 里的普通 JSON 文件，`pawl trend` 翻一翻你已有的 commit 历史就能直接绘出质量走势。

## 和别的做法比

“不让指标变差”并不是新想法。各家方案的本质差异，在于**门禁裁决在哪里下达**，以及**拿什么基准做比对**：

| 工具 | 裁决在哪里下达 | 基线保存在哪里 |
|---|---|---|
| **pawl** | **本地直接判定**（即 CLI 退出码） | 提交至仓库的 `pawl.snapshot.json` |
| SonarQube Server / Cloud | 服务端（免费自托管版亦然） | 远端服务器数据库 |
| Codecov / Coveralls | 服务端（上传报告后异步分析） | SaaS 平台 |
| betterer | 本地 | 随代码提交的 `.betterer.results` |
| git-ratchet | 本地 | `git-notes` |

Qlty（原 Code Climate Quality）横跨两端：本地 CLI 仅根据问题严重级别（`--fail-level`）与 upstream 对比判负，历史记录与 PR 门禁则托管在 Qlty Cloud。

各方案能守住哪些指标，往往受制于商业产品线与套餐：Codecov 在覆盖率之外补充了包体积与测试分析；Qlty Cloud 能在总覆盖率下跌时阻断 PR；SonarQube 也支持针对整仓配置自定义质量门。

但 pawl 的追求不是拼适配维度的清单有多长，而是**自身不内置任何特定分析器**——只要外部命令能吐出数字或键值对，pawl 就能提取并死死守住它。git-ratchet 同样通过 stdin 读取 `measure,value`，但它是在数值增长超过 slack 容差时判负，遇到覆盖率等“越大越好”的正向指标时，必须先在外部脚本取反转换才能适配。

有两样东西容易混淆，值得分清楚：

**固定阈值不是棘轮。** SonarQube 默认质量门是用固定条件卡“新代码”（支持配置为上一版本、近 N 天或特定分支 diff 等滑动窗口），也可配置整仓静态条件。但**阈值是人为划定的一道红线**（例如“覆盖率不得低于 80%”）。在历史包袱沉重的代码库中，红线定高了天天报错，团队麻木后只会选择绕过；定低了形同虚设；更难受的是团队往往要先为“到底多少算及格”无休止地开会扯皮。棘轮没有这条人为红线：**现状就是底线**。只要有人做出了改进并执行 `pawl record`，门槛就自动提升至新高度，只进不退。不需要先就“终点定在哪”达成一致，代码库就能在日常开发中持续向好。

**diff 过滤没有记忆。** `golangci-lint --new-from-rev` 或 `reviewdog -filter-mode=added` 只把问题收窄到本次 PR 触碰的代码行。这种做法开销小、很实用，但它**对仓库的全局演进毫无记忆**。许多严重退化**根本没有直接修改产生后果的代码行**：引入一个新依赖导致打包体积暴增、改动了底层配置导致某个核心测试套件被悄然 skip、或是修改公共类型后让下游未动过的文件泛滥出类型逃逸（如 `as any`）。只要没人触碰对应行，diff 过滤就对此一无所知。棘轮守卫的是代码库各维度的真实全局状态，不留劣化死角。

## pawl 如何测量仓库

`pawl.yaml` 中配置的每个**维度（dimension）**都包含唯一标识 `id`、评判方向以及测量数据来源：

- `file-length`、`pattern-count`、`file-bytes` 等零依赖原生原语；
- ESLint、Oxlint、SARIF、JUnit、lcov、cobertura 等工具与报告适配器；
- 输出纯数字或结构化对象的自定义命令。

**测量工具与门禁裁决完全解耦。** 即便团队从 ESLint 迁移到 Oxlint，也只需调整该维度的适配配置，原有的基线快照历史与 CI 门禁规则无需重做。

### 一份小而实用的配置

```yaml
snapshot: "pawl.snapshot.json"

dimensions:
  - id: "file-length"
    title: "超过 500 行的文件"
    direction: "lower-is-better"
    builtin: "file-length"
    options:
      threshold: 500
      include: ["src/**/*.ts", "src/**/*.go"]

  - id: "type-escapes"
    title: "TypeScript 类型逃生舱"
    direction: "lower-is-better"
    gate: "per-file-count"
    builtin: "pattern-count"
    options:
      pattern: 'as\s+any|@ts-(ignore|nocheck)'
      include: ["src/**/*.ts", "src/**/*.tsx"]

  - id: "line-coverage"
    title: "行覆盖率 %"
    direction: "higher-is-better"
    tolerance: 0.5
    builtin: "coverage"
    options:
      file: "coverage/lcov.info"
      format: "lcov"
```

[配置示例（RECIPES.md）](./RECIPES.md) 收录了 Go、TypeScript、Python、Rust、Swift、常用 linter、各类报告格式与自定义命令的配置模板，开箱即用。

### Gate 模式

总数（scalar total）默认始终参与比对；`gate` 字段可在此基础上启用更精确的约束策略：

| Gate 模式 | 适用场景 | 门禁行为 |
|---|---|---|
| `total`（默认） | 覆盖率、打包体积、问题总数 | 仅比对整体单一数值 |
| `per-file-count` | lint 问题、类型抑制、TODO 标记 | 精确到单文件计数：修好文件 A 绝不能抵消文件 B 新增的问题 |
| `per-key-value` | 分包/模块覆盖率、各 chunk 体积 | 分别保护基线中已存在的每一个 key，防止局部恶化 |

**指标好坏方向：**
- `lower-is-better`：用于技术债计数、缺陷标记与产物体积（越小越好）；
- `higher-is-better`：用于测试覆盖率、通过测试数等保底指标（越高越好）。

若某项指标存在正常的轻微波动（如每次浮动 0.1% 的覆盖率），可通过 `tolerance` 设置允许向变差方向浮动的绝对容差（slack）。

### 内置适配器

| 内置适配器 | 数据来源 | 常用 Gate |
|---|---|---|
| `file-length`、`file-bytes` | 仓库源码文件 | `total` + `per-key-value` |
| `pattern-count` | 正则文本匹配 | `per-file-count` |
| `eslint`、`oxlint` | linter 原生 JSON 输出 | `per-file-count` |
| `jscpd`、`swift-complexity` | 专用工具导出的 JSON | `total` / `per-file-count` |
| `json-value` | 任意 JSON 中的某个数值字段 | `total` / `per-key-value` |
| `lines` | 行导向的分析器输出 | `per-file-count` |
| `sarif` | SARIF 静态分析结果 | `per-file-count` |
| `junit` | 测试用例数（通过、失败、跳过或总计） | `total` |
| `coverage` | lcov 或 cobertura 覆盖率报告 | `total` |

多个维度可声明共用同一个命名 `analyzer`，避免重复执行开销巨大的扫描。各项适配器的完整配置项与输出规范详见 [引擎契约（spec）](./spec/README.md)。

### 自定义命令与声明式 `extract`

遇到 pawl 未原生集成的工具，声明自定义命令维度即可。完整适配协议接受一个标准 JSON 对象：

```json
{ "value": 42, "unit": "findings", "breakdown": { "src/a.ts:17": 2 } }
```

若命令本身就能输出纯数字或逐行问题列表，借助声明式的 `extract` 层即可免除编写包装脚本：

```yaml
- id: "circular-deps"
  title: "循环依赖"
  direction: "lower-is-better"
  command: "npx madge --circular --json src | jq 'length'"
  extract: number

- id: "todos"
  title: "TODO 标记"
  direction: "lower-is-better"
  gate: "per-file-count"
  command: "grep -rn TODO src"
  valid_exit_codes: [0, 1]
  extract:
    regex: '^(?P<path>[^:]+):(?P<line>\d+):'
```

**坚决不隐瞒测量失败**：命令崩溃、报告损坏、超时或正则提取失败时，pawl 会返回退出码 `2`，**绝不把“没测出来”伪装成“零问题”悄悄放行**。对于用非零退出码表示“发现问题”的工具（如 `grep` 无匹配返回 1，linter 发现违规返回 1），必须显式配置 `valid_exit_codes`，**切忌使用 `|| true` 暴力吞掉异常**，否则真正的程序崩溃也会被误判为“零缺陷通过”。

读取磁盘外部报告的维度可以设置 `artifact_max_age: "24h"`：当报告文件超过指定有效期时直接退出码 2 阻断，杜绝误读几天前残留的过期报告；由当前命令实时生成的报告天然视为新鲜。若未显式配置，pawl 仅将文件时间戳作为溯源信息（provenance）输出，不做强制中断。

## CLI 命令

| 命令 | 用途 |
|---|---|
| `pawl init` | 初始化起步配置文件 `pawl.yaml`（不会覆盖已有文件） |
| `pawl record` | 重新测量各项指标并固化写入快照基线 |
| `pawl check` | 对比当前测量值与基线快照；CLI 默认执行该命令 |
| `pawl measure` | 仅输出当前测量结果，不读取基线、不进行裁决 |
| `pawl guard <ref>` | 比对当前快照与 `<ref>` 处的历史快照，防止基线被意外或恶意降低 |
| `pawl trend [<id>]` | 遍历 Git 历史中的快照提交，查看指标演进趋势 |
| `pawl rank` | 按行数或字节体积对受测文件进行降序排列 |
| `pawl agent` | 生成或打印供 AI 编码 Agent 遵守的操作规约 |
| `pawl version` | 输出当前 pawl 版本号 |

### 退出码设计

pawl 严格通过退出码向 CI 与上层编排系统传递仲裁结果：

| 退出码 | 状态 | 判定依据 |
|:---:|---|---|
| **`0`** | **Pass** | 测量正常完成，所有受测指标均未出现退化 |
| **`1`** | **Regression** | 测量正常完成，但至少有一个指标劣于基线（质量阻断） |
| **`2`** | **Error** | 测量异常，无法给出可信裁决（如命令崩溃、超时、报告缺失或解析失败），**拒绝静默放行** |

完整参数选项见 `pawl help [command]`。自动化流水线建议指定 `--format json` 获取稳定的结构化裁决。

## 日常工作流

### 只锁定单项改进

```bash
pawl record --only line-coverage
```

只有指定的维度会重新测量并固化，快照中其余维度的数值保持原样。这样既不会因为其他未就绪工具的报错而阻碍记录，更不会在更新快照时不慎将其他意外退化的指标打包放行。

### 只检查改动行（Diff 范围收窄）

对于存量历史债较多的代码库：

```bash
pawl check --since origin/main
```

对于支持行级定位的 `per-file-count` 维度，门禁仅比对改动行引入的问题；而覆盖率总数等无法可靠归属到具体某行的全局指标，依然会执行全量比对并在输出中明确注明。工作区内未暂存和未跟踪的改动也会一并纳入检查。

### 显式接受技术债（特批劣化）

`pawl record` 默认拒绝将变差的数值写入快照。若因业务紧急确需接受一次退化，先预览影响，再显式确认：

```bash
pawl record --dry-run --accept-worse
pawl record --accept-worse
```

执行后，pawl 会在终端输出类似 `Pawl-Accept: <id> <value>` 的 Git commit trailer。在后续的代码评审中，`pawl guard` 会校验这一声明，借此区分“经过 PR 评审特批的技术债”与“未授权的私自篡改”。

### 复用单次测量结果

如果多个维度需要读取同一批耗时较长的构建产物或测试报告，建议先测一次并保存中间结果，确保 `check` 裁决与后续的 `record` 消费完全同一批输入：

```bash
pawl measure > .pawl/current.json
pawl check --current .pawl/current.json
pawl record --only line-coverage --current .pawl/current.json
```

### 查看历史走势

```bash
pawl trend
pawl trend line-coverage --limit 50
```

无需配置额外的时序数据库或监控面板，历史数据直接解析自 Git 提交树里的 `pawl.snapshot.json`。

## CI 集成

任何 CI 都可以直接下载二进制并运行 `pawl check`。在 PR 流水线中，建议同时运行 `pawl guard`，防止基线快照被意外降低。

### GitHub Actions

官方 Action 开箱即用，支持门禁判定、基线守卫与 PR 评论同步：

```yaml
permissions:
  contents: read
  pull-requests: write

jobs:
  quality:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0

      # 提前运行生成覆盖率或测试报告的命令
      - run: npm test -- --coverage

      - uses: tiangong-dev/pawl@v0.8.2
        with:
          command: check
          args: --since origin/${{ github.base_ref || 'main' }}
          guard-ref: origin/${{ github.base_ref || 'main' }}
```

**参数配置说明：**
- `command: check`：执行门禁检查；Action 会根据 JSON 裁决在 PR 下自动维护一条汇总评论，多次运行就地更新，不刷屏。
- `guard-ref`：传入目标基准分支（需提前 fetch），让同一个 Action 同时校验基线快照未被调低；guard 判定会先于 PR 评论执行。
- `args`：透传给 CLI 的参数。若其中包含 `-c/--config`，guard 会自动复用同一份配置。
- 若无需 PR 评论通知，可设置 `comment: 'false'`，并直接从 job 的 `permissions` 中移除 `pull-requests: write`（整个 Action 仅发表评论需要该权限）。
- 若不传入 `command`，该 Action 仅负责将 pawl 二进制安装到系统的 `PATH`。

### GitLab 等系统集成（利用 MR Code Quality 挂件）

`pawl check --format json` 遵循稳定的数据契约。借助官方转换脚本 [scripts/gitlab-codequality.mjs](scripts/gitlab-codequality.mjs)，可将裁决结果无缝转换为 [GitLab Code Quality](https://docs.gitlab.com/ci/testing/code_quality/) 报告，直接在 Merge Request 界面中展示问题明细：

```yaml
quality:
  script:
    - curl -fsSL -o gitlab-codequality.mjs https://raw.githubusercontent.com/tiangong-dev/pawl/main/scripts/gitlab-codequality.mjs
    - pawl check --format json > pawl.json || rc=$?
    - node gitlab-codequality.mjs pawl.json > gl-code-quality.json
    - exit ${rc:-0}
  artifacts:
    when: always
    reports:
      codequality: gl-code-quality.json
```

**关键工程细节：**
- **必须先暂存退出码**：使用 `|| rc=$?` 捕获 `pawl check` 的退出码，待生成报告后再统一 `exit`。若使用 `&&` 串联，一旦发生退化（退出码 1）或测量失败（退出码 2）就会立即短路终止，导致 MR 挂件因拿不到报告而无法展示缺陷详情。
- **故障零容忍**：当 pawl 遇到异常退出 2 时，脚本会生成一条 blocker 级别的 issue，**绝不允许门禁本身的故障在挂件上被误读为“代码完全通过”**。
- **路径基准映射**：pawl 产出的相对路径基于配置文件所在目录。若使用 `-c config/pawl.yaml`，需向脚本传入 `--config-dir=config`，GitLab 才能将 issue 精确挂载到源码行。`--anchor`（针对全仓指标或无法定位到行的兜底标识）默认取配置目录下的 `pawl.yaml`，配合 `--config-dir` 自动解析，无需重复拼接路径；仅当配置文件改名时才需手动指定（例如 `config/quality.yaml` 传入 `--anchor=quality.yaml`）。
- **生产版本锁定**：示例中通过 `main` 获取脚本；在正式生产环境中，建议将脚本固定为特定 release tag 或直接作为静态文件 vendor 到代码库中。

### 其他 CI 系统

Jenkins、CircleCI、Buildkite、Azure Pipelines、Woodpecker 等系统，只要能执行二进制即可直接使用，无需安装专属插件：

```bash
npx -y @pawl-tools/cli@0.8.2 check
```

整个过程不依赖服务端组件。

## Agent

仓库里要是有 Agent 在写代码，先让它看见这道门：

```bash
pawl agent --write agent      # 写入 AGENTS.md
pawl agent --write claude     # 写入 CLAUDE.md
```

CI 仍然说了算。`pawl agent` 只是写下操作说明：跑 `pawl check`、读 JSON 结论、修好了某项只记那一项（`pawl record --only <id>`），别整份快照重写。评测和夹具在 [demo/](./demo/README.md)。

## 能力边界

pawl 负责的是**组织测量、比较基线、给出裁决**。

它不是新的 linter，也不是托管看板、包管理器或自动修复器。项目仍然自行安装和配置分析工具；pawl 只是把这些不同来源的数字纳入同一套基线和同一道可执行门禁。

- [配置示例](./RECIPES.md)：可复制调整的常见维度模板
- [引擎契约](./spec/README.md)：确切的行为规范与文件格式
- [参与贡献](./CONTRIBUTING.md)：开发与测试流程
- [变更记录](./CHANGELOG.md)：版本升级说明

## 许可证

MIT，见 [LICENSE](./LICENSE)。
