# pawl

**一个语言无关的防劣化质量门禁(anti-regression quality gate)。**

English docs: [README.md](./README.md) · 完整行为契约见 [SPEC.md](./SPEC.md)。

每个**维度(dimension)** 测量一个数字——超长文件数、重复代码行、超过复杂度阈值的函数、测试覆盖率……任何能用一条命令算出一个数的东西都行。`pawl record` 把这些数字拍成基线快照;`pawl check` 重新测量,**只要有任何维度变差就让 CI 失败**。数字只能持平或变好——门禁永不倒退。

```bash
pawl record                     # 测量全部维度,写入基线
pawl check                      # CI 门禁:任何维度回归即退出码 1
pawl diff                       # 测量 + 对比,打印表格,永不失败
pawl baseline-guard origin/main # 防篡改:抓手改过的基线
```

用什么工具测量是每个维度自己的实现细节。把 ESLint 换成别的 linter、或把整个项目迁移到 pawl,只需改写**一条 adapter 命令**——基线和 CI 门禁原封不动。

---

## 为什么用质量门禁?

一刀切的阈值("覆盖率必须 ≥ 80%")要么第一天就把团队卡死,要么松到永远不触发。防劣化质量门禁换个思路:锁定**你今天所在的位置**,只允许变好——加了 600 行的文件、多了一个 `as any`、覆盖率下降的 PR 会失败;删掉它们的 PR 则把基线重新压低。你单调地偿还技术债,却从不用去拍一个魔法数字。

pawl 守护的不只是数字,还有**诚实性**:

- 一次**跑不起来**的测量(工具崩溃、报告缺失、超时)退出码 `2`——绝不静默当成"测得零"。"没测出来"和"测得是零"是两回事。
- `baseline-guard` 把已提交的快照与 PR 的目标分支对比,手改基线来伪造通过会被抓。

## 安装

```bash
npm install -D @pawl-tools/cli                          # 经 npm 装预编译二进制
go install github.com/tiangong-dev/pawl/cmd/pawl@latest # 或从源码构建
curl -fsSL https://raw.githubusercontent.com/tiangong-dev/pawl/main/install.sh | sh
```

`install.sh` 用机器上已有的工具链——有 Go 用 Go、有 npm 用 npm、都没有就直接下二进制。预编译二进制覆盖 darwin / linux / win32 的 x64 / arm64,且完全静态(不含 CGO),同一个 Linux 二进制在 glibc 和 musl 上都能跑。

pawl 本身是个零依赖的小 Go 二进制。adapter 自带各自的运行时(ESLint 维度需要 Node 等)——见[自定义 adapter](#自定义-adapter)。

## 快速上手

**1. 在仓库根写 `pawl.yaml`:**

```yaml
dimensions:
  - id: "file-length"
    title: "超过 500 行的文件"
    direction: "lower-is-better"
    builtin: "file-length"
    options:
      threshold: 500
      include: ["src/**/*.ts"]
      exclude: ["**/*.d.ts"]

  - id: "todos"
    title: "TODO / FIXME 标记"
    direction: "lower-is-better"
    builtin: "pattern-count"
    options:
      pattern: "TODO|FIXME"
      include: ["src/**/*.ts"]
```

**2. 记录基线**——提交生成的 `pawl.snapshot.json`:

```bash
pawl record
git add pawl.yaml pawl.snapshot.json && git commit -m "chore: 接入 pawl 门禁"
```

**3. 门禁每个 PR**——有维度回归时 `pawl check` 退出码 `1`:

```bash
pawl check
```

**4. 锁定改进。** 当某个 PR 把数字改好了,`check` 会提示你重新记录;`pawl record` 写入更低的新基线,从此它再也回不去。

## 命令

| 命令 | 作用 |
|---|---|
| `pawl record` | 测量全部维度,(覆盖)写入快照 |
| `pawl check` | 测量 + 对比;**任何回归退出码 1**——CI 门禁 |
| `pawl diff` | 测量 + 对比,打印表格,永远退出码 0 |
| `pawl baseline-guard <ref>` | 把工作区快照与 `<ref>` 处提交的版本对比——防篡改门禁 |
| `pawl version` | 打印 `pawl <version>`(无配置也能跑) |

`-c <path>` 指定配置文件(默认 `./pawl.yaml`)。不带命令**不会**默认成 `check`。

**旗标。** `--format json` 让 `record`/`check`/`diff` 输出稳定的机器可读裁决而非表格([schema](./SPEC.md))——pawl 只当门禁,任何 reporter 去消费这份 JSON。`--format codeclimate` 输出 [Code Climate 问题数组](#gitlab-code-quality),供 GitLab 的 Code Quality 面板渲染。`check --since <ref>` 把门禁收窄到相对 `<ref>` 改动的行([clean-as-you-code](#diff-收窄检查))。

### 退出码

| 码 | 含义 |
|------|---------|
| **0** | 通过(也包括带回归的 `diff`、以及 `baseline-guard` 的合理跳过) |
| **1** | `check`:某维度回归 · `baseline-guard`:快照相对 `<ref>` 变差 |
| **2** | 无法诚实测量/对比:配置错误、快照缺失/损坏、工具崩溃、超时、未知命令…… |

**1 与 2 的区分是承重的**:`1` 表示"测量正常,代码变差了";`2` 表示"没能诚实测量",绝不能被当成通过。

## 配置

`pawl.yaml` 列出各维度;每个维度要么是**内置(builtin)**、要么是**自定义命令(command)**(`builtin` / `command` 二选一,恰好一个)。

```yaml
snapshot: "pawl.snapshot.json"   # 可选,相对本文件所在目录

dimensions:
  - id: "cognitive-complexity"   # 必填,唯一
    title: "认知复杂度 > 15 的函数" # 必填,给人看的标题
    direction: "lower-is-better" # 必填:lower-is-better | higher-is-better
    gate: "per-file-count"       # 可选:total(默认) | per-file-count | per-key-value
    tolerance: 0                 # 可选,向变差方向的绝对容差
    timeout: "10m"               # 可选 Go duration,默认 10m
    builtin: "eslint"            # 内置 adapter……
    options:
      command: "npx eslint src --format json --no-inline-config"
      rules: ["sonarjs/cognitive-complexity"]

  - id: "coverage"
    title: "行覆盖率"
    direction: "higher-is-better"
    gate: "per-key-value"
    tolerance: 1
    command: "./scripts/coverage.sh"   # ……或一条自定义命令
```

## 内置 adapter

分两层。**原语(primitives)** 是 Go 原生实现(零依赖)。**工具 adapter** 运行**你**提供的分析器命令、解析它的机器输出——pawl 掌握格式知识,你掌握工具配置。

| builtin | 层 | 测量什么 | 常用 gate |
|---|---|---|---|
| `file-length` | 原语 | 行数超过 `threshold` 的文件数 | `total` |
| `pattern-count` | 原语 | 正则匹配数(各种抑制/逃生舱:`as any`、`//nolint`、`try!`) | `per-file-count` |
| `eslint` | adapter | ESLint 消息计数(可用 `rules` 过滤) | `per-file-count` |
| `jscpd` | adapter | 从 jscpd JSON 报告读重复行数 | `total` |
| `swift-complexity` | adapter | Swift **认知**复杂度超标函数(SwiftLint 测不了的) | `per-file-count` |
| `json-value` | adapter | 从任意工具 JSON 里读一个数(覆盖率 %、通过用例数、type-coverage)——`higher-is-better` 的家 | `per-key-value` |

每个 builtin 的确切选项、退出码处理、breakdown 形状见 [SPEC.md § Built-in adapters](./SPEC.md)。完整示例配置在各消费项目里。

## 自定义 adapter

**pawl 不限制编程语言。** 维度的 `command` 经 `sh -c` 执行——可以是 shell 脚本、Node、Python、Go、编译好的二进制、`curl | jq`,什么都行。它只需遵守契约:

- 往 stdout 打印**恰好一个 JSON 对象**:
  ```json
  { "value": 42, "unit": "things", "breakdown": { "src/a.ts:17": 2 } }
  ```
  `value` 必填且为有限数。`unit` 默认 `"count"`。`breakdown` 可选(`null` 与省略等价)。
- **退出码 0 = 一次测量;非 0 / 超时 / 非 JSON 的 stdout = 测量失败** → pawl 以退出码 2 中止整轮。这正是原始命令强过 `tool || true` 的地方:真实失败依然可被发现。
- 工作目录 = 配置文件所在目录;`PAWL_ROOT` 设为其绝对路径;stderr 透传给人看诊断。

`breakdown` 的 key 决定 [gate 模式](#gate-模式):`per-file-count` 用 `"<path>:<line>"` 形式的 key,`per-key-value` 用具名 key(如 `"pkg-a": 91.2`)。

> 任何项目迁移到 pawl 都靠这个——不需要 pawl 理解它的工具:把现有测量包成一条打印上述 JSON 的命令即可。

### 免写 wrapper:`extract`

当工具本身就打印数字(或一列可 grep 的发现)时,在 `command` 维度上声明 `extract`,pawl 直接派生测量——不用再写只为拼 JSON 的 wrapper 脚本。四种形态:

```yaml
- id: todos
  command: "grep -rn TODO src || true"
  direction: "lower-is-better"
  extract: lines            # value = 非空行数

- id: golangci
  command: "golangci-lint run ./... | grep -E '^[^:]+:[0-9]+:' || true"
  direction: "lower-is-better"
  gate: "per-file-count"
  extract:
    regex: '^(?P<path>[^:]+):(?P<line>\d+):'   # value = 匹配数;path/line → breakdown
```

另有 `extract: number`(stdout 就是一个数)和 `extract: { json_path: "a.b.c" }`(从命令 stdout 的 JSON 里读一个数)。诚实性规则不变:非 0 退出、或输出无法按声明抽取,都是测量失败(退出码 2)——用 `regex` 时每个非空行都必须匹配,写错的正则不会静默报零。细节见 [SPEC.md § Declarative extract layer](./SPEC.md)。

## gate 模式

标量总数**始终**会被检查(带 `tolerance`)。在其之上再叠一层 per-breakdown 检查,防止局部回归躲在净零总数背后(文件 A 变好、文件 B 变差、总数不变):

- **`total`**——只看标量。(把本已很长的文件改得更长不该失败;只有新文件越过限制、从而推动总数,才该失败。)
- **`per-file-count`**——每个文件的**违规计数**不得上升。文件 = 每个 breakdown key 里第一个 `:` 之前的子串。数 key 个数而非值,所以代码在文件内挪动不会触发。
- **`per-key-value`**——基线里每个 key 的**值**不得变差(带容差)。新增的 key、删除的 key 都忽略。适合按包统计的覆盖率 / type-coverage。

`tolerance` 是向变差方向的绝对容差;正好卡在边界上算通过。`higher-is-better` 与 `lower-is-better` 会把比较方向反过来。

按维度形态选 gate:`per-file-count` 是 *issue 计数*类维度(违规来来去去)最强的净零防护;`per-key-value` 适合 *key 稳定的数值*类维度(固定 key 集、值在动),且只守护基线里已有的 key。两者都不是万能的净零证明——[SPEC](./SPEC.md#gate-modes) 写清了各自的边界。

## CI 集成

pawl 是单个二进制——任何 CI 都能跑。两种常见接法:

### GitHub Actions

action 负责安装二进制;不传 `command` 时它只做这件事:

```yaml
- uses: tiangong-dev/pawl@v0.3.1   # 把 pawl 二进制放进 PATH——无需 Go/Node
  with:
    version: v0.3.1                # 可选;默认取最新 release
- run: pawl check
- run: pawl baseline-guard origin/${{ github.base_ref }}   # PR 上跑
```

传 `command` 时,action 还会跑门禁,并在 pull_request 上把结果渲染成一条 sticky 评论(取自 `--format json` 裁决)回写——不用再手写 `github-script`:

```yaml
# ... 门禁需要的前置步骤,如构建 exec adapter ...
- uses: tiangong-dev/pawl@v0.3.1
  with:
    command: check
    args: --since origin/${{ github.base_ref }}   # 可选的附加参数
    # comment: 'false'   # 默认 true;设 false 关掉 PR 评论
```

评论步骤需要 `permissions: pull-requests: write`。门禁退出码在评论之后强制执行,所以回归照样让 job 失败,同时评论仍会发出。在 `GITHUB_ACTIONS` 下,`check` 还会对每个回归在 PR diff 上发内联 `::error::` 注解,并在某维度变好但基线没重记时发 `::notice::`。

### GitLab Code Quality

`--format codeclimate` 输出 Code Climate 问题数组——把每个当前 per-file-count offender 作为带位置的 finding——GitLab 会渲染成 MR 的 **Code Quality** 面板与 diff 行内标注。新增/消除由 GitLab 自己比对 MR 分支与目标分支的报告得出,job 只负责产出工件:

```yaml
quality-gate:
  image: node:22
  script:
    - npx -y @pawl-tools/cli@0.3.1 check --format codeclimate > gl-code-quality-report.json
  artifacts:
    when: always                 # 门禁失败也要产出报告
    reports:
      codequality: gl-code-quality-report.json
```

`check` 的退出码仍然把关流水线(相对 snapshot 有回归即 1);`total`/`per-key-value` 维度无行内位置、不产生行内 finding,但其门禁仍靠退出码强制。

### 其他

pawl 是单个二进制——任何 CI 里跑 `npx -y @pawl-tools/cli@0.3.1 check`(或下载 release 二进制)即可。

### 防篡改

`pawl check` 只证明磁盘上的快照与一次新鲜测量一致——不证明快照的历史是诚实的。`pawl baseline-guard <base-ref>` 把已提交的快照与 PR 目标分支对比,若被手改成更差的值就失败。在 PR 上与 `check` 一起跑。

## diff 收窄检查

`pawl check --since <ref>` 保留完整门禁,但**只对相对 `<ref>` 改动的行上引入的回归失败**——未触碰行上的存量债务被豁免,于是庞大的历史基线不会卡住每个 PR,而新代码依然不能回归。它仍然需要快照(是"收窄到新代码的门禁",不是独立的新代码扫描器)。

```bash
pawl check --since origin/main        # PR 上:只对改动行门禁
```

`per-file-count` 维度(breakdown key 是 `"path:line"`、标量=offender 计数)会被收窄到新增行;`total` 与 `per-key-value` 维度无法忠实按行归属,**按全量强制**(会被显式标注,绝不静默跳过)——这样 `--since` 恰好是"full-mode 裁决收窄到改动行",不多不少。输出会报告 merge-base、哪些维度被全量强制、以及有多少条存量回归被豁免;加 `--format json` 得到机器可读形式(`mode: "since"`,每条被豁免的回归标 `suppressed`)。

作用域按**行号**(与 reviewdog / Sonar clean-as-you-code 同):内容未变、只是位置移动的存量 offender 不会被 flag,但落在**内容真正改动的行**上的 offender 即使"是移过来的"也会计入——它从不漏报改动行。细节见 [SPEC.md](./SPEC.md#diff-scoped-checking)。

## 边界(设计决策)

pawl 是**质量门禁 + 诚实守卫,不是代码分析器**——它从不解析任何语言。数行数、跑正则是 Go 原生的,因为它们不需要语法;所有需要真正语言语义的东西(复杂度、类型逃逸)都通过 adapter 委托给该语言自己最好的分析器,这样门禁给出的数字和开发者在 IDE 里看到的一致。理由见 [SPEC.md § Scope boundary](./SPEC.md)。

## 许可证

MIT——见 [LICENSE](./LICENSE)。
