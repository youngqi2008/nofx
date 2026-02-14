package market

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"
)

// FundingRateCache 资金费率缓存结构
// Binance Funding Rate 每 8 小时才更新一次，使用 1 小时缓存可显著减少 API 调用
type FundingRateCache struct {
	Rate      float64
	UpdatedAt time.Time
}

var (
	fundingRateMap sync.Map // map[string]*FundingRateCache
	frCacheTTL     = 1 * time.Hour
)

// Get 获取指定代币的市场数据
func Get(symbol string) (*Data, error) {
	var klines3m, klines4h []Kline
	var err error
	// 标准化symbol
	symbol = Normalize(symbol)
	// 获取3分钟K线数据 (最近10个)
	klines3m, err = WSMonitorCli.GetCurrentKlines(symbol, "3m") // 多获取一些用于计算
	if err != nil {
		return nil, fmt.Errorf("获取3分钟K线失败: %v", err)
	}

	// Data staleness detection: Prevent DOGEUSDT-style price freeze issues
	if isStaleData(klines3m, symbol) {
		log.Printf("⚠️  WARNING: %s detected stale data (consecutive price freeze), skipping symbol", symbol)
		return nil, fmt.Errorf("%s data is stale, possible cache failure", symbol)
	}

	// 获取4小时K线数据 (最近10个)
	klines4h, err = WSMonitorCli.GetCurrentKlines(symbol, "4h") // 多获取用于计算指标
	if err != nil {
		return nil, fmt.Errorf("获取4小时K线失败: %v", err)
	}

	// 检查数据是否为空
	if len(klines3m) == 0 {
		return nil, fmt.Errorf("3分钟K线数据为空")
	}
	if len(klines4h) == 0 {
		return nil, fmt.Errorf("4小时K线数据为空")
	}

	// 计算当前指标 (基于3分钟最新数据)
	currentPrice := klines3m[len(klines3m)-1].Close
	currentEMA20 := calculateEMA(klines3m, 20)
	currentEMA50 := calculateEMA(klines3m, 50)
	currentMACD := calculateMACD(klines3m)
	currentRSI7 := calculateRSI(klines3m, 7)
	currentRSI14 := calculateRSI(klines3m, 14)
	currentK, currentD, currentJ := calculateKDJ(klines3m)

	// 计算价格变化百分比
	// 1小时价格变化 = 20个3分钟K线前的价格
	priceChange1h := 0.0
	if len(klines3m) >= 21 { // 至少需要21根K线 (当前 + 20根前)
		price1hAgo := klines3m[len(klines3m)-21].Close
		if price1hAgo > 0 {
			priceChange1h = ((currentPrice - price1hAgo) / price1hAgo) * 100
		}
	}

	// 4小时价格变化 = 1个4小时K线前的价格
	priceChange4h := 0.0
	if len(klines4h) >= 2 {
		price4hAgo := klines4h[len(klines4h)-2].Close
		if price4hAgo > 0 {
			priceChange4h = ((currentPrice - price4hAgo) / price4hAgo) * 100
		}
	}

	// 获取OI数据
	oiData, err := getOpenInterestData(symbol)
	if err != nil {
		// OI失败不影响整体,使用默认值
		oiData = &OIData{Latest: 0, Average: 0}
	}

	// 获取Funding Rate
	fundingRate, _ := getFundingRate(symbol)

	// 计算日内系列数据
	intradayData := calculateIntradaySeries(klines3m)

	// 计算长期数据
	longerTermData := calculateLongerTermData(klines4h)

	return &Data{
		Symbol:            symbol,
		CurrentPrice:      currentPrice,
		PriceChange1h:     priceChange1h,
		PriceChange4h:     priceChange4h,
		CurrentEMA20:      currentEMA20,
		CurrentEMA50:      currentEMA50,
		CurrentMACD:       currentMACD,
		CurrentRSI7:       currentRSI7,
		CurrentRSI14:      currentRSI14,
		CurrentKDJK:       currentK,
		CurrentKDJD:       currentD,
		CurrentKDJJ:       currentJ,
		OpenInterest:      oiData,
		FundingRate:       fundingRate,
		IntradaySeries:    intradayData,
		LongerTermContext: longerTermData,
	}, nil
}

// calculateEMA 计算EMA
func calculateEMA(klines []Kline, period int) float64 {
	if len(klines) < period {
		return 0
	}

	// 计算SMA作为初始EMA
	sum := 0.0
	for i := 0; i < period; i++ {
		sum += klines[i].Close
	}
	ema := sum / float64(period)

	// 计算EMA
	multiplier := 2.0 / float64(period+1)
	for i := period; i < len(klines); i++ {
		ema = (klines[i].Close-ema)*multiplier + ema
	}

	return ema
}

// calculateMACD 计算MACD
func calculateMACD(klines []Kline) float64 {
	if len(klines) < 26 {
		return 0
	}

	// 计算12期和26期EMA
	ema12 := calculateEMA(klines, 12)
	ema26 := calculateEMA(klines, 26)

	// MACD = EMA12 - EMA26
	return ema12 - ema26
}

// calculateRSI 计算RSI
func calculateRSI(klines []Kline, period int) float64 {
	if len(klines) <= period {
		return 0
	}

	gains := 0.0
	losses := 0.0

	// 计算初始平均涨跌幅
	for i := 1; i <= period; i++ {
		change := klines[i].Close - klines[i-1].Close
		if change > 0 {
			gains += change
		} else {
			losses += -change
		}
	}

	avgGain := gains / float64(period)
	avgLoss := losses / float64(period)

	// 使用Wilder平滑方法计算后续RSI
	for i := period + 1; i < len(klines); i++ {
		change := klines[i].Close - klines[i-1].Close
		if change > 0 {
			avgGain = (avgGain*float64(period-1) + change) / float64(period)
			avgLoss = (avgLoss * float64(period-1)) / float64(period)
		} else {
			avgGain = (avgGain * float64(period-1)) / float64(period)
			avgLoss = (avgLoss*float64(period-1) + (-change)) / float64(period)
		}
	}

	if avgLoss == 0 {
		return 100
	}

	rs := avgGain / avgLoss
	rsi := 100 - (100 / (1 + rs))

	return rsi
}

// calculateKDJ 计算 KDJ 指标（9,3,3），返回最后一根 K 线的 K, D, J
func calculateKDJ(klines []Kline) (k, d, j float64) {
	const n, m1, m2 = 9, 3, 3
	if len(klines) < n {
		return 50, 50, 50
	}
	rsvs := make([]float64, len(klines))
	for i := n - 1; i < len(klines); i++ {
		high := klines[i].High
		low := klines[i].Low
		for jj := i - n + 1; jj <= i; jj++ {
			if klines[jj].High > high {
				high = klines[jj].High
			}
			if klines[jj].Low < low {
				low = klines[jj].Low
			}
		}
		if high == low {
			rsvs[i] = 50
		} else {
			rsvs[i] = (klines[i].Close - low) / (high - low) * 100
		}
	}
	// K = 2/3*K_prev + 1/3*RSV, D = 2/3*D_prev + 1/3*K, J = 3K - 2D
	start := n - 1
	kVal := rsvs[start]
	dVal := kVal
	for i := start + 1; i < len(rsvs); i++ {
		kVal = (kVal*float64(m1-1) + rsvs[i]) / float64(m1)
		dVal = (dVal*float64(m2-1) + kVal) / float64(m2)
	}
	jVal := 3*kVal - 2*dVal
	return kVal, dVal, jVal
}

// calculateBOLL 计算布林带（20 周期，2 倍标准差），返回最后一根 K 线的 Upper, Mid, Lower
func calculateBOLL(klines []Kline, period int, mult float64) (upper, mid, lower float64) {
	if len(klines) < period {
		return 0, 0, 0
	}
	slice := klines[len(klines)-period:]
	sum := 0.0
	for _, k := range slice {
		sum += k.Close
	}
	mid = sum / float64(period)
	variance := 0.0
	for _, k := range slice {
		variance += (k.Close - mid) * (k.Close - mid)
	}
	std := math.Sqrt(variance / float64(period))
	upper = mid + mult*std
	lower = mid - mult*std
	return upper, mid, lower
}

// calculateATR 计算ATR
func calculateATR(klines []Kline, period int) float64 {
	if len(klines) <= period {
		return 0
	}

	trs := make([]float64, len(klines))
	for i := 1; i < len(klines); i++ {
		high := klines[i].High
		low := klines[i].Low
		prevClose := klines[i-1].Close

		tr1 := high - low
		tr2 := math.Abs(high - prevClose)
		tr3 := math.Abs(low - prevClose)

		trs[i] = math.Max(tr1, math.Max(tr2, tr3))
	}

	// 计算初始ATR
	sum := 0.0
	for i := 1; i <= period; i++ {
		sum += trs[i]
	}
	atr := sum / float64(period)

	// Wilder平滑
	for i := period + 1; i < len(klines); i++ {
		atr = (atr*float64(period-1) + trs[i]) / float64(period)
	}

	return atr
}

// calculateIntradaySeries 计算日内系列数据
func calculateIntradaySeries(klines []Kline) *IntradayData {
	data := &IntradayData{
		MidPrices:   make([]float64, 0, 10),
		EMA20Values: make([]float64, 0, 10),
		MACDValues:  make([]float64, 0, 10),
		RSI7Values:  make([]float64, 0, 10),
		RSI14Values: make([]float64, 0, 10),
		Volume:      make([]float64, 0, 10),
	}

	// 获取最近10个数据点
	start := len(klines) - 10
	if start < 0 {
		start = 0
	}

	for i := start; i < len(klines); i++ {
		data.MidPrices = append(data.MidPrices, klines[i].Close)
		data.Volume = append(data.Volume, klines[i].Volume)

		// 计算每个点的EMA20
		if i >= 19 {
			ema20 := calculateEMA(klines[:i+1], 20)
			data.EMA20Values = append(data.EMA20Values, ema20)
		}

		// 计算每个点的MACD
		if i >= 25 {
			macd := calculateMACD(klines[:i+1])
			data.MACDValues = append(data.MACDValues, macd)
		}

		// 计算每个点的RSI
		if i >= 7 {
			rsi7 := calculateRSI(klines[:i+1], 7)
			data.RSI7Values = append(data.RSI7Values, rsi7)
		}
		if i >= 14 {
			rsi14 := calculateRSI(klines[:i+1], 14)
			data.RSI14Values = append(data.RSI14Values, rsi14)
		}
	}

	// 计算3m ATR14
	data.ATR14 = calculateATR(klines, 14)

	return data
}

// calculateLongerTermData 计算长期数据
func calculateLongerTermData(klines []Kline) *LongerTermData {
	data := &LongerTermData{
		MACDValues:  make([]float64, 0, 10),
		RSI14Values: make([]float64, 0, 10),
	}

	// 计算EMA
	data.EMA20 = calculateEMA(klines, 20)
	data.EMA50 = calculateEMA(klines, 50)

	// 计算ATR
	data.ATR3 = calculateATR(klines, 3)
	data.ATR14 = calculateATR(klines, 14)

	// 计算成交量
	if len(klines) > 0 {
		data.CurrentVolume = klines[len(klines)-1].Volume
		// 计算平均成交量
		sum := 0.0
		for _, k := range klines {
			sum += k.Volume
		}
		data.AverageVolume = sum / float64(len(klines))
	}

	// 计算MACD和RSI序列
	start := len(klines) - 10
	if start < 0 {
		start = 0
	}

	for i := start; i < len(klines); i++ {
		if i >= 25 {
			macd := calculateMACD(klines[:i+1])
			data.MACDValues = append(data.MACDValues, macd)
		}
		if i >= 14 {
			rsi14 := calculateRSI(klines[:i+1], 14)
			data.RSI14Values = append(data.RSI14Values, rsi14)
		}
	}

	return data
}

// getOpenInterestData 获取OI数据
func getOpenInterestData(symbol string) (*OIData, error) {
	url := fmt.Sprintf("https://fapi.binance.com/fapi/v1/openInterest?symbol=%s", symbol)

	apiClient := NewAPIClient()
	resp, err := apiClient.client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result struct {
		OpenInterest string `json:"openInterest"`
		Symbol       string `json:"symbol"`
		Time         int64  `json:"time"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	oi, _ := strconv.ParseFloat(result.OpenInterest, 64)

	return &OIData{
		Latest:  oi,
		Average: oi * 0.999, // 近似平均值
	}, nil
}

// getFundingRate 获取资金费率（优化：使用 1 小时缓存）
func getFundingRate(symbol string) (float64, error) {
	// 检查缓存（有效期 1 小时）
	// Funding Rate 每 8 小时才更新，1 小时缓存非常合理
	if cached, ok := fundingRateMap.Load(symbol); ok {
		cache := cached.(*FundingRateCache)
		if time.Since(cache.UpdatedAt) < frCacheTTL {
			// 缓存命中，直接返回
			return cache.Rate, nil
		}
	}

	// 缓存过期或不存在，调用 API
	url := fmt.Sprintf("https://fapi.binance.com/fapi/v1/premiumIndex?symbol=%s", symbol)

	apiClient := NewAPIClient()
	resp, err := apiClient.client.Get(url)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}

	var result struct {
		Symbol          string `json:"symbol"`
		MarkPrice       string `json:"markPrice"`
		IndexPrice      string `json:"indexPrice"`
		LastFundingRate string `json:"lastFundingRate"`
		NextFundingTime int64  `json:"nextFundingTime"`
		InterestRate    string `json:"interestRate"`
		Time            int64  `json:"time"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return 0, err
	}

	rate, _ := strconv.ParseFloat(result.LastFundingRate, 64)

	// 更新缓存
	fundingRateMap.Store(symbol, &FundingRateCache{
		Rate:      rate,
		UpdatedAt: time.Now(),
	})

	return rate, nil
}

// Format 格式化输出市场数据
func Format(data *Data) string {
	var sb strings.Builder

	// 使用动态精度格式化价格
	priceStr := formatPriceWithDynamicPrecision(data.CurrentPrice)
	sb.WriteString(fmt.Sprintf("current_price = %s, current_ema20 = %.3f, current_macd = %.3f, current_rsi (7 period) = %.3f\n\n",
		priceStr, data.CurrentEMA20, data.CurrentMACD, data.CurrentRSI7))

	sb.WriteString(fmt.Sprintf("In addition, here is the latest %s open interest and funding rate for perps:\n\n",
		data.Symbol))

	if data.OpenInterest != nil {
		// 使用动态精度格式化 OI 数据
		oiLatestStr := formatPriceWithDynamicPrecision(data.OpenInterest.Latest)
		oiAverageStr := formatPriceWithDynamicPrecision(data.OpenInterest.Average)
		sb.WriteString(fmt.Sprintf("Open Interest: Latest: %s Average: %s\n\n",
			oiLatestStr, oiAverageStr))
	}

	sb.WriteString(fmt.Sprintf("Funding Rate: %.2e\n\n", data.FundingRate))

	if data.IntradaySeries != nil {
		sb.WriteString("Intraday series (3‑minute intervals, oldest → latest):\n\n")

		if len(data.IntradaySeries.MidPrices) > 0 {
			sb.WriteString(fmt.Sprintf("Mid prices: %s\n\n", formatFloatSlice(data.IntradaySeries.MidPrices)))
		}

		if len(data.IntradaySeries.EMA20Values) > 0 {
			sb.WriteString(fmt.Sprintf("EMA indicators (20‑period): %s\n\n", formatFloatSlice(data.IntradaySeries.EMA20Values)))
		}

		if len(data.IntradaySeries.MACDValues) > 0 {
			sb.WriteString(fmt.Sprintf("MACD indicators: %s\n\n", formatFloatSlice(data.IntradaySeries.MACDValues)))
		}

		if len(data.IntradaySeries.RSI7Values) > 0 {
			sb.WriteString(fmt.Sprintf("RSI indicators (7‑Period): %s\n\n", formatFloatSlice(data.IntradaySeries.RSI7Values)))
		}

		if len(data.IntradaySeries.RSI14Values) > 0 {
			sb.WriteString(fmt.Sprintf("RSI indicators (14‑Period): %s\n\n", formatFloatSlice(data.IntradaySeries.RSI14Values)))
		}

		if len(data.IntradaySeries.Volume) > 0 {
			sb.WriteString(fmt.Sprintf("Volume: %s\n\n", formatFloatSlice(data.IntradaySeries.Volume)))
		}

		sb.WriteString(fmt.Sprintf("3m ATR (14‑period): %.3f\n\n", data.IntradaySeries.ATR14))
	}

	if data.LongerTermContext != nil {
		sb.WriteString("Longer‑term context (4‑hour timeframe):\n\n")

		sb.WriteString(fmt.Sprintf("20‑Period EMA: %.3f vs. 50‑Period EMA: %.3f\n\n",
			data.LongerTermContext.EMA20, data.LongerTermContext.EMA50))

		sb.WriteString(fmt.Sprintf("3‑Period ATR: %.3f vs. 14‑Period ATR: %.3f\n\n",
			data.LongerTermContext.ATR3, data.LongerTermContext.ATR14))

		sb.WriteString(fmt.Sprintf("Current Volume: %.3f vs. Average Volume: %.3f\n\n",
			data.LongerTermContext.CurrentVolume, data.LongerTermContext.AverageVolume))

		if len(data.LongerTermContext.MACDValues) > 0 {
			sb.WriteString(fmt.Sprintf("MACD indicators: %s\n\n", formatFloatSlice(data.LongerTermContext.MACDValues)))
		}

		if len(data.LongerTermContext.RSI14Values) > 0 {
			sb.WriteString(fmt.Sprintf("RSI indicators (14‑Period): %s\n\n", formatFloatSlice(data.LongerTermContext.RSI14Values)))
		}
	}

	return sb.String()
}

// appendIndicatorStatsLine 根据选中的 indicators 拼接一行 min/max/avg 文本（Last10 或 Last6）
func appendIndicatorStatsLine(sb *strings.Builder, s *IndicatorStats, indicators []string) {
	all := len(indicators) == 0
	parts := []string{}
	parts = append(parts, fmt.Sprintf("price min/max/avg=%.4f/%.4f/%.4f", s.PriceMin, s.PriceMax, s.PriceAvg))
	if all || indicatorInSet("ema", indicators) {
		parts = append(parts, fmt.Sprintf("ema20=%.4f/%.4f/%.4f", s.EMA20Min, s.EMA20Max, s.EMA20Avg))
	}
	if all || indicatorInSet("macd", indicators) {
		parts = append(parts, fmt.Sprintf("macd=%.4f/%.4f/%.4f", s.MACDMin, s.MACDMax, s.MACDAvg))
	}
	if all || indicatorInSet("rsi", indicators) {
		parts = append(parts, fmt.Sprintf("rsi7=%.2f/%.2f/%.2f rsi14=%.2f/%.2f/%.2f", s.RSI7Min, s.RSI7Max, s.RSI7Avg, s.RSI14Min, s.RSI14Max, s.RSI14Avg))
	}
	if all || indicatorInSet("atr", indicators) {
		parts = append(parts, fmt.Sprintf("atr14=%.4f/%.4f/%.4f", s.ATR14Min, s.ATR14Max, s.ATR14Avg))
	}
	if all || indicatorInSet("volume", indicators) {
		parts = append(parts, fmt.Sprintf("volume=%.2f/%.2f/%.2f", s.VolumeMin, s.VolumeMax, s.VolumeAvg))
	}
	if all || indicatorInSet("bollinger", indicators) {
		parts = append(parts, fmt.Sprintf("boll_u=%.4f/%.4f/%.4f boll_m=%.4f/%.4f/%.4f boll_l=%.4f/%.4f/%.4f",
			s.BOLLUpperMin, s.BOLLUpperMax, s.BOLLUpperAvg, s.BOLLMidMin, s.BOLLMidMax, s.BOLLMidAvg,
			s.BOLLLowerMin, s.BOLLLowerMax, s.BOLLLowerAvg))
	}
	if all || indicatorInSet("kdj", indicators) {
		parts = append(parts, fmt.Sprintf("kdj_k=%.2f/%.2f/%.2f kdj_d=%.2f/%.2f/%.2f kdj_j=%.2f/%.2f/%.2f",
			s.KDJKMin, s.KDJKMax, s.KDJKAvg, s.KDJDMin, s.KDJDMax, s.KDJDAvg, s.KDJJMin, s.KDJJMax, s.KDJJAvg))
	}
	sb.WriteString(strings.Join(parts, " "))
}

// FormatSummary 按币种输出「当前时刻」指标 + 各周期 10/6 根 K 线汇总（用于 User Prompt）
// cfg 为 nil 或 Timeframes/Indicators 为空时输出全部周期、全部指标
func FormatSummary(data *Data, cfg *AggregateConfig) string {
	var sb strings.Builder
	// 当前时刻（始终输出）
	sb.WriteString(fmt.Sprintf("当前时刻: price=%s ema20=%.4f ema50=%.4f macd=%.4f rsi7=%.2f rsi14=%.2f KDJ_K=%.2f KDJ_D=%.2f KDJ_J=%.2f\n",
		formatPriceWithDynamicPrecision(data.CurrentPrice), data.CurrentEMA20, data.CurrentEMA50, data.CurrentMACD,
		data.CurrentRSI7, data.CurrentRSI14, data.CurrentKDJK, data.CurrentKDJD, data.CurrentKDJJ))
	if data.OpenInterest != nil {
		sb.WriteString(fmt.Sprintf("Open Interest: %s | Funding Rate: %.2e\n", formatPriceWithDynamicPrecision(data.OpenInterest.Latest), data.FundingRate))
	}
	order := defaultTimeframes
	if cfg != nil && len(cfg.Timeframes) > 0 {
		order = cfg.Timeframes
	}
	indicators := []string(nil)
	if cfg != nil && len(cfg.Indicators) > 0 {
		indicators = cfg.Indicators
	}
	for _, tf := range order {
		agg, ok := data.TimeframeAggregates[tf]
		if !ok || agg == nil {
			continue
		}
		sb.WriteString(fmt.Sprintf("%s Timeframe: Last10[", tf))
		appendIndicatorStatsLine(&sb, &agg.Last10, indicators)
		sb.WriteString("] Last6[")
		appendIndicatorStatsLine(&sb, &agg.Last6, indicators)
		sb.WriteString("]\n")
	}
	return sb.String()
}

// formatPriceWithDynamicPrecision 根据价格区间动态选择精度
// 这样可以完美支持从超低价 meme coin (< 0.0001) 到 BTC/ETH 的所有币种
func formatPriceWithDynamicPrecision(price float64) string {
	switch {
	case price < 0.0001:
		// 超低价 meme coin: 1000SATS, 1000WHY, DOGS
		// 0.00002070 → "0.00002070" (8位小数)
		return fmt.Sprintf("%.8f", price)
	case price < 0.001:
		// 低价 meme coin: NEIRO, HMSTR, HOT, NOT
		// 0.00015060 → "0.000151" (6位小数)
		return fmt.Sprintf("%.6f", price)
	case price < 0.01:
		// 中低价币: PEPE, SHIB, MEME
		// 0.00556800 → "0.005568" (6位小数)
		return fmt.Sprintf("%.6f", price)
	case price < 1.0:
		// 低价币: ASTER, DOGE, ADA, TRX
		// 0.9954 → "0.9954" (4位小数)
		return fmt.Sprintf("%.4f", price)
	case price < 100:
		// 中价币: SOL, AVAX, LINK, MATIC
		// 23.4567 → "23.4567" (4位小数)
		return fmt.Sprintf("%.4f", price)
	default:
		// 高价币: BTC, ETH (节省 Token)
		// 45678.9123 → "45678.91" (2位小数)
		return fmt.Sprintf("%.2f", price)
	}
}

// formatFloatSlice 格式化float64切片为字符串（使用动态精度）
func formatFloatSlice(values []float64) string {
	strValues := make([]string, len(values))
	for i, v := range values {
		strValues[i] = formatPriceWithDynamicPrecision(v)
	}
	return "[" + strings.Join(strValues, ", ") + "]"
}

// Normalize 标准化symbol,确保是USDT交易对
func Normalize(symbol string) string {
	symbol = strings.ToUpper(symbol)
	if strings.HasSuffix(symbol, "USDT") {
		return symbol
	}
	return symbol + "USDT"
}

// parseFloat 解析float值
func parseFloat(v interface{}) (float64, error) {
	switch val := v.(type) {
	case string:
		return strconv.ParseFloat(val, 64)
	case float64:
		return val, nil
	case int:
		return float64(val), nil
	case int64:
		return float64(val), nil
	default:
		return 0, fmt.Errorf("unsupported type: %T", v)
	}
}

// getKlinesForTimeframe 获取指定周期的 K 线：3m/4h 用 WebSocket 缓存，其余用 REST API
func getKlinesForTimeframe(symbol string, timeframe string, limit int) ([]Kline, error) {
	symbol = Normalize(symbol)
	if timeframe == "3m" || timeframe == "4h" {
		return WSMonitorCli.GetCurrentKlines(symbol, timeframe)
	}
	apiClient := NewAPIClient()
	return apiClient.GetKlines(symbol, timeframe, limit)
}

// minMaxAvg 对 float64 切片计算最小值、最大值、平均值（忽略 NaN/0 的语义，仅做数值）
func minMaxAvg(s []float64) (minV, maxV, avgV float64) {
	if len(s) == 0 {
		return 0, 0, 0
	}
	minV = s[0]
	maxV = s[0]
	sum := 0.0
	for _, v := range s {
		if v < minV {
			minV = v
		}
		if v > maxV {
			maxV = v
		}
		sum += v
	}
	return minV, maxV, sum / float64(len(s))
}

// defaultTimeframes 未配置时使用的周期列表（与前端可选一致）
var defaultTimeframes = []string{"1m", "3m", "5m", "15m", "30m", "1h", "4h", "1d"}

// validTimeframeSet Binance 支持的 K 线周期
var validTimeframeSet = map[string]bool{
	"1m": true, "3m": true, "5m": true, "15m": true, "30m": true,
	"1h": true, "2h": true, "4h": true, "6h": true, "12h": true, "1d": true,
}

// ValidateTimeframe 校验周期是否支持
func ValidateTimeframe(tf string) bool { return validTimeframeSet[tf] }

// indicatorInSet 判断指标 id 是否在选中列表中；空列表表示全部选中
func indicatorInSet(id string, list []string) bool {
	if len(list) == 0 {
		return true
	}
	for _, x := range list {
		if x == id {
			return true
		}
	}
	return false
}

// computeIndicatorStatsForLastN 对 klines 最后 n 根 K 线计算各指标序列，再汇总为 min/max/avg
// indicators 为空时计算全部；否则只计算列表中的指标：ema, macd, rsi, atr, volume, bollinger, kdj
func computeIndicatorStatsForLastN(klines []Kline, n int, indicators []string) IndicatorStats {
	var s IndicatorStats
	if len(klines) < n {
		n = len(klines)
	}
	if n == 0 {
		return s
	}
	start := len(klines) - n

	wantEMA := indicatorInSet("ema", indicators)
	wantMACD := indicatorInSet("macd", indicators)
	wantRSI := indicatorInSet("rsi", indicators)
	wantATR := indicatorInSet("atr", indicators)
	wantVolume := indicatorInSet("volume", indicators)
	wantBOLL := indicatorInSet("bollinger", indicators)
	wantKDJ := indicatorInSet("kdj", indicators)

	prices := make([]float64, 0, n)
	ema20s := make([]float64, 0, n)
	macds := make([]float64, 0, n)
	rsi7s := make([]float64, 0, n)
	rsi14s := make([]float64, 0, n)
	atr14s := make([]float64, 0, n)
	vols := make([]float64, 0, n)
	bollU := make([]float64, 0, n)
	bollM := make([]float64, 0, n)
	bollL := make([]float64, 0, n)
	kdjKs := make([]float64, 0, n)
	kdjDs := make([]float64, 0, n)
	kdjJs := make([]float64, 0, n)

	for i := start; i < len(klines); i++ {
		sub := klines[:i+1]
		prices = append(prices, klines[i].Close)
		if wantVolume {
			vols = append(vols, klines[i].Volume)
		}
		if wantEMA && len(sub) >= 20 {
			ema20s = append(ema20s, calculateEMA(sub, 20))
		}
		if wantMACD && len(sub) >= 26 {
			macds = append(macds, calculateMACD(sub))
		}
		if wantRSI {
			if len(sub) >= 8 {
				rsi7s = append(rsi7s, calculateRSI(sub, 7))
			}
			if len(sub) >= 15 {
				rsi14s = append(rsi14s, calculateRSI(sub, 14))
			}
		}
		if wantATR && len(sub) >= 15 {
			atr14s = append(atr14s, calculateATR(sub, 14))
		}
		if wantBOLL && len(sub) >= 20 {
			u, m, l := calculateBOLL(sub, 20, 2)
			bollU = append(bollU, u)
			bollM = append(bollM, m)
			bollL = append(bollL, l)
		}
		if wantKDJ {
			k, d, j := calculateKDJ(sub)
			kdjKs = append(kdjKs, k)
			kdjDs = append(kdjDs, d)
			kdjJs = append(kdjJs, j)
		}
	}

	s.PriceMin, s.PriceMax, s.PriceAvg = minMaxAvg(prices)
	if wantVolume && len(vols) > 0 {
		s.VolumeMin, s.VolumeMax, s.VolumeAvg = minMaxAvg(vols)
	}
	if wantEMA && len(ema20s) > 0 {
		s.EMA20Min, s.EMA20Max, s.EMA20Avg = minMaxAvg(ema20s)
	}
	if wantMACD && len(macds) > 0 {
		s.MACDMin, s.MACDMax, s.MACDAvg = minMaxAvg(macds)
	}
	if wantRSI {
		if len(rsi7s) > 0 {
			s.RSI7Min, s.RSI7Max, s.RSI7Avg = minMaxAvg(rsi7s)
		}
		if len(rsi14s) > 0 {
			s.RSI14Min, s.RSI14Max, s.RSI14Avg = minMaxAvg(rsi14s)
		}
	}
	if wantATR && len(atr14s) > 0 {
		s.ATR14Min, s.ATR14Max, s.ATR14Avg = minMaxAvg(atr14s)
	}
	if wantBOLL && len(bollU) > 0 {
		s.BOLLUpperMin, s.BOLLUpperMax, s.BOLLUpperAvg = minMaxAvg(bollU)
		s.BOLLMidMin, s.BOLLMidMax, s.BOLLMidAvg = minMaxAvg(bollM)
		s.BOLLLowerMin, s.BOLLLowerMax, s.BOLLLowerAvg = minMaxAvg(bollL)
	}
	if wantKDJ && len(kdjKs) > 0 {
		s.KDJKMin, s.KDJKMax, s.KDJKAvg = minMaxAvg(kdjKs)
		s.KDJDMin, s.KDJDMax, s.KDJDAvg = minMaxAvg(kdjDs)
		s.KDJJMin, s.KDJJMax, s.KDJJAvg = minMaxAvg(kdjJs)
	}
	return s
}

// computeTimeframeAggregate 根据 klines 计算该周期的 10 根 / 6 根 K 线汇总；indicators 为空表示全部
func computeTimeframeAggregate(klines []Kline, timeframe string, indicators []string) *TimeframeAggregate {
	if len(klines) < 6 {
		return nil
	}
	return &TimeframeAggregate{
		Timeframe: timeframe,
		Last10:    computeIndicatorStatsForLastN(klines, 10, indicators),
		Last6:     computeIndicatorStatsForLastN(klines, 6, indicators),
	}
}

// GetWithTimeframeAggregates 获取市场数据并填充各周期的 10/6 根 K 线汇总
// cfg 为 nil 或 Timeframes/Indicators 为空时使用默认：全部周期、全部指标
func GetWithTimeframeAggregates(symbol string, cfg *AggregateConfig) (*Data, error) {
	data, err := Get(symbol)
	if err != nil {
		return nil, err
	}
	timeframes := defaultTimeframes
	indicators := []string(nil)
	if cfg != nil {
		if len(cfg.Timeframes) > 0 {
			timeframes = cfg.Timeframes
		}
		if len(cfg.Indicators) > 0 {
			indicators = cfg.Indicators
		}
	}
	const klineLimit = 50
	data.TimeframeAggregates = make(map[string]*TimeframeAggregate)
	for _, tf := range timeframes {
		if !ValidateTimeframe(tf) {
			continue
		}
		limit := klineLimit
		if cfg != nil && cfg.DataPoints != nil && cfg.DataPoints[tf] > 0 {
			if cfg.DataPoints[tf] > limit {
				limit = cfg.DataPoints[tf]
			}
		}
		klines, err := getKlinesForTimeframe(symbol, tf, limit)
		if err != nil || len(klines) < 6 {
			continue
		}
		if agg := computeTimeframeAggregate(klines, tf, indicators); agg != nil {
			data.TimeframeAggregates[tf] = agg
		}
	}
	return data, nil
}

// isStaleData detects stale data (consecutive price freeze)
// Fix DOGEUSDT-style issue: consecutive N periods with completely unchanged prices indicate data source anomaly
func isStaleData(klines []Kline, symbol string) bool {
	if len(klines) < 5 {
		return false // Insufficient data to determine
	}

	// Detection threshold: 5 consecutive 3-minute periods with unchanged price (15 minutes without fluctuation)
	const stalePriceThreshold = 5
	const priceTolerancePct = 0.0001 // 0.01% fluctuation tolerance (avoid false positives)

	// Take the last stalePriceThreshold K-lines
	recentKlines := klines[len(klines)-stalePriceThreshold:]
	firstPrice := recentKlines[0].Close

	// Check if all prices are within tolerance
	for i := 1; i < len(recentKlines); i++ {
		priceDiff := math.Abs(recentKlines[i].Close-firstPrice) / firstPrice
		if priceDiff > priceTolerancePct {
			return false // Price fluctuation exists, data is normal
		}
	}

	// Additional check: MACD and volume
	// If price is unchanged but MACD/volume shows normal fluctuation, it might be a real market situation (extremely low volatility)
	// Check if volume is also 0 (data completely frozen)
	allVolumeZero := true
	for _, k := range recentKlines {
		if k.Volume > 0 {
			allVolumeZero = false
			break
		}
	}

	if allVolumeZero {
		log.Printf("⚠️  %s stale data confirmed: price freeze + zero volume", symbol)
		return true
	}

	// Price frozen but has volume: might be extremely low volatility market, allow but log warning
	log.Printf("⚠️  %s detected extreme price stability (no fluctuation for %d consecutive periods), but volume is normal", symbol, stalePriceThreshold)
	return false
}
