<p align="center">
  <img src="assets/banner.svg" alt="pawl — 防劣化质量门禁" width="820">
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

**pawl 是一道防止代码质量倒退的门禁。** 它先记录仓库当前的质量数据，之后用同样的方法重新测量；任何指标变差，门禁就失败。

固定阈值往往要求老项目先还清技术债，门禁才能启用。pawl 不需要。第一次测量就是基线：存量问题可以暂时保留，但新的改动不能让它们继续增加；某项指标一旦改善，新基线就会把成果锁住。

```bash
pawl record                     # 记录当前基线
pawl check                      # 有指标退化时退出 1
pawl record --only line-coverage # 只锁定一项改进
pawl guard origin/main          # 检查基线有没有被调低
```

一个**维度（dimension）**就是一项可量化的数据：覆盖率、通过的测试数、 lint 问题、超长文件、产物体积、循环依赖，或者项目自己的指标。pawl 既提供常用适配器，也接受自定义命令，因此不限定语言，也不要求替换现有工具链。

## 快速上手

通过 npm、Go 或安装脚本获取静态二进制：

```bash
npm install -D @pawl-tools/cli
# 或：go install github.com/tiangong-dev/pawl/cmd/pawl@latest
# 或：curl -fsSL https://raw.githubusercontent.com/tiangong-dev/pawl/main/install.sh | sh
```

生成配置并记录第一份基线：

```bash
pawl init
pawl record
git add pawl.yaml pawl.snapshot.json
git commit -m "chore: 接入 pawl 质量门禁"
```

以后在本地和 CI 中运行 `pawl check`。发生退化时，pawl 会明确指出哪项指标发生了变化：

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

## 为什么用棘轮，而不是固定阈值

棘轮只能往一个方向转动，pawl 也是一样：质量只能变好，基线不会被悄悄调差。

固定阈值只有项目已经达标时才好用。覆盖率只有 62%、还留着几个 700 行文件、又赶着上线的老仓库用不上它——阈值定在理想状态，所有 PR 都会失败；定在现状，规则就形同虚设。

pawl 从项目的真实现状出发：第一份快照如实记录现有技术债，不需要提前整改；但从这一刻起新增的退化会立即失败，覆盖率下降或新增一个超长文件都会让 `check` 退出 1。改进也会被留住——某个维度改善后重新记录，后续改动就不能让它退回原值。按文件、按 key 比较时，A 文件的修复也不能抵消 B 文件新增的问题。

基线本身只是提交到 Git 的一个 JSON 文件。不需要账号，不需要服务端，也不收集遥测；`pawl trend` 直接从已有的 Git 历史读取指标走势。

## pawl 如何测量仓库

`pawl.yaml` 中的每个维度都有 id、好坏方向和一个测量来源：

- `file-length`、`pattern-count` 等零依赖原语；
- ESLint、Oxlint、SARIF、JUnit、lcov、cobertura 等工具或报告适配器；
- 输出数字或测量对象的自定义命令。

测量工具与最终裁决彼此分离。即使团队替换了 linter，也只需修改该维度的适配方式，不必重做基线流程和 CI 门禁。

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

[配置示例](./RECIPES.md)收录了 Go、TypeScript、Python、Rust、Swift、常见 lint 工具、报告格式和自定义命令，可以直接复制后调整路径。

### Gate 模式

总数始终会参与比较，`gate` 可以在此基础上增加更精确的检查：

| gate | 适用指标 | 行为 |
|---|---|---|
| `total` | 覆盖率、产物体积、问题总数 | 比较单个数值 |
| `per-file-count` | lint 问题、抑制标记、TODO | 一个文件的修复不能抵消另一个文件的新问题 |
| `per-key-value` | 分包覆盖率、各产物体积 | 分别保护基线中已有的每个 key |

技术债数量和体积通常使用 `lower-is-better`；覆盖率、通过测试数等地板指标使用 `higher-is-better`。对于存在小幅波动的指标，可用 `tolerance` 设置向变差方向的绝对容差。

### 内置集成

| builtin | 测量来源 | 常用 gate |
|---|---|---|
| `file-length`、`file-bytes` | 仓库文件 | `total` + `per-key-value` |
| `pattern-count` | 正则匹配 | `per-file-count` |
| `eslint`、`oxlint` | linter 原生输出 | `per-file-count` |
| `jscpd`、`swift-complexity` | 工具专用 JSON | `total` / `per-file-count` |
| `json-value` | JSON 中的一个数值 | `total` / `per-key-value` |
| `lines` | 按行输出的分析结果 | `per-file-count` |
| `sarif` | SARIF findings | `per-file-count` |
| `junit` | 通过、失败、跳过或全部测试数 | `total` |
| `coverage` | lcov 或 cobertura 覆盖率 | `total` |

命名 analyzer 可以让多个维度共用一次扫描。所有选项和输出语义以 [引擎契约](./spec/README.md)为准。

### 自定义命令与 `extract`

遇到 pawl 不认识的工具，可以定义命令维度。完整适配器接受一个 JSON 对象：

```json
{ "value": 42, "unit": "findings", "breakdown": { "src/a.ts:17": 2 } }
```

如果命令本来就会打印数字或逐行输出问题，`extract` 可以省掉包装脚本：

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

测量失败不等于结果为零。命令崩溃、报告损坏、超时或抽取失败时，pawl 会退出 2，不会把“没测出来”伪装成“测得零”。对于用非零退出码表示“发现问题”的工具，应声明 `valid_exit_codes`，而不是用 `|| true` 吞掉所有错误。

## 命令

| 命令 | 用途 |
|---|---|
| `pawl init` | 写入起步 `pawl.yaml`，已有文件不会被覆盖 |
| `pawl record` | 测量并写入快照 |
| `pawl check` | 对比当前测量与基线；也是默认命令 |
| `pawl measure` | 只输出当前测量，不读基线、不下裁决 |
| `pawl guard <ref>` | 与 `<ref>` 中的快照比较，防止基线被调低 |
| `pawl trend [<id>]` | 查看已提交快照的历史 |
| `pawl rank` | 按行数或字节数排列纳入检查的文件 |
| `pawl agent` | 安装或打印供编码 Agent 使用的操作说明 |
| `pawl version` | 输出当前版本 |

三个退出码的区别很重要：

| 退出码 | 含义 |
|---|---|
| `0` | 测量完成，门禁通过 |
| `1` | 测量完成，但有指标退化 |
| `2` | pawl 无法给出可信裁决 |

完整参数见 `pawl help [command]`。需要稳定的机器可读结果时，使用 `--format json`。

## 日常工作流

### 只锁定一项改进

```bash
pawl record --only line-coverage
```

只有指定维度会重新测量和更新，其余值从已有快照原样复制。这样既不会被无关适配器的故障挡住，也不会在记录一项改进时顺手放过另一项退化。

### 只检查改动行

历史债较多的仓库可以运行：

```bash
pawl check --since origin/main
```

能定位到行的 `per-file-count` 问题只检查改动行；覆盖率总数等无法可靠归属到某一行的指标仍会全量执行，并在结果中明确标注。未提交和未跟踪的改动也会纳入检查。

### 明确接受技术债

`pawl record` 默认拒绝写入更差的值。确实需要接受一次退化时，先预览，再显式记录：

```bash
pawl record --dry-run --accept-worse
pawl record --accept-worse
```

pawl 会输出一条 `Pawl-Accept: <id> <value>` 提交 trailer。`pawl guard` 借此区分评审过的技术债和未经授权的基线下调。

### 复用同一次测量

如果维度读取构建产物，最好只测一次，确保 `check` 和后续 `record` 看到的是同一份结果：

```bash
pawl measure > .pawl/current.json
pawl check --current .pawl/current.json
pawl record --only line-coverage --current .pawl/current.json
```

### 查看走势

```bash
pawl trend
pawl trend line-coverage --limit 50
```

历史直接来自 Git 中的 `pawl.snapshot.json`，不需要额外数据库。

## CI 集成

任何 CI 都可以安装二进制并运行 `pawl check`。在 PR 上应额外运行 `pawl guard`，避免有人通过修改快照悄悄降低门槛。

### GitHub Actions

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

      # 先运行会生成报告的测试或分析器。
      - run: npm test -- --coverage

      - uses: tiangong-dev/pawl@v0
        with:
          command: check
          args: --since origin/${{ github.base_ref || 'main' }}

      - if: github.event_name == 'pull_request'
        run: pawl guard origin/${{ github.base_ref }}
```

传入 `command: check` 后，Action 会执行门禁，并可根据 JSON 裁决维护一条 PR 评论；不需要评论时设置 `comment: 'false'`。如果不传 `command`，Action 只负责把 pawl 安装到 `PATH`。

### 其他 CI

可以下载 release 二进制、使用 npm 包，或直接运行：

```bash
npx -y @pawl-tools/cli@0.8.0 check
```

整个过程不依赖服务端组件。

## 可选：给编码 Agent 安装操作说明

无论代码来自开发者、生成器还是 Agent，pawl 的门禁逻辑都完全相同。如果仓库中会使用编码 Agent，`pawl agent` 可以把操作说明写入 `AGENTS.md` 或 `CLAUDE.md`，提醒它运行门禁、读取 JSON 裁决，并只更新真正改善的维度：

```bash
pawl agent --write agent      # 写入 AGENTS.md
pawl agent --write claude     # 写入 CLAUDE.md
```

这只是一个可选集成，不是 pawl 的另一种工作模式；最终裁决仍由 CI 负责。该命令的评测与 fixture 在 [demo/](./demo/README.md) 中。

## 能力边界

pawl 负责的是**组织测量、比较基线、给出裁决**。它不是新的 linter、托管面板、包管理器或自动修复器。项目仍然自行安装和配置分析工具；pawl 把这些不同来源的数字纳入同一套基线和同一道可执行门禁。

- [配置示例](./RECIPES.md)：可复制调整的常见维度
- [引擎契约](./spec/README.md)：精确行为和文件格式
- [参与贡献](./CONTRIBUTING.md)：开发与测试流程
- [变更记录](./CHANGELOG.md)：版本升级说明

## 许可证

MIT，见 [LICENSE](./LICENSE)。
