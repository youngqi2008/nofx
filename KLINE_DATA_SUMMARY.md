# 实际交易K线数据总结

## 一、原始K线数据结构（从交易所获取）

### `market.Kline` 结构体
位置：`market/types.go:94-106`

```go
type Kline struct {
    OpenTime            int64   // 开盘时间（Unix毫秒时间戳）
    Open                float64 // 开盘价
    High                float64 // 最高价
    Low                 float64 // 最低价
    Close               float64 // 收盘价
    Volume              float64 // 成交量（基础资产）
    CloseTime           int64   // 收盘时间（Unix毫秒时间戳）
    QuoteVolume         float64 // 成交额（计价资产，如USDT）
    Trades              int     // 成交笔数
    TakerBuyBaseVolume  float64 // 主动买入成交量（基础资产）
    TakerBuyQuoteVolume float64 // 主动买入成交额（计价资产）
}
```

**数据来源：**
- 加密货币：通过 CoinAnk API 获取（免费开放API）
- xyz dex资产（股票、外汇等）：通过 Hyperliquid API 获取

## 二、传递给AI的K线数据结构

### `market.KlineBar` 结构体
位置：`market/types.go:23-30`

```go
type KlineBar struct {
    Time   int64   // Unix时间戳（毫秒）
    Open   float64 // 开盘价
    High   float64 // 最高价
    Low    float64 // 最低价
    Close  float64 // 收盘价
    Volume float64 // 成交量
}
```

**实际传递给AI的格式：**
```
Time(UTC)      Open      High      Low       Close     Volume
01-02 15:04    1234.56   1250.00   1230.00   1245.00   123456.78
```

**说明：**
- 只显示最近30根K线（如果配置的K线数量>30）
- 最后一根K线标记为"<- current"或"<- 当前"
- 时间格式：MM-DD HH:MM (UTC)

## 三、每个时间框架的完整数据

### `market.TimeframeSeriesData` 结构体
位置：`market/types.go:32-48`

```go
type TimeframeSeriesData struct {
    Timeframe   string     // 时间框架标识，如 "5m", "15m", "1h", "4h"
    Klines      []KlineBar // 完整的OHLCV K线数据数组
    MidPrices   []float64  // 价格序列（已废弃，保留用于兼容性）
    EMA20Values []float64  // EMA20序列
    EMA50Values []float64  // EMA50序列
    MACDValues  []float64  // MACD序列
    RSI7Values  []float64  // RSI7序列
    RSI14Values []float64  // RSI14序列
    Volume      []float64  // 成交量序列（已废弃，使用Klines）
    ATR14       float64    // ATR14值
    BOLLUpper   []float64  // 布林带上轨
    BOLLMiddle  []float64  // 布林带中轨（SMA）
    BOLLLower   []float64  // 布林带下轨
}
```

## 四、实际交易中使用的K线数据字段

### 1. 基础OHLCV数据（必需）
- **Time**: 时间戳（毫秒）
- **Open**: 开盘价
- **High**: 最高价
- **Low**: 最低价
- **Close**: 收盘价
- **Volume**: 成交量

### 2. 技术指标（根据配置启用）
- **EMA20**: 20周期指数移动平均线
- **EMA50**: 50周期指数移动平均线
- **MACD**: MACD指标值
- **RSI7**: 7周期相对强弱指标
- **RSI14**: 14周期相对强弱指标
- **ATR14**: 14周期平均真实波幅
- **BOLL**: 布林带（上轨、中轨、下轨）

### 3. 多时间框架支持
系统支持多个时间框架，每个时间框架都有独立的K线数据和技术指标：

**支持的时间周期（共14个）：**

| 时间周期 | 时长 | 分类 | 说明 |
|---------|------|------|------|
| `1m` | 1分钟 | 超短线 | 适用于高频交易、套利 |
| `3m` | 3分钟 | 超短线 | 默认日内交易周期 |
| `5m` | 5分钟 | 超短线 | 常用短线周期 |
| `15m` | 15分钟 | 日内 | 常用日内周期 |
| `30m` | 30分钟 | 日内 | 日内交易周期 |
| `1h` | 1小时 | 日内 | 常用周期 |
| `2h` | 2小时 | 日内/波段 | 短期波段 |
| `4h` | 4小时 | 波段 | 常用波段周期 |
| `6h` | 6小时 | 波段 | 中期波段 |
| `8h` | 8小时 | 波段 | 中期波段 |
| `12h` | 12小时 | 波段 | 中期波段 |
| `1d` | 1天 | 持仓 | 日线级别 |
| `3d` | 3天 | 持仓 | 多日周期 |
| `1w` | 1周 | 持仓 | 周线级别 |

**时间框架配置说明：**
- **主时间框架（Primary Timeframe）**: 用于计算当前指标（CurrentEMA20, CurrentMACD, CurrentRSI7）
- **其他时间框架**: 用于多时间框架分析，可同时选择多个
- **默认配置**: 通常使用 `3m` 或 `5m` 作为主时间框架，`4h` 作为长期时间框架

## 五、交易上下文中配置的K线数据周期

### 配置方式

交易上下文（`Context`）中的K线数据周期通过策略配置（`StrategyConfig`）来设置，支持两种配置方式：

#### 1. 新配置方式（推荐）：多时间周期选择
```go
SelectedTimeframes []string  // 选中的时间周期列表，例如：["5m", "15m", "1h", "4h"]
PrimaryTimeframe   string    // 主时间周期，例如："5m"
```

#### 2. 旧配置方式（兼容）：主周期+长期周期
```go
PrimaryTimeframe string  // 主时间周期，例如："3m" 或 "5m"
LongerTimeframe  string  // 长期时间周期，例如："4h"
```

### 默认配置

系统默认配置（`store/strategy.go:217-225`）：
```go
SelectedTimeframes: []string{"3m", "5m", "15m", "30m", "1h", "4h", "1d"}  // 默认7个时间周期
PrimaryTimeframe:   "5m"                                                   // 主时间周期
LongerTimeframe:    "4h"                                                   // 长期时间周期
```

### 配置逻辑

在 `kernel/engine.go:fetchMarketDataWithStrategy()` 中的处理逻辑：

1. **优先使用新配置**：
   - 如果 `SelectedTimeframes` 不为空，使用这些时间周期
   - 如果 `PrimaryTimeframe` 为空，使用 `SelectedTimeframes[0]` 作为主时间周期

2. **兼容旧配置**：
   - 如果 `SelectedTimeframes` 为空：
     - 使用 `PrimaryTimeframe`（如果为空则默认 `"3m"`）
     - 如果配置了 `LongerTimeframe`，也会加入时间周期列表

3. **K线数量**：
   - `PrimaryCount`：主时间周期的K线数量（默认30根）
   - `LongerCount`：长期时间周期的K线数量（默认10根）

### 实际使用示例

**示例1：默认配置**
```json
{
  "selected_timeframes": ["3m", "5m", "15m", "30m", "1h", "4h", "1d"],
  "primary_timeframe": "5m",
  "primary_count": 30
}
```
**结果**：交易上下文中包含7个时间周期的K线数据：`3m`, `5m`, `15m`, `30m`, `1h`, `4h`, `1d`

**示例2：自定义配置**
```json
{
  "selected_timeframes": ["3m", "15m", "1h", "4h", "1d"],
  "primary_timeframe": "3m",
  "primary_count": 50
}
```
**结果**：交易上下文中包含5个时间周期的K线数据：`3m`, `15m`, `1h`, `4h`, `1d`

**示例3：旧配置方式（兼容）**
```json
{
  "primary_timeframe": "5m",
  "longer_timeframe": "4h",
  "primary_count": 30
}
```
**结果**：交易上下文中包含2个时间周期的K线数据：`5m`, `4h`

### 数据存储位置

配置的时间周期数据存储在：
- `ctx.MarketDataMap[symbol].TimeframeData` - 每个币种的市场数据
- `TimeframeData` 是一个 `map[string]*TimeframeSeriesData`，key 为时间周期字符串（如 `"5m"`, `"1h"`）

### 代码位置

- **配置读取**: `kernel/engine.go:330-352` (fetchMarketDataWithStrategy)
- **数据获取**: `market/data.go:249-358` (GetWithTimeframes)
- **数据格式化**: `kernel/formatter.go:318-356` (formatKlineDataZH/EN)

## 六、数据获取流程

1. **获取原始K线数据**
   - 调用 `market.GetWithTimeframes()` 或 `market.Get()`
   - 从 CoinAnk API（加密货币）或 Hyperliquid API（xyz dex资产）获取

2. **数据转换**
   - 将 `market.Kline` 转换为 `market.KlineBar`
   - 计算技术指标（EMA, MACD, RSI, ATR, BOLL）

3. **数据格式化**
   - 通过 `kernel/formatter.go` 中的 `formatKlineDataZH()` 或 `formatKlineDataEN()` 格式化
   - 传递给AI进行决策分析

## 七、关键代码位置

- **数据结构定义**: `market/types.go`
- **数据获取**: `market/data.go` (GetWithTimeframes, getKlinesFromCoinAnk, getKlinesFromHyperliquid)
- **数据格式化**: `kernel/formatter.go` (formatKlineDataZH, formatKlineDataEN)
- **数据使用**: `kernel/engine.go` (fetchMarketDataWithStrategy, formatTimeframeSeriesData)

## 八、注意事项

1. **数据新鲜度检测**: 系统会检测数据是否过期（连续5个周期价格不变且成交量为0）
2. **数据量限制**: 默认显示最近30根K线，可通过配置调整
3. **精度处理**: 价格根据数值范围自动调整精度（支持从极低价格meme币到BTC/ETH）
4. **流动性过滤**: 对于候选币种，会过滤掉OI价值低于15M USD的币种（xyz dex资产除外）
