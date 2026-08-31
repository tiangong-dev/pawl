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

Agent 写代码很快。它也特别会把仓库弄得稍微差一点，而且 review 一时半会儿看不出来：覆盖率掉一个点、多了个 800 行的文件、以前禁掉的 lint 又回来了。PR 看起来没问题。底线已经动了。

pawl 是一个语言无关的 CI 质量门禁，用的是棘轮的办法：把仓库当前产出的数字——覆盖率、lint 问题数、失败的测试数、超长文件数、产物体积——记成一份提交进 Git 的基线，之后任何让某个数字变差的改动都会让 `pawl check` 退出 1。旧债可以先留着。新债不行。没有服务端，不用注册，也不上传任何数据。

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

## 为什么不直接把覆盖率卡在 80%？

因为仓库现在不是 80%。是 62%，还搁着三个 700 行的文件，并且这周要上线。阈值卡在理想值，每个 PR 都红；卡在现状，等于没卡。

pawl 从今天的数字起算。第一份快照就是底线。之后谁改差了，CI 就红。真修好一项，就只把那一项重新记进去，新底线锁住。按文件、按 key 比的时候，修 A 文件不能拿来抵 B 文件新捅的娄子。

基线就是 Git 里的一个 JSON，所以 `pawl trend` 直接读你已有的提交记录。

## 和别的做法比

"不让某个数字变差"不是新想法。各家的区别在于判定在哪里算，以及能保护的数字有多宽。

| | 要不要服务端或账号 | 语言 | 保护什么 |
|---|---|---|---|
| **pawl** | 不要，基线就是仓库里的一个 JSON | 都行，靠适配器和自定义命令 | 任何命令能打印出来的数字 |
| SonarQube Server / Cloud | 要，判定在服务端算，免费的自托管版也一样 | 35+，靠它自带的分析器 | 它自己的分析器报出来的问题 |
| Qlty（原 Code Climate Quality） | CLI 不用，但趋势和 PR 看板在它的云上 | 40+，靠内置的 70+ 个分析器 | 这些分析器报出来的东西 |
| Codecov / Coveralls | 要，报告得传上去 | 都行，靠覆盖率报告格式 | 只有覆盖率 |
| betterer | 不用 | Node.js；自带的测试针对 JS、TS、CSS | 它那套泛型测试 API 返回的任何值 |
| git-ratchet | 不用，基线存在 git-notes 里 | 都行，从标准输入读 `measure,value` | 任何管进去的数字 |

有两样东西经常被当成同一回事，值得分清楚：

**SonarQube 的 "Clean as You Code" 不是棘轮。** 它是拿固定阈值去卡*新代码*，也就是这次改动碰过的那些行。这和"整个仓库的这个数字是不是比上周记下来的那次差"是两个问题。两种做法都允许老代码继续烂着，但只有后者能发现总数在往下滑。

**diff 过滤没有记忆。** `golangci-lint --new-from-rev` 和 `reviewdog -filter-mode=added` 把问题收窄到改动过的行，这很有用，也很便宜。但有些数字变差的时候，没有任何一行是被人改过的：依赖装进来一堆代码、产物体积涨了、某个测试开始被 skip。过滤器对这些一无所知。

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

读取磁盘报告的维度可以设置 `artifact_max_age: "24h"`，让过期报告直接以退出码 2 判为无法测量；本次命令生成的报告则天然视为新鲜。未设置时，pawl 只把年龄作为证据提示。

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

      - uses: tiangong-dev/pawl@v0.8.2
        with:
          command: check
          args: --since origin/${{ github.base_ref || 'main' }}
          guard-ref: origin/${{ github.base_ref || 'main' }}
```

传入 `command: check` 后，Action 会执行门禁，并可根据 JSON 裁决维护一条 PR 评论；设置 `guard-ref`（使用前先 fetch 目标 ref）即可让同一个 Action 同时保护 baseline。如果 `args` 中有 `-c/--config`，guard 会复用同一份配置；guard 会先于可选的 PR 评论执行。不需要评论时设置 `comment: 'false'`——顺手把 job 的 `permissions:` 里的 `pull-requests: write` 也删掉，因为整个 Action 里只有评论这一步需要它。如果不传 `command`，Action 只负责把 pawl 安装到 `PATH`。

### GitLab 等没有原生 pawl 挂件的系统

`pawl check --format json` 是稳定契约；[scripts/gitlab-codequality.mjs](scripts/gitlab-codequality.mjs) 把一份裁决转换成 [GitLab Code Quality](https://docs.gitlab.com/ci/testing/code_quality/) 报告，供 MR 挂件使用。这是一个转换脚本，不是 pawl 的输出格式——GitLab 不像 GitHub Actions 那样是 pawl 的目标平台，所以它留在 CLI 表面之外。

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

上面的 `main` 现在就能取到脚本；等脚本进了某个 release 之后，再改成固定某个 tag 或 commit（跟本文档别处固定 `guard-ref`/Action 版本一样）——也可以直接把文件 vendor 进仓库。先把 `rc` 存下来再做转换，最后再 `exit`——用 `&&` 会在回归/测不出来的退出码上短路，恰好在挂件最需要的时候不生成报告。测不出来（exit 2）时仍会产出一条 blocker 级别的 issue 而不是空数组，门禁坏掉不能在挂件上读成干净。

pawl 报告的路径是相对配置文件所在目录的，不是相对仓库根目录。如果上面的 `pawl check` 是用 `-c config/pawl.yaml` 跑的，就要把这个目录传过去，GitLab 才能把 issue 挂到正确的文件上：`node gitlab-codequality.mjs pawl.json --config-dir=config`。`--anchor`（测不出来/纯 total 兜底时用的定位）本身就是相对配置目录的，默认值 `pawl.yaml`，配合 `--config-dir` 已经能解析对，不需要再叠一层目录——只有配置文件不叫 `pawl.yaml` 时才需要覆盖它，比如 `config/quality.yaml` 对应 `--anchor=quality.yaml`，而不是 `--anchor=config/quality.yaml`。

### 其他 CI

Jenkins、CircleCI、Buildkite、Azure Pipelines、Woodpecker——只要能跑一个二进制就不需要插件。可以下载 release 二进制、使用 npm 包，或直接运行：

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

CI 仍然说了算。`pawl agent` 只是写下操作说明：跑 `pawl check`、读 JSON 结论、修好了只记那一项，别整份快照重写。评测和夹具在 [demo/](./demo/README.md)。

## 能力边界

pawl 负责的是**组织测量、比较基线、给出裁决**。它不是新的 linter、托管面板、包管理器或自动修复器。项目仍然自行安装和配置分析工具；pawl 把这些不同来源的数字纳入同一套基线和同一道可执行门禁。

- [配置示例](./RECIPES.md)：可复制调整的常见维度
- [引擎契约](./spec/README.md)：精确行为和文件格式
- [参与贡献](./CONTRIBUTING.md)：开发与测试流程
- [变更记录](./CHANGELOG.md)：版本升级说明

## 许可证

MIT，见 [LICENSE](./LICENSE)。
