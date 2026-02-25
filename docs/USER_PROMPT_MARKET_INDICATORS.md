# 用户提示词中的市场数据指标名称与查找方式

本文档列出**当前候选币种 / 持仓币种**在用户提示词里出现的**汇总数据指标名称**，以及如何在完整用户提示词中快速定位这些内容。

---

## 一、用户提示词由谁生成、结构概览

- **入口**：`kernel/engine.go` 中的 `StrategyEngine.BuildUserPrompt(ctx, previousDecisionRecord)`。
- **市场数据写入**：每个持仓/候选币种会调用 `formatMarketData(data)`，其输出为**仅汇总**（每周期 Current + Last 10/6 bars），**不包含** K 线表与各指标序列明细。

在用户提示词中的大致顺序为：

1. 时间与周期  
2. **BTC 一行**（若存在 BTCUSDT）  
3. **Account: …**  
4. Recent Completed Trades（若有）  
5. 历史/24h/今日交易统计与明细（若有）  
6. **## Current Positions**（或中文：当前持仓）  
7. **## Candidate Coins (N coins)**（或中文：候选币种）  
8. OI / NetFlow / Price Ranking 等（若有）  
9. 上一轮决策上下文（若有）  
10. 结尾一句 “Now please analyze and output your decision…”

**汇总数据指标**主要出现在：**每个持仓/候选币种的 “=== SYMBOL Market Data ===” 块** 以及 **BTC 那一行**。下面按「指标名称」和「如何查找」分别说明。

---

## 二、汇总数据指标名称列表（按在提示词中的出现顺序）

### 1. 顶部 BTC 一行（仅当有 BTCUSDT 时）

在提示词**最前面几行**，可能出现类似：

```
BTC: 97234.56 (1h: +1.23%, 4h: -0.45%) | MACD: 0.1234 | RSI: 58.00
```

可搜索的**固定前缀与指标名**：

| 在提示词中的写法 | 含义 |
|------------------|------|
| `BTC:`           | BTC 价格与涨跌幅、MACD、RSI 整行 |
| `1h:` / `4h:`    | 1 小时 / 4 小时涨跌幅（%） |
| `MACD:`          | 当前 MACD 值 |
| `RSI:`           | 当前 RSI（此处为 RSI7） |

---

### 2. 每个币种的 “=== SYMBOL Market Data ===” 块（持仓 + 候选）

每个持仓或候选币种会有一段以 **`=== 交易对 Market Data ===`** 开头的块，例如：

```
=== ETHUSDT Market Data ===

**Current (primary TF):** price = 3500.1234, ema20 = 3480.500, ema50 = 3450.200, macd = 12.340, rsi7 = 58.200, rsi14 = 55.100, k = 72.300, d = 68.100, j = 80.700
```

下面列出这段里会出现的**汇总指标名称**（与代码中 `formatMarketData` 输出一致，便于你 Ctrl+F 搜索）：

| 指标名称（在提示词中精确字符串） | 含义 | 是否受策略配置开关控制 |
|--------------------------------|------|------------------------|
| `**Current (primary TF):**`     | 当前时刻汇总（主周期） | — |
| `price`                        | 当前价格 | 否，始终有 |
| `ema20` / `ema50`              | 当前 EMA20 / EMA50 | 是，`EnableEMA` |
| `macd`                         | 当前 MACD | 是，`EnableMACD` |
| `rsi7` / `rsi14`               | 当前 RSI7 / RSI14 | 是，`EnableRSI` |
| `k` / `d` / `j`                | KDJ 的 K、D、J 值 | 是，`EnableKDJ` |

紧接着可能还有（若配置开启）：

| 指标名称（在提示词中的写法） | 含义 |
|-----------------------------|------|
| `Additional data for SYMBOL:` | 下面为 OI / 资金费率 的引导行 |
| `Open Interest: Latest:`      | 持仓量最新值 |
| `Open Interest: ... Average:` | 持仓量平均值（同一行或下一行） |
| `Funding Rate:`               | 资金费率 |

**在提示词里如何找到：**

- 搜 **`=== `** 可定位到每个币种的 Market Data 块。
- 搜 **`current_price`** 可找到所有「当前价 + 当前指标」那一行。
- 搜 **`Open Interest`** / **`Funding Rate`** 可找到 OI 与资金费率。

---

### 3. 多周期块内：当前时刻 + 最近 10 根 / 6 根汇总（按周期开关）

每个 **`=== XM Timeframe (oldest → latest) ===`** 块内，会先输出该周期的**当前时刻**与**最近 10 根、6 根 K 线的 max/min/avg**（受策略指标开关控制）：

- **`**Current (this TF):**`**：本周期最后一根的价格、ema20、ema50、macd、rsi7、rsi14、ATR14、BOLL Upper/Middle/Lower、k/d/j。
- **`**Last 10 bars (max / min / avg):**`**：最近 10 根 K 线对应的 price、ema20、ema50、macd、rsi7、rsi14、BOLL、KDJ、volume 的 最大值 / 最小值 / 平均值（volume 受 `EnableVolume` 控制）。
- **`**Last 6 bars (max / min / avg):**`**：最近 6 根 K 线同上（含 volume 统计）。

在提示词中搜索 **`Last 10 bars`** 或 **`Last 6 bars`** 可定位到这些汇总。

**说明**：用户提示词中**不再输出**各周期的 K 线表（Time/Open/High/Low/Close/Volume）以及 EMA/MACD/RSI/BOLL/KDJ 等指标序列明细，仅保留上述「当前时刻 + Last 10/6 bars 汇总」，以减少重复、控制篇幅。

---

### 4. 量化数据块（若启用 QuantData）

在部分币种下还会出现量化数据，例如：

```
📊 SYMBOL Quantitative Data:
Price Change: 5m: +0.12% | 15m: -0.30% | ...
```

可搜索：**`Quantitative Data`**、**`Price Change`**（以及配置中的多周期标签如 `5m`、`15m`、`1h` 等）。

---

## 三、在用户提示词中如何快速定位（实操）

1. **找「所有候选/持仓的汇总行情」**  
   - 搜：**`=== `** 或 **`Market Data ===`**，每个匹配即一个币种的市场数据块；块内第一段就是「当前价 + 当前指标」的汇总行。

2. **找「当前价与当前指标」**  
   - 搜：**`current_price`**，会命中每个币种的那一行（含 current_ema20 / current_macd / current_rsi7 / current_k,d,j 等，取决于配置）。

3. **找「持仓量 / 资金费率」**  
   - 搜：**`Open Interest`** 或 **`Funding Rate`**。

4. **找「BTC 的 1h/4h 与 MACD/RSI」**  
   - 搜：**`BTC:`**，通常就在提示词前几行。

5. **找「某周期汇总」**  
   - 搜：**`=== 1M Timeframe`** 或 **`=== 5M Timeframe`**（把 1M/5M 换成实际周期）可定位到该周期块；块内仅有 Current (this TF) 与 Last 10/6 bars 汇总，无 K 线表与指标序列。

6. **区分持仓与候选**  
   - 用户提示词中先出现 **## Current Positions**，再出现 **## Candidate Coins**；每个位置下面的 **`=== SYMBOL Market Data ===`** 即该币种的汇总（含各周期 Current + Last 10/6 bars），无明细序列。

---

## 四、代码位置（便于对照或修改）

| 内容 | 文件与位置 |
|------|------------|
| 用户提示词整体结构、BTC 行、Account、持仓/候选列表 | `kernel/engine.go`：`BuildUserPrompt`（约 1099 行起） |
| 每个币种「汇总 + OI/Funding」输出 | `kernel/engine.go`：`formatMarketData`（约 1549 行起） |
| 多周期仅汇总（Current + Last 10/6 bars，无 K 线表与指标序列） | `kernel/engine.go`：`formatTimeframeSeriesData` → `writeTimeframeSummary` |
| 策略里指标开关（EnableEMA / EnableMACD / EnableRSI / EnableKDJ 等） | `store` 包中策略/指标配置 |

把上述**指标名称**在日志或调试里打印出的完整 userPrompt 中做 Ctrl+F，即可精确找到对应汇总数据在用户提示词中的位置。
