package market

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// MarketRegimeName is the final regime output.
// Must be one of:
// 向上突破 / 向下突破 / 单边上行 / 单边下行 / 震荡 / 趋势不明确
type MarketRegimeName string

const (
	RegimeUpBreakout   MarketRegimeName = "向上突破"
	RegimeDownBreakout MarketRegimeName = "向下突破"
	RegimeUpTrend      MarketRegimeName = "单边上行"
	RegimeDownTrend    MarketRegimeName = "单边下行"
	RegimeRange        MarketRegimeName = "震荡"
	RegimeUnclear      MarketRegimeName = "趋势不明确"
)

type MarketRegimeResult struct {
	Regime MarketRegimeName `json:"regime"`
	// Score is how many sub-conditions were satisfied under the selected step.
	Score int `json:"score"`
	// Hits are the names of satisfied sub-conditions (e.g. 破上/量增/势强/均线/轨扩).
	Hits []string `json:"hits,omitempty"`
	// Debug holds key numeric facts (kept short for prompt).
	Debug map[string]float64 `json:"debug,omitempty"`
	// Missing is filled when required inputs are insufficient.
	Missing []string `json:"missing,omitempty"`
}

// ComputeMarketRegime computes the market regime for ONE symbol from market.Data.
//
// Data requirements (timeframe series):
// - 1h: klines (>= 30 recommended), EMA20/EMA50, MACD values, ATR14 values, BOLL upper/mid/lower
// - 15m: klines (>= 4) for V = sum(vol[0..3]) on latest 4 bars (include current bar)
//
// Note:
// - This implementation uses existing series available in TimeframeData. If series are too short
//   (e.g. not enough BOLL/ATR history), it will degrade to "趋势不明确" with Missing reasons.
func ComputeMarketRegime(d *Data) MarketRegimeResult {
	if d == nil || d.TimeframeData == nil {
		return MarketRegimeResult{Regime: RegimeUnclear, Missing: []string{"TimeframeData is nil"}}
	}
	h1 := d.TimeframeData["1h"]
	m15 := d.TimeframeData["15m"]
	if h1 == nil {
		return MarketRegimeResult{Regime: RegimeUnclear, Missing: []string{"missing 1h timeframe data"}}
	}
	if m15 == nil {
		return MarketRegimeResult{Regime: RegimeUnclear, Missing: []string{"missing 15m timeframe data"}}
	}

	missing := make([]string, 0, 6)

	// Current price P = latest 1h close (as per requirements).
	p, ok := lastClose(h1)
	if !ok {
		missing = append(missing, "1h klines empty")
	}

	// 1h bands
	up, okUp := lastFloat(h1.BOLLUpper)
	mid, okMid := lastFloat(h1.BOLLMiddle)
	low, okLow := lastFloat(h1.BOLLLower)
	if !okUp || !okMid || !okLow {
		missing = append(missing, "1h BOLL (upper/middle/lower) insufficient")
	}

	atrNow, okATR := lastFloat(h1.ATR14Values)
	if !okATR || atrNow <= 0 {
		missing = append(missing, "1h ATR14 insufficient")
	}

	ema20Now, okE20 := lastFloat(h1.EMA20Values)
	ema50Now, okE50 := lastFloat(h1.EMA50Values)
	if !okE20 || !okE50 {
		missing = append(missing, "1h EMA20/EMA50 insufficient")
	}

	// Volume definitions
	v, okV := sumLastNVolumes(m15, 4) // V: 15m 最近4根成交量和（含当前K）
	if !okV {
		missing = append(missing, "15m last 4 volumes insufficient")
	}
	v11, okV11 := avgLastNVolumes(h1, 11) // V11: 1h 最近11根成交量均值（含当前K）
	if !okV11 || v11 <= 0 {
		missing = append(missing, "1h V11 (avg volume last 11) insufficient")
	}

	// For "当前成交量是最近11根里最大的两根之一": use current 1h volume (not 15m).
	curVol1h, okCurVol1h := lastVolume(h1)
	if !okCurVol1h {
		missing = append(missing, "1h current volume missing")
	}

	// Bandwidth series for 轨扩 / 缩
	bandNow, bandOk := bandWidthLast(h1)
	if !bandOk {
		missing = append(missing, "1h band width insufficient")
	}

	debug := map[string]float64{
		"P":       p,
		"UP":      up,
		"MID":     mid,
		"LOW":     low,
		"ATR14":   atrNow,
		"EMA20":   ema20Now,
		"EMA50":   ema50Now,
		"V":       v,
		"V11":     v11,
		"V1H":     curVol1h,
		"Band":    bandNow,
		"BandPct": safeDiv(bandNow, math.Max(p, 1e-12)),
	}

	// If core inputs missing, still try, but final may degrade.
	_ = debug

	// Step 1: 向上突破 (>=3/5)
	upBreakHits := make([]string, 0, 5)
	if condBreakUp(p, up, atrNow) {
		upBreakHits = append(upBreakHits, "破上")
	}
	if condVolumeSpike(curVol1h, v11) && okCurVol1h && isTop2Volume(h1, 11) {
		upBreakHits = append(upBreakHits, "量增")
	}
	if condStrongCandle(h1, atrNow, true) {
		upBreakHits = append(upBreakHits, "势强")
	}
	if okE20 && okE50 && p > ema20Now && ema20Now > ema50Now {
		upBreakHits = append(upBreakHits, "均线")
	}
	if condBandExpand(h1) {
		upBreakHits = append(upBreakHits, "轨扩")
	}
	if len(upBreakHits) >= 3 {
		return MarketRegimeResult{Regime: RegimeUpBreakout, Score: len(upBreakHits), Hits: upBreakHits, Debug: debug, Missing: missingIfAny(missing)}
	}

	// Step 2: 向下突破 (>=3/5)
	downBreakHits := make([]string, 0, 5)
	if condBreakDown(p, low, atrNow) {
		downBreakHits = append(downBreakHits, "破下")
	}
	if condVolumeSpike(curVol1h, v11) && okCurVol1h && isTop2Volume(h1, 11) {
		downBreakHits = append(downBreakHits, "量增")
	}
	if condStrongCandle(h1, atrNow, false) {
		downBreakHits = append(downBreakHits, "势强")
	}
	if okE20 && okE50 && p < ema20Now && ema20Now < ema50Now {
		downBreakHits = append(downBreakHits, "均线")
	}
	if condBandExpand(h1) {
		downBreakHits = append(downBreakHits, "轨扩")
	}
	if len(downBreakHits) >= 3 {
		return MarketRegimeResult{Regime: RegimeDownBreakout, Score: len(downBreakHits), Hits: downBreakHits, Debug: debug, Missing: missingIfAny(missing)}
	}

	// Step 3: 单边上行 (>=4/5)
	upTrendHits := make([]string, 0, 5)
	if condMAStackAndCloses(h1, true) {
		upTrendHits = append(upTrendHits, "多")
	}
	if condMACDMomentum(h1, true) {
		upTrendHits = append(upTrendHits, "强")
	}
	if condWaveBollPrice(h1, true) {
		upTrendHits = append(upTrendHits, "波")
	}
	if condVolumeAsymmetry(h1, v11, true) {
		upTrendHits = append(upTrendHits, "量")
	}
	if condStructureAndSupport(h1, true) {
		upTrendHits = append(upTrendHits, "构")
	}
	if len(upTrendHits) >= 4 {
		return MarketRegimeResult{Regime: RegimeUpTrend, Score: len(upTrendHits), Hits: upTrendHits, Debug: debug, Missing: missingIfAny(missing)}
	}

	// Step 4: 单边下行 (>=4/5)
	downTrendHits := make([]string, 0, 5)
	if condMAStackAndCloses(h1, false) {
		downTrendHits = append(downTrendHits, "空")
	}
	if condMACDMomentum(h1, false) {
		downTrendHits = append(downTrendHits, "弱")
	}
	if condWaveBollPrice(h1, false) {
		downTrendHits = append(downTrendHits, "波")
	}
	if condVolumeAsymmetry(h1, v11, false) {
		downTrendHits = append(downTrendHits, "量")
	}
	if condStructureAndSupport(h1, false) {
		downTrendHits = append(downTrendHits, "构")
	}
	if len(downTrendHits) >= 4 {
		return MarketRegimeResult{Regime: RegimeDownTrend, Score: len(downTrendHits), Hits: downTrendHits, Debug: debug, Missing: missingIfAny(missing)}
	}

	// Step 5: 震荡 (>=3/5)
	rangeHits := make([]string, 0, 5)
	if condEMAChop(h1, atrNow) {
		rangeHits = append(rangeHits, "缠")
	}
	if condMACDChop(h1) {
		rangeHits = append(rangeHits, "无")
	}
	if condVolatilityContraction(h1) {
		rangeHits = append(rangeHits, "缩")
	}
	if condLowVolumeNoSpike(h1, v11) {
		rangeHits = append(rangeHits, "量")
	}
	if condMessyFalseBreak(h1, atrNow) {
		rangeHits = append(rangeHits, "乱")
	}
	if len(rangeHits) >= 3 {
		return MarketRegimeResult{Regime: RegimeRange, Score: len(rangeHits), Hits: rangeHits, Debug: debug, Missing: missingIfAny(missing)}
	}

	return MarketRegimeResult{Regime: RegimeUnclear, Score: 0, Hits: nil, Debug: debug, Missing: missingIfAny(missing)}
}

func missingIfAny(m []string) []string {
	if len(m) == 0 {
		return nil
	}
	// Dedup
	set := map[string]struct{}{}
	out := make([]string, 0, len(m))
	for _, s := range m {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := set[s]; ok {
			continue
		}
		set[s] = struct{}{}
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func lastFloat(xs []float64) (float64, bool) {
	if len(xs) == 0 {
		return 0, false
	}
	return xs[len(xs)-1], true
}

func bars(tf *TimeframeSeriesData) []KlineBar {
	if tf == nil {
		return nil
	}
	if len(tf.KlinesFull) > 0 {
		return tf.KlinesFull
	}
	return tf.Klines
}

func lastClose(tf *TimeframeSeriesData) (float64, bool) {
	ks := bars(tf)
	if len(ks) == 0 {
		return 0, false
	}
	return ks[len(ks)-1].Close, true
}

func lastVolume(tf *TimeframeSeriesData) (float64, bool) {
	ks := bars(tf)
	if len(ks) == 0 {
		return 0, false
	}
	return ks[len(ks)-1].Volume, true
}

func sumLastNVolumes(tf *TimeframeSeriesData, n int) (float64, bool) {
	ks := bars(tf)
	if n <= 0 || len(ks) < n {
		return 0, false
	}
	sum := 0.0
	for i := len(ks) - n; i < len(ks); i++ {
		sum += ks[i].Volume
	}
	return sum, true
}

func avgLastNVolumes(tf *TimeframeSeriesData, n int) (float64, bool) {
	ks := bars(tf)
	if n <= 0 || len(ks) < n {
		return 0, false
	}
	sum := 0.0
	for i := len(ks) - n; i < len(ks); i++ {
		sum += ks[i].Volume
	}
	return sum / float64(n), true
}

func isTop2Volume(tf *TimeframeSeriesData, n int) bool {
	ks := bars(tf)
	if n <= 0 || len(ks) < n {
		return false
	}
	cur := ks[len(ks)-1].Volume
	vols := make([]float64, 0, n)
	for i := len(ks) - n; i < len(ks); i++ {
		vols = append(vols, ks[i].Volume)
	}
	sort.Float64s(vols) // ascending
	if len(vols) < 2 {
		return false
	}
	// top2 means >= second largest (the 2nd from end).
	return cur >= vols[len(vols)-2]
}

func bandWidthLast(tf *TimeframeSeriesData) (float64, bool) {
	if tf == nil {
		return 0, false
	}
	up, okUp := lastFloat(tf.BOLLUpper)
	low, okLow := lastFloat(tf.BOLLLower)
	if !okUp || !okLow {
		return 0, false
	}
	return up - low, true
}

func condBreakUp(p, up, atr float64) bool {
	if atr <= 0 {
		return false
	}
	if p <= up {
		return false
	}
	return (p - up) >= 1.0*atr
}

func condBreakDown(p, low, atr float64) bool {
	if atr <= 0 {
		return false
	}
	if p >= low {
		return false
	}
	return (low - p) >= 1.0*atr
}

func condVolumeSpike(vCur, v11 float64) bool {
	if v11 <= 0 {
		return false
	}
	return vCur >= 1.8*v11
}

func condStrongCandle(tf *TimeframeSeriesData, atr float64, bullish bool) bool {
	ks := bars(tf)
	if len(ks) == 0 || atr <= 0 {
		return false
	}
	k := ks[len(ks)-1]
	if bullish {
		if !(k.Close > k.Open) {
			return false
		}
		body := k.Close - k.Open
		return body >= 1.5*atr
	}
	if !(k.Close < k.Open) {
		return false
	}
	body := k.Open - k.Close
	return body >= 1.5*atr
}

func medianLastN(xs []float64, n int) (float64, bool) {
	if n <= 0 || len(xs) < n {
		return 0, false
	}
	tmp := make([]float64, n)
	copy(tmp, xs[len(xs)-n:])
	sort.Float64s(tmp)
	// n is expected odd (11), median is middle at index n/2
	return tmp[n/2], true
}

func quantile20Last11(xs []float64) (float64, bool) {
	if len(xs) < 11 {
		return 0, false
	}
	tmp := make([]float64, 11)
	copy(tmp, xs[len(xs)-11:])
	sort.Float64s(tmp)
	// 20% quantile for 11 items: take 3rd smallest (index 2)
	return tmp[2], true
}

func meanAbsLastN(xs []float64, n int) (float64, bool) {
	if n <= 0 || len(xs) < n {
		return 0, false
	}
	sum := 0.0
	for i := len(xs) - n; i < len(xs); i++ {
		sum += math.Abs(xs[i])
	}
	return sum / float64(n), true
}

func condBandExpand(tf *TimeframeSeriesData) bool {
	if tf == nil {
		return false
	}
	// Need Band[0], Band[1], Band[2] and median over last 11.
	if len(tf.BOLLUpper) < 11 || len(tf.BOLLLower) < 11 {
		return false
	}
	bands := make([]float64, 0, 11)
	for i := 0; i < 11; i++ {
		idx := len(tf.BOLLUpper) - 11 + i
		bands = append(bands, tf.BOLLUpper[idx]-tf.BOLLLower[idx])
	}
	b0 := bands[10]
	b1 := bands[9]
	b2 := bands[8]
	if !(b0 > b1 && b1 > b2) {
		return false
	}
	tmp := make([]float64, 11)
	copy(tmp, bands)
	sort.Float64s(tmp)
	med := tmp[5] // 6th smallest
	return b0 > med
}

func condMAStackAndCloses(tf *TimeframeSeriesData, bullish bool) bool {
	// 条件 [多]/[空]:
	// - 当前 P 与 EMA20/EMA50 多/空头排列
	// - 最近11根收盘价在 EMA20 上/下方次数 >=7
	ks := bars(tf)
	if len(ks) == 0 {
		return false
	}
	// Build EMA20/EMA50 per bar using existing arrays where possible; fallback to recompute from klines.
	// To keep correctness and alignment, recompute per bar EMA20/EMA50 for the last 11 bars.
	if len(ks) < 60 { // need enough to compute EMA50 reliably; still try but may fail.
		// proceed; calculateEMA will return 0 if insufficient, making condition fail.
	}
	n := 11
	if len(ks) < n {
		return false
	}
	// Current stack
	ema20Now := emaAt(tf, 20, len(ks)-1)
	ema50Now := emaAt(tf, 50, len(ks)-1)
	pNow := ks[len(ks)-1].Close
	if bullish {
		if !(pNow > ema20Now && ema20Now > ema50Now) {
			return false
		}
	} else {
		if !(pNow < ema20Now && ema20Now < ema50Now) {
			return false
		}
	}

	// Count closes above/below EMA20 in last 11 bars.
	cnt := 0
	for i := len(ks) - n; i < len(ks); i++ {
		ema20 := emaAt(tf, 20, i)
		c := ks[i].Close
		if bullish {
			if c > ema20 {
				cnt++
			}
		} else {
			if c < ema20 {
				cnt++
			}
		}
	}
	return cnt >= 7
}

func emaAt(tf *TimeframeSeriesData, period int, idx int) float64 {
	// Recompute EMA(period) at klines[idx] using calculateEMA on prefix, ensuring same logic as market.calculateEMA.
	// If insufficient data, returns 0.
	ks := bars(tf)
	if idx < 0 || idx >= len(ks) {
		return 0
	}
	kl := make([]Kline, 0, idx+1)
	for i := 0; i <= idx; i++ {
		k := ks[i]
		kl = append(kl, Kline{OpenTime: k.Time, Open: k.Open, High: k.High, Low: k.Low, Close: k.Close, Volume: k.Volume})
	}
	return calculateEMA(kl, period)
}

func atrAt(tf *TimeframeSeriesData, idx int) float64 {
	// Prefer provided ATR series if aligned; otherwise recompute via Wilder series.
	ks := bars(tf)
	if idx < 0 || idx >= len(ks) {
		return 0
	}
	// If ATR14Values length equals klines length (not guaranteed), use it.
	if len(tf.ATR14Values) == len(ks) {
		return tf.ATR14Values[idx]
	}
	// Fallback recompute series.
	kl := make([]Kline, 0, len(ks))
	for i := 0; i < len(ks); i++ {
		k := ks[i]
		kl = append(kl, Kline{OpenTime: k.Time, Open: k.Open, High: k.High, Low: k.Low, Close: k.Close, Volume: k.Volume})
	}
	series := calculateATRSeries(kl, 14)
	if idx < len(series) {
		return series[idx]
	}
	return 0
}

func macdAt(tf *TimeframeSeriesData, idx int) float64 {
	// Use provided MACDValues if aligned; otherwise recompute MACD(EMA12-EMA26) at idx.
	ks := bars(tf)
	if idx < 0 || idx >= len(ks) {
		return 0
	}
	if len(tf.MACDValues) == len(ks) {
		return tf.MACDValues[idx]
	}
	kl := make([]Kline, 0, idx+1)
	for i := 0; i <= idx; i++ {
		k := ks[i]
		kl = append(kl, Kline{OpenTime: k.Time, Open: k.Open, High: k.High, Low: k.Low, Close: k.Close, Volume: k.Volume})
	}
	return calculateMACD(kl)
}

func bollAt(tf *TimeframeSeriesData, idx int) (up, mid, low float64, ok bool) {
	ks := bars(tf)
	if idx < 0 || idx >= len(ks) {
		return 0, 0, 0, false
	}
	// Provided BOLL arrays are not necessarily aligned; recompute for correctness.
	kl := make([]Kline, 0, idx+1)
	for i := 0; i <= idx; i++ {
		k := ks[i]
		kl = append(kl, Kline{OpenTime: k.Time, Open: k.Open, High: k.High, Low: k.Low, Close: k.Close, Volume: k.Volume})
	}
	u, m, l := calculateBOLL(kl, 20, 2.0)
	if u == 0 && m == 0 && l == 0 {
		return 0, 0, 0, false
	}
	return u, m, l, true
}

func condMACDMomentum(tf *TimeframeSeriesData, bullish bool) bool {
	// [强]/[弱]:
	// - 当前 MACD 柱值 >0 / <0
	// - 最近5根中 至少3次递增(多)/递减(空)：后一根 > 前一根 / < 前一根
	ks := bars(tf)
	if len(ks) < 30 {
		// Need enough for MACD(26).
		return false
	}
	lastIdx := len(ks) - 1
	cur := macdAt(tf, lastIdx)
	if bullish {
		if !(cur > 0) {
			return false
		}
	} else {
		if !(cur < 0) {
			return false
		}
	}
	inc := 0
	// last 5 bars: indices [last-4..last]
	start := lastIdx - 4
	if start < 1 {
		return false
	}
	for i := start + 1; i <= lastIdx; i++ {
		prev := macdAt(tf, i-1)
		now := macdAt(tf, i)
		if bullish {
			if now > prev {
				inc++
			}
		} else {
			if now < prev {
				inc++
			}
		}
	}
	return inc >= 3
}

func condWaveBollPrice(tf *TimeframeSeriesData, bullish bool) bool {
	// [波]:
	// - 当前ATR14 > 最近11根ATR14中位数
	// - UP上升(本期UP>上期UP) / LOW下降(本期LOW<上期LOW)
	// - 价格在UP与MID之间(多) / LOW与MID之间(空)
	ks := bars(tf)
	if len(ks) < 25 {
		return false
	}
	n := 11
	if len(ks) < n+1 {
		return false
	}
	last := len(ks) - 1
	atrNow := atrAt(tf, last)
	if atrNow <= 0 {
		return false
	}
	atrs := make([]float64, 0, n)
	for i := last - (n - 1); i <= last; i++ {
		atrs = append(atrs, atrAt(tf, i))
	}
	tmp := make([]float64, len(atrs))
	copy(tmp, atrs)
	sort.Float64s(tmp)
	med := tmp[len(tmp)/2] // 6th when 11
	if !(atrNow > med) {
		return false
	}

	up0, mid0, low0, ok0 := bollAt(tf, last)
	up1, _, low1, ok1 := bollAt(tf, last-1)
	if !ok0 || !ok1 {
		return false
	}

	ks := bars(tf)
	p := ks[last].Close
	if bullish {
		if !(up0 > up1) {
			return false
		}
		if !(mid0 < p && p < up0) {
			return false
		}
	} else {
		if !(low0 < low1) { // falling lower band
			return false
		}
		if !(low0 < p && p < mid0) {
			return false
		}
	}
	return true
}

func condVolumeAsymmetry(tf *TimeframeSeriesData, v11 float64, bullish bool) bool {
	// [量]:
	// A: 当前K线阳/阴线 且 V > 1.5*V11
	// OR
	// B: 最近5根阳线平均量 > 1.2*V11 且 最近5根阴线平均量 < 0.8*V11（对应数量为0则不成立）
	ks := bars(tf)
	if len(ks) < 6 || v11 <= 0 {
		return false
	}
	last := ks[len(ks)-1]
	if bullish {
		if last.Close > last.Open && last.Volume > 1.5*v11 {
			return true
		}
	} else {
		if last.Close < last.Open && last.Volume > 1.5*v11 {
			return true
		}
	}

	// Condition B: use last 5 bars (include current).
	n := 5
	start := len(ks) - n
	if start < 0 {
		return false
	}
	sumBull, cntBull := 0.0, 0
	sumBear, cntBear := 0.0, 0
	for i := start; i < len(ks); i++ {
		k := ks[i]
		if k.Close > k.Open {
			sumBull += k.Volume
			cntBull++
		} else if k.Close < k.Open {
			sumBear += k.Volume
			cntBear++
		}
	}
	if cntBull == 0 || cntBear == 0 {
		return false
	}
	avgBull := sumBull / float64(cntBull)
	avgBear := sumBear / float64(cntBear)
	if bullish {
		return avgBull > 1.2*v11 && avgBear < 0.8*v11
	}
	return avgBear > 1.2*v11 && avgBull < 0.8*v11
}

func condStructureAndSupport(tf *TimeframeSeriesData, bullish bool) bool {
	// [构]:
	// - 最近5个波段低点/高点 逐步抬高/降低（用 fractal swing points 近似）
	// - 支撑/压力：价格从未跌破/突破EMA50，或短暂(1-2根)后快速回到EMA50上/下方
	ks := bars(tf)
	if len(ks) < 60 {
		// Ensure EMA50 meaningful and enough swings.
		return false
	}
	if bullish {
		lows := lastNSwingLows(tf, 5)
		if len(lows) < 5 || !strictlyIncreasing(lows) {
			return false
		}
		if !supportAroundEMA50(tf, true) {
			return false
		}
		return true
	}
	highs := lastNSwingHighs(tf, 5)
	if len(highs) < 5 || !strictlyDecreasing(highs) {
		return false
	}
	if !supportAroundEMA50(tf, false) {
		return false
	}
	return true
}

func lastNSwingLows(tf *TimeframeSeriesData, n int) []float64 {
	out := make([]float64, 0, n)
	ks := bars(tf)
	if len(ks) < 3 {
		return out
	}
	for i := 1; i < len(ks)-1; i++ {
		l0 := ks[i].Low
		if l0 < ks[i-1].Low && l0 < ks[i+1].Low {
			out = append(out, l0)
		}
	}
	if len(out) <= n {
		return out
	}
	return out[len(out)-n:]
}

func lastNSwingHighs(tf *TimeframeSeriesData, n int) []float64 {
	out := make([]float64, 0, n)
	ks := bars(tf)
	if len(ks) < 3 {
		return out
	}
	for i := 1; i < len(ks)-1; i++ {
		h0 := ks[i].High
		if h0 > ks[i-1].High && h0 > ks[i+1].High {
			out = append(out, h0)
		}
	}
	if len(out) <= n {
		return out
	}
	return out[len(out)-n:]
}

func strictlyIncreasing(xs []float64) bool {
	for i := 1; i < len(xs); i++ {
		if !(xs[i] > xs[i-1]) {
			return false
		}
	}
	return true
}

func strictlyDecreasing(xs []float64) bool {
	for i := 1; i < len(xs); i++ {
		if !(xs[i] < xs[i-1]) {
			return false
		}
	}
	return true
}

func supportAroundEMA50(tf *TimeframeSeriesData, bullish bool) bool {
	// Approx:
	// - compute EMA50 for each bar in last 11 bars, check excursions beyond EMA50.
	// - allow max 2 consecutive bars violating, and require latest close back to correct side.
	ks := bars(tf)
	if len(ks) < 60 {
		return false
	}
	n := 11
	last := len(ks) - 1
	maxConsec := 0
	curConsec := 0
	for i := last - (n - 1); i <= last; i++ {
		ema50 := emaAt(tf, 50, i)
		c := ks[i].Close
		viol := false
		if bullish {
			viol = c < ema50
		} else {
			viol = c > ema50
		}
		if viol {
			curConsec++
			if curConsec > maxConsec {
				maxConsec = curConsec
			}
		} else {
			curConsec = 0
		}
	}
	latestEMA50 := emaAt(tf, 50, last)
	latestClose := ks[last].Close
	if bullish {
		if !(latestClose > latestEMA50) {
			return false
		}
	} else {
		if !(latestClose < latestEMA50) {
			return false
		}
	}
	return maxConsec <= 2
}

func condEMAChop(tf *TimeframeSeriesData, atrNow float64) bool {
	// [缠] A: 最近11根 EMA20/EMA50 交叉次数>=2
	// OR B: |EMA20-EMA50| < 0.5 * 当前ATR14
	ks := bars(tf)
	if len(ks) < 60 {
		// need EMA50 stability
		return false
	}
	n := 11
	if len(ks) < n+1 {
		return false
	}
	last := len(ks) - 1
	// Condition B
	ema20 := emaAt(tf, 20, last)
	ema50 := emaAt(tf, 50, last)
	atr := atrAt(tf, last)
	if atrNow > 0 {
		atr = atrNow
	}
	if atr > 0 && math.Abs(ema20-ema50) < 0.5*atr {
		return true
	}
	// Condition A: count crossings in last 11 bars (compare sign of diff between consecutive bars)
	cross := 0
	prevDiff := emaAt(tf, 20, last-n) - emaAt(tf, 50, last-n)
	for i := last - (n - 1); i <= last; i++ {
		diff := emaAt(tf, 20, i) - emaAt(tf, 50, i)
		if diff*prevDiff < 0 {
			cross++
		}
		prevDiff = diff
	}
	return cross >= 2
}

func condMACDChop(tf *TimeframeSeriesData) bool {
	// [无]:
	// - 最近11根 MACD hist 负转正/正转负次数>=2 (hist[i]*hist[i-1]<0)
	// - |当前hist| < 最近11根 |hist| 平均值
	ks := bars(tf)
	if len(ks) < 30 {
		return false
	}
	n := 11
	if len(ks) < n+1 {
		return false
	}
	last := len(ks) - 1
	cross := 0
	prev := macdAt(tf, last-n)
	absSum := 0.0
	for i := last - (n - 1); i <= last; i++ {
		now := macdAt(tf, i)
		if now*prev < 0 {
			cross++
		}
		absSum += math.Abs(now)
		prev = now
	}
	avgAbs := absSum / float64(n)
	curAbs := math.Abs(macdAt(tf, last))
	return cross >= 2 && curAbs < avgAbs
}

func condVolatilityContraction(tf *TimeframeSeriesData) bool {
	// [缩]:
	// (A 当前ATR14 < 最近11根ATR14的20%分位数) OR (B ATR14连续3期递减)
	// AND 额外要求: Bandwidth 连续3期递减
	ks := bars(tf)
	if len(ks) < 25 {
		return false
	}
	n := 11
	if len(ks) < n+2 {
		return false
	}
	last := len(ks) - 1
	// Build ATR last 11
	atrs := make([]float64, 0, n)
	for i := last - (n - 1); i <= last; i++ {
		atrs = append(atrs, atrAt(tf, i))
	}
	// Condition A: < 20% quantile (3rd smallest)
	tmp := make([]float64, len(atrs))
	copy(tmp, atrs)
	sort.Float64s(tmp)
	q20 := tmp[2]
	atrNow := atrAt(tf, last)
	condA := atrNow > 0 && atrNow < q20
	// Condition B: ATR连续3期递减
	atr0 := atrAt(tf, last)
	atr1 := atrAt(tf, last-1)
	atr2 := atrAt(tf, last-2)
	condB := atr0 > 0 && atr1 > 0 && atr2 > 0 && (atr0 < atr1 && atr1 < atr2)
	// Bandwidth 连续3期递减
	bw0 := bandAt(tf, last)
	bw1 := bandAt(tf, last-1)
	bw2 := bandAt(tf, last-2)
	bwDec3 := (bw0 > 0 && bw1 > 0 && bw2 > 0 && (bw0 < bw1 && bw1 < bw2))
	return (condA || condB) && bwDec3
}

func bandAt(tf *TimeframeSeriesData, idx int) float64 {
	u, _, l, ok := bollAt(tf, idx)
	if !ok {
		return 0
	}
	return u - l
}

func condLowVolumeNoSpike(tf *TimeframeSeriesData, v11 float64) bool {
	// [量]:
	// - 最近5根平均量 < 0.8*V11
	// - 最近5根没有任何一根量 > 1.8*V11
	ks := bars(tf)
	if len(ks) < 5 || v11 <= 0 {
		return false
	}
	n := 5
	start := len(ks) - n
	sum := 0.0
	for i := start; i < len(ks); i++ {
		v := ks[i].Volume
		sum += v
		if v > 1.8*v11 {
			return false
		}
	}
	avg := sum / float64(n)
	return avg < 0.8*v11
}

func condMessyFalseBreak(tf *TimeframeSeriesData, atrNow float64) bool {
	// [乱]:
	// - 最近11根整体振幅 (maxHigh - minLow) < 2*当前ATR14
	// - 任意一根 close 到 上下轨的最小距离 < 0.3*ATR14
	ks := bars(tf)
	if len(ks) < 25 {
		return false
	}
	n := 11
	if len(ks) < n {
		return false
	}
	last := len(ks) - 1
	atr := atrAt(tf, last)
	if atrNow > 0 {
		atr = atrNow
	}
	if atr <= 0 {
		return false
	}
	maxH := -math.MaxFloat64
	minL := math.MaxFloat64
	hasTiny := false
	for i := last - (n - 1); i <= last; i++ {
		k := ks[i]
		if k.High > maxH {
			maxH = k.High
		}
		if k.Low < minL {
			minL = k.Low
		}
		up, _, low, ok := bollAt(tf, i)
		if !ok {
			continue
		}
		dist := math.Min(math.Abs(k.Close-up), math.Abs(k.Close-low))
		if dist < 0.3*atr {
			hasTiny = true
		}
	}
	amp := maxH - minL
	return amp < 2*atr && hasTiny
}

func safeDiv(a, b float64) float64 {
	if b == 0 {
		return 0
	}
	return a / b
}

// FormatMarketRegimeForPrompt returns a compact string suitable for embedding into user prompt.
func FormatMarketRegimeForPrompt(r MarketRegimeResult) string {
	hits := ""
	if len(r.Hits) > 0 {
		hits = strings.Join(r.Hits, "+")
	}
	miss := ""
	if len(r.Missing) > 0 {
		miss = strings.Join(r.Missing, ";")
	}
	if hits == "" && miss == "" {
		return fmt.Sprintf("%s", r.Regime)
	}
	parts := []string{string(r.Regime)}
	if hits != "" {
		parts = append(parts, "命中="+hits)
	}
	if miss != "" {
		parts = append(parts, "缺失="+miss)
	}
	return strings.Join(parts, " | ")
}

