package trader

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"nofx/hook"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/adshao/go-binance/v2/futures"
)

// getBrOrderID 生成唯一订单ID（合约专用）
// 格式: x-{BR_ID}{TIMESTAMP}{RANDOM}
// 合约限制32字符，统一使用此限制以保持一致性
// 使用纳秒时间戳+随机数确保全局唯一性（冲突概率 < 10^-20）
func getBrOrderID() string {
	brID := "KzrpZaP9" // 合约br ID

	// 计算可用空间: 32 - len("x-KzrpZaP9") = 32 - 11 = 21字符
	// 分配: 13位时间戳 + 8位随机数 = 21字符（完美利用）
	timestamp := time.Now().UnixNano() % 10000000000000 // 13位纳秒时间戳

	// 生成4字节随机数（8位十六进制）
	randomBytes := make([]byte, 4)
	rand.Read(randomBytes)
	randomHex := hex.EncodeToString(randomBytes)

	// 格式: x-KzrpZaP9{13位时间戳}{8位随机}
	// 示例: x-KzrpZaP91234567890123abcdef12 (正好31字符)
	orderID := fmt.Sprintf("x-%s%d%s", brID, timestamp, randomHex)

	// 确保不超过32字符限制（理论上正好31字符）
	if len(orderID) > 32 {
		orderID = orderID[:32]
	}

	return orderID
}

// FuturesTrader 币安合约交易器
type FuturesTrader struct {
	client    *futures.Client
	apiKey    string
	secretKey string

	// 余额缓存
	cachedBalance     map[string]interface{}
	balanceCacheTime  time.Time
	balanceCacheMutex sync.RWMutex

	// 持仓缓存
	cachedPositions     []map[string]interface{}
	positionsCacheTime  time.Time
	positionsCacheMutex sync.RWMutex

	// 缓存有效期（15秒）
	cacheDuration time.Duration
}

// NewFuturesTrader 创建合约交易器
func NewFuturesTrader(apiKey, secretKey string, userId string) *FuturesTrader {
	client := futures.NewClient(apiKey, secretKey)

	hookRes := hook.HookExec[hook.NewBinanceTraderResult](hook.NEW_BINANCE_TRADER, userId, client)
	if hookRes != nil && hookRes.GetResult() != nil {
		client = hookRes.GetResult()
	}

	// 同步时间，避免 Timestamp ahead 错误
	syncBinanceServerTime(client)
	trader := &FuturesTrader{
		client:      client,
		apiKey:      apiKey,
		secretKey:   secretKey,
		cacheDuration: 15 * time.Second, // 15秒缓存
	}

	// 设置双向持仓模式（Hedge Mode）
	// 这是必需的，因为代码中使用了 PositionSide (LONG/SHORT)
	if err := trader.setDualSidePosition(); err != nil {
		log.Printf("⚠️ 设置双向持仓模式失败: %v (如果已是双向模式则忽略此警告)", err)
	}

	return trader
}

// setDualSidePosition 设置双向持仓模式（初始化时调用）
func (t *FuturesTrader) setDualSidePosition() error {
	// 尝试设置双向持仓模式
	err := t.client.NewChangePositionModeService().
		DualSide(true). // true = 双向持仓（Hedge Mode）
		Do(context.Background())

	if err != nil {
		// 如果错误信息包含"No need to change"，说明已经是双向持仓模式
		if strings.Contains(err.Error(), "No need to change position side") {
			log.Printf("  ✓ 账户已是双向持仓模式（Hedge Mode）")
			return nil
		}
		// 其他错误则返回（但在调用方不会中断初始化）
		return err
	}

	log.Printf("  ✓ 账户已切换为双向持仓模式（Hedge Mode）")
	log.Printf("  ℹ️  双向持仓模式允许同时持有多单和空单")
	return nil
}

// syncBinanceServerTime 同步币安服务器时间，确保请求时间戳合法
func syncBinanceServerTime(client *futures.Client) {
	serverTime, err := client.NewServerTimeService().Do(context.Background())
	if err != nil {
		log.Printf("⚠️ 同步币安服务器时间失败: %v", err)
		return
	}

	now := time.Now().UnixMilli()
	offset := now - serverTime
	client.TimeOffset = offset
	log.Printf("⏱ 已同步币安服务器时间，偏移 %dms", offset)
}

// GetBalance 获取账户余额（带缓存）
func (t *FuturesTrader) GetBalance() (map[string]interface{}, error) {
	// 先检查缓存是否有效
	t.balanceCacheMutex.RLock()
	if t.cachedBalance != nil && time.Since(t.balanceCacheTime) < t.cacheDuration {
		cacheAge := time.Since(t.balanceCacheTime)
		t.balanceCacheMutex.RUnlock()
		log.Printf("✓ 使用缓存的账户余额（缓存时间: %.1f秒前）", cacheAge.Seconds())
		return t.cachedBalance, nil
	}
	t.balanceCacheMutex.RUnlock()

	// 缓存过期或不存在，调用API
	log.Printf("🔄 缓存过期，正在调用币安API获取账户余额...")
	account, err := t.client.NewGetAccountService().Do(context.Background())
	if err != nil {
		log.Printf("❌ 币安API调用失败: %v", err)
		return nil, fmt.Errorf("获取账户信息失败: %w", err)
	}

	result := make(map[string]interface{})
	result["totalWalletBalance"], _ = strconv.ParseFloat(account.TotalWalletBalance, 64)
	result["availableBalance"], _ = strconv.ParseFloat(account.AvailableBalance, 64)
	result["totalUnrealizedProfit"], _ = strconv.ParseFloat(account.TotalUnrealizedProfit, 64)

	log.Printf("✓ 币安API返回: 总余额=%s, 可用=%s, 未实现盈亏=%s",
		account.TotalWalletBalance,
		account.AvailableBalance,
		account.TotalUnrealizedProfit)

	// 更新缓存
	t.balanceCacheMutex.Lock()
	t.cachedBalance = result
	t.balanceCacheTime = time.Now()
	t.balanceCacheMutex.Unlock()

	return result, nil
}

// GetPositions 获取所有持仓（带缓存）
func (t *FuturesTrader) GetPositions() ([]map[string]interface{}, error) {
	// 先检查缓存是否有效
	t.positionsCacheMutex.RLock()
	if t.cachedPositions != nil && time.Since(t.positionsCacheTime) < t.cacheDuration {
		cacheAge := time.Since(t.positionsCacheTime)
		t.positionsCacheMutex.RUnlock()
		log.Printf("✓ 使用缓存的持仓信息（缓存时间: %.1f秒前）", cacheAge.Seconds())
		return t.cachedPositions, nil
	}
	t.positionsCacheMutex.RUnlock()

	// 缓存过期或不存在，调用API
	log.Printf("🔄 缓存过期，正在调用币安API获取持仓信息...")
	positions, err := t.client.NewGetPositionRiskService().Do(context.Background())
	if err != nil {
		return nil, fmt.Errorf("获取持仓失败: %w", err)
	}

	var result []map[string]interface{}
	for _, pos := range positions {
		posAmt, _ := strconv.ParseFloat(pos.PositionAmt, 64)
		if posAmt == 0 {
			continue // 跳过无持仓的
		}

		posMap := make(map[string]interface{})
		posMap["symbol"] = pos.Symbol
		posMap["positionAmt"], _ = strconv.ParseFloat(pos.PositionAmt, 64)
		posMap["entryPrice"], _ = strconv.ParseFloat(pos.EntryPrice, 64)
		posMap["markPrice"], _ = strconv.ParseFloat(pos.MarkPrice, 64)
		posMap["unRealizedProfit"], _ = strconv.ParseFloat(pos.UnRealizedProfit, 64)
		posMap["leverage"], _ = strconv.ParseFloat(pos.Leverage, 64)
		posMap["liquidationPrice"], _ = strconv.ParseFloat(pos.LiquidationPrice, 64)

		// 判断方向
		if posAmt > 0 {
			posMap["side"] = "long"
		} else {
			posMap["side"] = "short"
		}

		result = append(result, posMap)
	}

	// 更新缓存
	t.positionsCacheMutex.Lock()
	t.cachedPositions = result
	t.positionsCacheTime = time.Now()
	t.positionsCacheMutex.Unlock()

	return result, nil
}

// SetMarginMode 设置仓位模式
func (t *FuturesTrader) SetMarginMode(symbol string, isCrossMargin bool) error {
	var marginType futures.MarginType
	if isCrossMargin {
		marginType = futures.MarginTypeCrossed
	} else {
		marginType = futures.MarginTypeIsolated
	}

	// 尝试设置仓位模式
	err := t.client.NewChangeMarginTypeService().
		Symbol(symbol).
		MarginType(marginType).
		Do(context.Background())

	marginModeStr := "全仓"
	if !isCrossMargin {
		marginModeStr = "逐仓"
	}

	if err != nil {
		// 如果错误信息包含"No need to change"，说明仓位模式已经是目标值
		if contains(err.Error(), "No need to change margin type") {
			log.Printf("  ✓ %s 仓位模式已是 %s", symbol, marginModeStr)
			return nil
		}
		// 如果有持仓，无法更改仓位模式，但不影响交易
		if contains(err.Error(), "Margin type cannot be changed if there exists position") {
			log.Printf("  ⚠️ %s 有持仓，无法更改仓位模式，继续使用当前模式", symbol)
			return nil
		}
		// 检测多资产模式（错误码 -4168）
		if contains(err.Error(), "Multi-Assets mode") || contains(err.Error(), "-4168") || contains(err.Error(), "4168") {
			log.Printf("  ⚠️ %s 检测到多资产模式，强制使用全仓模式", symbol)
			log.Printf("  💡 提示：如需使用逐仓模式，请在币安关闭多资产模式")
			return nil
		}
		// 检测统一账户 API（Portfolio Margin）
		if contains(err.Error(), "unified") || contains(err.Error(), "portfolio") || contains(err.Error(), "Portfolio") {
			log.Printf("  ❌ %s 检测到统一账户 API，无法进行合约交易", symbol)
			return fmt.Errorf("请使用「现货与合约交易」API 权限，不要使用「统一账户 API」")
		}
		log.Printf("  ⚠️ 设置仓位模式失败: %v", err)
		// 不返回错误，让交易继续
		return nil
	}

	log.Printf("  ✓ %s 仓位模式已设置为 %s", symbol, marginModeStr)
	return nil
}

// SetLeverage 设置杠杆（智能判断+冷却期）
func (t *FuturesTrader) SetLeverage(symbol string, leverage int) error {
	// 先尝试获取当前杠杆（从持仓信息）
	currentLeverage := 0
	positions, err := t.GetPositions()
	if err == nil {
		for _, pos := range positions {
			if pos["symbol"] == symbol {
				if lev, ok := pos["leverage"].(float64); ok {
					currentLeverage = int(lev)
					break
				}
			}
		}
	}

	// 如果当前杠杆已经是目标杠杆，跳过
	if currentLeverage == leverage && currentLeverage > 0 {
		log.Printf("  ✓ %s 杠杆已是 %dx，无需切换", symbol, leverage)
		return nil
	}

	// 切换杠杆
	_, err = t.client.NewChangeLeverageService().
		Symbol(symbol).
		Leverage(leverage).
		Do(context.Background())

	if err != nil {
		// 如果错误信息包含"No need to change"，说明杠杆已经是目标值
		if contains(err.Error(), "No need to change") {
			log.Printf("  ✓ %s 杠杆已是 %dx", symbol, leverage)
			return nil
		}
		return fmt.Errorf("设置杠杆失败: %w", err)
	}

	log.Printf("  ✓ %s 杠杆已切换为 %dx", symbol, leverage)

	// 切换杠杆后等待5秒（避免冷却期错误）
	log.Printf("  ⏱ 等待5秒冷却期...")
	time.Sleep(5 * time.Second)

	return nil
}

// OpenLong 开多仓
func (t *FuturesTrader) OpenLong(symbol string, quantity float64, leverage int) (map[string]interface{}, error) {
	// 先取消该币种的所有委托单（清理旧的止损止盈单）
	if err := t.CancelAllOrders(symbol); err != nil {
		log.Printf("  ⚠ 取消旧委托单失败（可能没有委托单）: %v", err)
	}

	// 设置杠杆
	if err := t.SetLeverage(symbol, leverage); err != nil {
		return nil, err
	}

	// 注意：仓位模式应该由调用方（AutoTrader）在开仓前通过 SetMarginMode 设置

	// 格式化数量到正确精度
	quantityStr, err := t.FormatQuantity(symbol, quantity)
	if err != nil {
		return nil, err
	}

	// ✅ 检查格式化后的数量是否为 0（防止四舍五入导致的错误）
	quantityFloat, parseErr := strconv.ParseFloat(quantityStr, 64)
	if parseErr != nil || quantityFloat <= 0 {
		return nil, fmt.Errorf("开仓数量过小，格式化后为 0 (原始: %.8f → 格式化: %s)。建议增加开仓金额或选择价格更低的币种", quantity, quantityStr)
	}

	// ✅ 检查最小名义价值（Binance 要求至少 10 USDT）
	if err := t.CheckMinNotional(symbol, quantityFloat); err != nil {
		return nil, err
	}

	// 创建市价买入订单（使用br ID）
	order, err := t.client.NewCreateOrderService().
		Symbol(symbol).
		Side(futures.SideTypeBuy).
		PositionSide(futures.PositionSideTypeLong).
		Type(futures.OrderTypeMarket).
		Quantity(quantityStr).
		NewClientOrderID(getBrOrderID()).
		Do(context.Background())

	if err != nil {
		return nil, fmt.Errorf("开多仓失败: %w", err)
	}

	log.Printf("✓ 开多仓成功: %s 数量: %s", symbol, quantityStr)
	log.Printf("  订单ID: %d", order.OrderID)

	result := make(map[string]interface{})
	result["orderId"] = order.OrderID
	result["symbol"] = order.Symbol
	result["status"] = order.Status
	return result, nil
}

// OpenShort 开空仓
func (t *FuturesTrader) OpenShort(symbol string, quantity float64, leverage int) (map[string]interface{}, error) {
	// 先取消该币种的所有委托单（清理旧的止损止盈单）
	if err := t.CancelAllOrders(symbol); err != nil {
		log.Printf("  ⚠ 取消旧委托单失败（可能没有委托单）: %v", err)
	}

	// 设置杠杆
	if err := t.SetLeverage(symbol, leverage); err != nil {
		return nil, err
	}

	// 注意：仓位模式应该由调用方（AutoTrader）在开仓前通过 SetMarginMode 设置

	// 格式化数量到正确精度
	quantityStr, err := t.FormatQuantity(symbol, quantity)
	if err != nil {
		return nil, err
	}

	// ✅ 检查格式化后的数量是否为 0（防止四舍五入导致的错误）
	quantityFloat, parseErr := strconv.ParseFloat(quantityStr, 64)
	if parseErr != nil || quantityFloat <= 0 {
		return nil, fmt.Errorf("开仓数量过小，格式化后为 0 (原始: %.8f → 格式化: %s)。建议增加开仓金额或选择价格更低的币种", quantity, quantityStr)
	}

	// ✅ 检查最小名义价值（Binance 要求至少 10 USDT）
	if err := t.CheckMinNotional(symbol, quantityFloat); err != nil {
		return nil, err
	}

	// 创建市价卖出订单（使用br ID）
	order, err := t.client.NewCreateOrderService().
		Symbol(symbol).
		Side(futures.SideTypeSell).
		PositionSide(futures.PositionSideTypeShort).
		Type(futures.OrderTypeMarket).
		Quantity(quantityStr).
		NewClientOrderID(getBrOrderID()).
		Do(context.Background())

	if err != nil {
		return nil, fmt.Errorf("开空仓失败: %w", err)
	}

	log.Printf("✓ 开空仓成功: %s 数量: %s", symbol, quantityStr)
	log.Printf("  订单ID: %d", order.OrderID)

	result := make(map[string]interface{})
	result["orderId"] = order.OrderID
	result["symbol"] = order.Symbol
	result["status"] = order.Status
	return result, nil
}

// CloseLong 平多仓
func (t *FuturesTrader) CloseLong(symbol string, quantity float64) (map[string]interface{}, error) {
	// 如果数量为0，获取当前持仓数量
	if quantity == 0 {
		positions, err := t.GetPositions()
		if err != nil {
			return nil, err
		}

		for _, pos := range positions {
			if pos["symbol"] == symbol && pos["side"] == "long" {
				quantity = pos["positionAmt"].(float64)
				break
			}
		}

		if quantity == 0 {
			return nil, fmt.Errorf("没有找到 %s 的多仓", symbol)
		}
	}

	// 格式化数量
	quantityStr, err := t.FormatQuantity(symbol, quantity)
	if err != nil {
		return nil, err
	}

	// 创建市价卖出订单（平多，使用br ID）
	order, err := t.client.NewCreateOrderService().
		Symbol(symbol).
		Side(futures.SideTypeSell).
		PositionSide(futures.PositionSideTypeLong).
		Type(futures.OrderTypeMarket).
		Quantity(quantityStr).
		NewClientOrderID(getBrOrderID()).
		Do(context.Background())

	if err != nil {
		return nil, fmt.Errorf("平多仓失败: %w", err)
	}

	log.Printf("✓ 平多仓成功: %s 数量: %s", symbol, quantityStr)

	// 平仓后取消该币种的所有挂单（止损止盈单）
	if err := t.CancelAllOrders(symbol); err != nil {
		log.Printf("  ⚠ 取消挂单失败: %v", err)
	}

	result := make(map[string]interface{})
	result["orderId"] = order.OrderID
	result["symbol"] = order.Symbol
	result["status"] = order.Status
	return result, nil
}

// CloseShort 平空仓
func (t *FuturesTrader) CloseShort(symbol string, quantity float64) (map[string]interface{}, error) {
	// 如果数量为0，获取当前持仓数量
	if quantity == 0 {
		positions, err := t.GetPositions()
		if err != nil {
			return nil, err
		}

		for _, pos := range positions {
			if pos["symbol"] == symbol && pos["side"] == "short" {
				quantity = -pos["positionAmt"].(float64) // 空仓数量是负的，取绝对值
				break
			}
		}

		if quantity == 0 {
			return nil, fmt.Errorf("没有找到 %s 的空仓", symbol)
		}
	}

	// 格式化数量
	quantityStr, err := t.FormatQuantity(symbol, quantity)
	if err != nil {
		return nil, err
	}

	// 创建市价买入订单（平空，使用br ID）
	order, err := t.client.NewCreateOrderService().
		Symbol(symbol).
		Side(futures.SideTypeBuy).
		PositionSide(futures.PositionSideTypeShort).
		Type(futures.OrderTypeMarket).
		Quantity(quantityStr).
		NewClientOrderID(getBrOrderID()).
		Do(context.Background())

	if err != nil {
		return nil, fmt.Errorf("平空仓失败: %w", err)
	}

	log.Printf("✓ 平空仓成功: %s 数量: %s", symbol, quantityStr)

	// 平仓后取消该币种的所有挂单（止损止盈单）
	if err := t.CancelAllOrders(symbol); err != nil {
		log.Printf("  ⚠ 取消挂单失败: %v", err)
	}

	result := make(map[string]interface{})
	result["orderId"] = order.OrderID
	result["symbol"] = order.Symbol
	result["status"] = order.Status
	return result, nil
}

// CancelStopLossOrders 仅取消止损单（不影响止盈单）
// 注意：现在使用Algo Order API创建止损/止盈，需要使用条件订单的查询和取消接口
func (t *FuturesTrader) CancelStopLossOrders(symbol string) error {
	// 查询条件订单（使用openAlgoOrders查询当前挂单）
	algoOrders, err := t.queryAlgoOrders(symbol)
	if err != nil {
		return fmt.Errorf("获取条件订单失败: %w", err)
	}

	// 过滤出止损单并取消（取消所有方向的止损单，包括LONG和SHORT）
	canceledCount := 0
	var cancelErrors []error
	for _, order := range algoOrders {
		orderType, _ := order["orderType"].(string)

		// 只取消止损订单（不取消止盈订单）
		if orderType == "STOP_MARKET" || orderType == "STOP" {
			algoId, ok := order["algoId"].(float64)
			if !ok {
				log.Printf("  ⚠ 无法获取algoId，跳过订单: %v", order)
				continue
			}

			err := t.cancelAlgoOrder(symbol, int64(algoId))
			if err != nil {
				errMsg := fmt.Sprintf("algoId %d: %v", int64(algoId), err)
				cancelErrors = append(cancelErrors, fmt.Errorf("%s", errMsg))
				log.Printf("  ⚠ 取消止损单失败: %s", errMsg)
				continue
			}

			canceledCount++
			positionSide, _ := order["positionSide"].(string)
			log.Printf("  ✓ 已取消止损单 (algoId: %d, 类型: %s, 方向: %s)", int64(algoId), orderType, positionSide)
		}
	}

	if canceledCount == 0 && len(cancelErrors) == 0 {
		log.Printf("  ℹ %s 没有止损单需要取消", symbol)
	} else if canceledCount > 0 {
		log.Printf("  ✓ 已取消 %s 的 %d 个止损单", symbol, canceledCount)
	}

	// 如果所有取消都失败了，返回错误
	if len(cancelErrors) > 0 && canceledCount == 0 {
		return fmt.Errorf("取消止损单失败: %v", cancelErrors)
	}

	return nil
}

// CancelTakeProfitOrders 仅取消止盈单（不影响止损单）
// 注意：现在使用Algo Order API创建止损/止盈，需要使用条件订单的查询和取消接口
func (t *FuturesTrader) CancelTakeProfitOrders(symbol string) error {
	// 查询条件订单
	algoOrders, err := t.queryAlgoOrders(symbol)
	if err != nil {
		return fmt.Errorf("获取条件订单失败: %w", err)
	}

	// 过滤出止盈单并取消（取消所有方向的止盈单，包括LONG和SHORT）
	canceledCount := 0
	var cancelErrors []error
	for _, order := range algoOrders {
		orderType, _ := order["orderType"].(string)

		// 只取消止盈订单（不取消止损订单）
		if orderType == "TAKE_PROFIT_MARKET" || orderType == "TAKE_PROFIT" {
			algoId, ok := order["algoId"].(float64)
			if !ok {
				log.Printf("  ⚠ 无法获取algoId，跳过订单: %v", order)
				continue
			}

			err := t.cancelAlgoOrder(symbol, int64(algoId))
			if err != nil {
				errMsg := fmt.Sprintf("algoId %d: %v", int64(algoId), err)
				cancelErrors = append(cancelErrors, fmt.Errorf("%s", errMsg))
				log.Printf("  ⚠ 取消止盈单失败: %s", errMsg)
				continue
			}

			canceledCount++
			positionSide, _ := order["positionSide"].(string)
			log.Printf("  ✓ 已取消止盈单 (algoId: %d, 类型: %s, 方向: %s)", int64(algoId), orderType, positionSide)
		}
	}

	if canceledCount == 0 && len(cancelErrors) == 0 {
		log.Printf("  ℹ %s 没有止盈单需要取消", symbol)
	} else if canceledCount > 0 {
		log.Printf("  ✓ 已取消 %s 的 %d 个止盈单", symbol, canceledCount)
	}

	// 如果所有取消都失败了，返回错误
	if len(cancelErrors) > 0 && canceledCount == 0 {
		return fmt.Errorf("取消止盈单失败: %v", cancelErrors)
	}

	return nil
}

// CancelAllOrders 取消该币种的所有挂单（包括普通订单和条件订单）
func (t *FuturesTrader) CancelAllOrders(symbol string) error {
	var errors []error

	// 1. 取消所有普通订单
	err := t.client.NewCancelAllOpenOrdersService().
		Symbol(symbol).
		Do(context.Background())

	if err != nil {
		errors = append(errors, fmt.Errorf("取消普通订单失败: %w", err))
	}

	// 2. 取消所有条件订单
	algoOrders, err := t.queryAlgoOrders(symbol)
	if err != nil {
		// 如果查询失败，只记录错误但不中断（可能没有条件订单）
		log.Printf("  ⚠ 查询条件订单失败: %v", err)
	} else {
		// 取消所有条件订单
		for _, algoOrder := range algoOrders {
			algoId, ok := algoOrder["algoId"].(float64)
			if !ok {
				continue
			}
			if err := t.cancelAlgoOrder(symbol, int64(algoId)); err != nil {
				errors = append(errors, fmt.Errorf("取消条件订单 %d 失败: %w", int64(algoId), err))
			}
		}
	}

	// 如果有错误，返回第一个错误
	if len(errors) > 0 {
		return errors[0]
	}

	log.Printf("  ✓ 已取消 %s 的所有挂单（包括普通订单和条件订单）", symbol)
	return nil
}

// CancelStopOrders 取消该币种的止盈/止损单（用于调整止盈止损位置）
// 注意：现在使用Algo Order API创建止损/止盈，需要使用条件订单的查询和取消接口
func (t *FuturesTrader) CancelStopOrders(symbol string) error {
	// 查询条件订单
	algoOrders, err := t.queryAlgoOrders(symbol)
	if err != nil {
		return fmt.Errorf("获取条件订单失败: %w", err)
	}

	// 过滤出止盈止损单并取消
	canceledCount := 0
	for _, order := range algoOrders {
		orderType, _ := order["orderType"].(string)

		// 只取消止损和止盈订单
		if orderType == "STOP_MARKET" ||
			orderType == "TAKE_PROFIT_MARKET" ||
			orderType == "STOP" ||
			orderType == "TAKE_PROFIT" {

			algoId, ok := order["algoId"].(float64)
			if !ok {
				log.Printf("  ⚠ 无法获取algoId，跳过订单: %v", order)
				continue
			}

			err := t.cancelAlgoOrder(symbol, int64(algoId))
			if err != nil {
				log.Printf("  ⚠ 取消订单 %d 失败: %v", int64(algoId), err)
				continue
			}

			canceledCount++
			log.Printf("  ✓ 已取消 %s 的止盈/止损单 (algoId: %d, 类型: %s)",
				symbol, int64(algoId), orderType)
		}
	}

	if canceledCount == 0 {
		log.Printf("  ℹ %s 没有止盈/止损单需要取消", symbol)
	} else {
		log.Printf("  ✓ 已取消 %s 的 %d 个止盈/止损单", symbol, canceledCount)
	}

	return nil
}

// GetMarketPrice 获取市场价格
func (t *FuturesTrader) GetMarketPrice(symbol string) (float64, error) {
	prices, err := t.client.NewListPricesService().Symbol(symbol).Do(context.Background())
	if err != nil {
		return 0, fmt.Errorf("获取价格失败: %w", err)
	}

	if len(prices) == 0 {
		return 0, fmt.Errorf("未找到价格")
	}

	price, err := strconv.ParseFloat(prices[0].Price, 64)
	if err != nil {
		return 0, err
	}

	return price, nil
}

// CalculatePositionSize 计算仓位大小
func (t *FuturesTrader) CalculatePositionSize(balance, riskPercent, price float64, leverage int) float64 {
	riskAmount := balance * (riskPercent / 100.0)
	positionValue := riskAmount * float64(leverage)
	quantity := positionValue / price
	return quantity
}

// SetStopLoss 设置止损单（使用Algo Order API）
func (t *FuturesTrader) SetStopLoss(symbol string, positionSide string, quantity, stopPrice float64) error {
	var side string
	var posSide string

	if positionSide == "LONG" {
		side = "SELL"
		posSide = "LONG"
	} else {
		side = "BUY"
		posSide = "SHORT"
	}

	// 格式化数量
	quantityStr, err := t.FormatQuantity(symbol, quantity)
	if err != nil {
		return err
	}

	// 使用Algo Order API创建止损单
	err = t.createAlgoOrder(symbol, side, posSide, "STOP_MARKET", quantityStr, stopPrice)
	if err != nil {
		return fmt.Errorf("设置止损失败: %w", err)
	}

	log.Printf("  止损价设置: %.4f", stopPrice)
	return nil
}

// SetTakeProfit 设置止盈单（使用Algo Order API）
func (t *FuturesTrader) SetTakeProfit(symbol string, positionSide string, quantity, takeProfitPrice float64) error {
	var side string
	var posSide string

	if positionSide == "LONG" {
		side = "SELL"
		posSide = "LONG"
	} else {
		side = "BUY"
		posSide = "SHORT"
	}

	// 格式化数量
	quantityStr, err := t.FormatQuantity(symbol, quantity)
	if err != nil {
		return err
	}

	// 使用Algo Order API创建止盈单
	err = t.createAlgoOrder(symbol, side, posSide, "TAKE_PROFIT_MARKET", quantityStr, takeProfitPrice)
	if err != nil {
		return fmt.Errorf("设置止盈失败: %w", err)
	}

	log.Printf("  止盈价设置: %.4f", takeProfitPrice)
	return nil
}

// createAlgoOrder 使用Algo Order API创建条件订单（止损/止盈）
// 参考: https://developers.binance.com/docs/zh-CN/derivatives/usds-margined-futures/trade/rest-api/New-Algo-Order
func (t *FuturesTrader) createAlgoOrder(symbol, side, positionSide, orderType, quantityStr string, stopPrice float64) error {
	// 格式化触发价格到正确的精度
	roundedPrice, err := t.FormatPrice(symbol, stopPrice)
	if err != nil {
		return fmt.Errorf("格式化价格失败: %w", err)
	}

	// 获取价格精度用于格式化字符串
	pricePrecision, err := t.GetPricePrecision(symbol)
	if err != nil {
		pricePrecision = 8 // 默认精度
	}
	format := fmt.Sprintf("%%.%df", pricePrecision)
	triggerPriceStr := fmt.Sprintf(format, roundedPrice)

	// 构建请求参数
	params := url.Values{}
	params.Set("algoType", "CONDITIONAL") // 必须设置为CONDITIONAL
	params.Set("symbol", symbol)
	params.Set("side", side)
	params.Set("positionSide", positionSide)
	params.Set("type", orderType)
	params.Set("triggerPrice", triggerPriceStr) // 使用triggerPrice而不是stopPrice
	params.Set("workingType", "CONTRACT_PRICE")
	params.Set("timeInForce", "GTC")
	// 使用closePosition=true来平仓，而不是指定quantity
	// 根据文档：STOP_MARKET和TAKE_PROFIT_MARKET配合closePosition=true可以平掉当时持有的所有仓位
	params.Set("closePosition", "true")
	params.Set("timestamp", strconv.FormatInt(time.Now().UnixMilli(), 10))

	// 生成签名（在添加signature之前）
	queryString := params.Encode()
	signature := t.generateSignature(queryString)
	params.Set("signature", signature)

	// 构建请求URL - 使用Algo Order API端点
	baseURL := "https://fapi.binance.com"
	reqURL := fmt.Sprintf("%s/fapi/v1/algoOrder", baseURL)

	// 创建HTTP请求，参数放在请求体中
	req, err := http.NewRequest("POST", reqURL, strings.NewReader(params.Encode()))
	if err != nil {
		return fmt.Errorf("创建请求失败: %w", err)
	}

	// 设置请求头
	req.Header.Set("X-MBX-APIKEY", t.apiKey)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	// 发送请求
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	// 读取响应
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("读取响应失败: %w", err)
	}

	// 检查响应状态
	if resp.StatusCode != http.StatusOK {
		// 尝试解析JSON错误响应
		bodyStr := string(body)
		if strings.Contains(bodyStr, `"code"`) {
			// 如果是JSON格式的错误响应，直接返回
			return fmt.Errorf("API错误 (状态码: %d): %s", resp.StatusCode, bodyStr)
		}
		// 如果是HTML错误页面（如404），返回更友好的错误信息
		if strings.Contains(bodyStr, "<!DOCTYPE html>") || strings.Contains(bodyStr, "<html>") {
			return fmt.Errorf("API端点不存在 (状态码: %d): 请检查API端点是否正确", resp.StatusCode)
		}
		return fmt.Errorf("API错误 (状态码: %d): %s", resp.StatusCode, bodyStr)
	}

	// 检查响应体是否包含错误信息
	bodyStr := string(body)
	if strings.Contains(bodyStr, `"code"`) {
		// 尝试解析JSON错误响应
		if strings.Contains(bodyStr, "-4120") {
			return fmt.Errorf("Binance API错误 -4120: %s", bodyStr)
		}
		// 其他错误代码也返回
		if strings.Contains(bodyStr, `"msg"`) {
			return fmt.Errorf("Binance API错误: %s", bodyStr)
		}
	}

	// 成功响应通常包含algoId和clientAlgoId
	// 响应格式: {"algoId": 2146760, "clientAlgoId": "...", "algoStatus": "NEW", ...}
	log.Printf("  ✓ Algo订单创建成功: %s", bodyStr)
	return nil
}

// generateSignature 生成HMAC SHA256签名
func (t *FuturesTrader) generateSignature(queryString string) string {
	mac := hmac.New(sha256.New, []byte(t.secretKey))
	mac.Write([]byte(queryString))
	return hex.EncodeToString(mac.Sum(nil))
}

// queryAlgoOrders 查询条件订单（当前挂单）
// 使用 GET /fapi/v1/openAlgoOrders 接口查询当前所有条件订单
func (t *FuturesTrader) queryAlgoOrders(symbol string) ([]map[string]interface{}, error) {
	// 构建请求参数
	params := url.Values{}
	params.Set("symbol", symbol)
	// 使用当前时间戳（毫秒）
	timestamp := time.Now().UnixMilli()
	params.Set("timestamp", strconv.FormatInt(timestamp, 10))
	// 添加recvWindow参数以提高兼容性
	params.Set("recvWindow", "5000")

	// 生成签名（在添加signature之前）
	queryString := params.Encode()
	signature := t.generateSignature(queryString)
	params.Set("signature", signature)

	// 构建请求URL - 查询当前条件挂单
	baseURL := "https://fapi.binance.com"
	reqURL := fmt.Sprintf("%s/fapi/v1/openAlgoOrders?%s", baseURL, params.Encode())

	// 创建HTTP请求
	req, err := http.NewRequest("GET", reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}

	// 设置请求头
	req.Header.Set("X-MBX-APIKEY", t.apiKey)

	// 发送请求
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	// 读取响应
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}

	// 检查响应状态
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API错误 (状态码: %d): %s", resp.StatusCode, string(body))
	}

	// 解析JSON响应
	var orders []map[string]interface{}
	if err := json.Unmarshal(body, &orders); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	return orders, nil
}

// cancelAlgoOrder 取消条件订单
// 参考: https://developers.binance.com/docs/zh-CN/derivatives/usds-margined-futures/trade/rest-api/Cancel-Algo-Order
func (t *FuturesTrader) cancelAlgoOrder(symbol string, algoId int64) error {
	// 构建请求参数
	params := url.Values{}
	params.Set("symbol", symbol)
	params.Set("algoId", strconv.FormatInt(algoId, 10))
	params.Set("timestamp", strconv.FormatInt(time.Now().UnixMilli(), 10))

	// 生成签名
	queryString := params.Encode()
	signature := t.generateSignature(queryString)
	params.Set("signature", signature)

	// 构建请求URL - 取消条件订单
	baseURL := "https://fapi.binance.com"
	reqURL := fmt.Sprintf("%s/fapi/v1/algoOrder?%s", baseURL, params.Encode())

	// 创建HTTP请求（DELETE方法）
	req, err := http.NewRequest("DELETE", reqURL, nil)
	if err != nil {
		return fmt.Errorf("创建请求失败: %w", err)
	}

	// 设置请求头
	req.Header.Set("X-MBX-APIKEY", t.apiKey)

	// 发送请求
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	// 读取响应
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("读取响应失败: %w", err)
	}

	// 检查响应状态
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("API错误 (状态码: %d): %s", resp.StatusCode, string(body))
	}

	// 检查响应体是否包含错误信息
	bodyStr := string(body)
	// Binance API 成功响应格式: {"algoId":...,"code":"200","msg":"success"}
	// 错误响应格式: {"code":-1022,"msg":"Signature for this request is not valid."}
	// 需要检查 code 是否为 "200" 或 200（成功），其他值才是错误
	if strings.Contains(bodyStr, `"code"`) {
		// 检查是否是成功响应
		if strings.Contains(bodyStr, `"code":"200"`) || strings.Contains(bodyStr, `"code":200`) {
			// 成功响应，返回 nil
			return nil
		}
		// 检查是否是订单不存在的错误（-2011），这种情况应该视为成功
		// 因为订单可能已经被触发、取消或过期
		if strings.Contains(bodyStr, `"code":-2011`) || strings.Contains(bodyStr, `"-2011"`) {
			// 订单不存在，视为成功（订单已经被取消或触发）
			log.Printf("  ℹ 条件订单 %d 不存在（可能已被触发或取消）", algoId)
			return nil
		}
		// 检查是否包含错误消息
		if strings.Contains(bodyStr, `"msg"`) {
			// 这是错误响应
			return fmt.Errorf("Binance API错误: %s", bodyStr)
		}
	}

	return nil
}

// GetOpenOrders 获取所有挂单（用于清理孤儿订单）
// 注意：需要同时查询普通订单（/fapi/v1/openOrders）和条件订单（/fapi/v1/openAlgoOrders）
func (t *FuturesTrader) GetOpenOrders() (map[string][]map[string]interface{}, error) {
	result := make(map[string][]map[string]interface{})

	// 1. 获取所有普通订单（使用 /fapi/v1/openOrders）
	orders, err := t.client.NewListOpenOrdersService().
		Do(context.Background())

	if err != nil {
		return nil, fmt.Errorf("获取普通挂单失败: %w", err)
	}

	// 按币种分组普通订单
	for _, order := range orders {
		symbol := order.Symbol
		orderMap := map[string]interface{}{
			"symbol":      order.Symbol,
			"orderId":     order.OrderID,
			"type":        string(order.Type),
			"positionSide": string(order.PositionSide),
			"side":        string(order.Side),
		}
		result[symbol] = append(result[symbol], orderMap)
	}

	// 2. 获取所有条件订单（使用 /fapi/v1/openAlgoOrders）
	// 查询当前所有条件订单，如果失败则忽略（不影响普通订单的返回）
	allAlgoOrders, err := t.queryAllAlgoOrders()
	if err != nil {
		// 如果查询条件订单失败，只返回普通订单（不影响主要功能）
		log.Printf("  ⚠ 获取条件订单失败（将只返回普通订单）: %v", err)
		return result, nil
	}

	// 按币种分组条件订单
	for _, algoOrder := range allAlgoOrders {
		symbol, _ := algoOrder["symbol"].(string)
		if symbol == "" {
			continue
		}
		algoId, _ := algoOrder["algoId"].(float64)
		orderType, _ := algoOrder["orderType"].(string)
		positionSide, _ := algoOrder["positionSide"].(string)
		side, _ := algoOrder["side"].(string)

		orderMap := map[string]interface{}{
			"symbol":      symbol,
			"orderId":     int64(algoId), // 使用algoId作为orderId
			"algoId":      int64(algoId), // 同时保存algoId
			"type":        orderType,
			"positionSide": positionSide,
			"side":        side,
			"isAlgoOrder": true, // 标记为条件订单
		}
		result[symbol] = append(result[symbol], orderMap)
	}

	return result, nil
}

// queryAllAlgoOrders 查询所有条件订单（不指定symbol）
// 使用 GET /fapi/v1/openAlgoOrders 接口查询当前所有条件订单
func (t *FuturesTrader) queryAllAlgoOrders() ([]map[string]interface{}, error) {
	// 构建请求参数（不指定symbol，查询所有条件订单）
	params := url.Values{}
	// 使用当前时间戳（毫秒）
	timestamp := time.Now().UnixMilli()
	params.Set("timestamp", strconv.FormatInt(timestamp, 10))
	// 添加recvWindow参数以提高兼容性
	params.Set("recvWindow", "5000")

	// 生成签名（在添加signature之前）
	queryString := params.Encode()
	signature := t.generateSignature(queryString)
	params.Set("signature", signature)

	// 构建请求URL - 查询所有条件挂单
	baseURL := "https://fapi.binance.com"
	reqURL := fmt.Sprintf("%s/fapi/v1/openAlgoOrders?%s", baseURL, params.Encode())

	// 创建HTTP请求
	req, err := http.NewRequest("GET", reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}

	// 设置请求头
	req.Header.Set("X-MBX-APIKEY", t.apiKey)

	// 发送请求
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	// 读取响应
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}

	// 检查响应状态
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API错误 (状态码: %d): %s", resp.StatusCode, string(body))
	}

	// 解析JSON响应
	var orders []map[string]interface{}
	if err := json.Unmarshal(body, &orders); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	return orders, nil
}

// GetMinNotional 获取最小名义价值（Binance要求）
func (t *FuturesTrader) GetMinNotional(symbol string) float64 {
	// 使用保守的默认值 10 USDT，确保订单能够通过交易所验证
	return 10.0
}

// CheckMinNotional 检查订单是否满足最小名义价值要求
func (t *FuturesTrader) CheckMinNotional(symbol string, quantity float64) error {
	price, err := t.GetMarketPrice(symbol)
	if err != nil {
		return fmt.Errorf("获取市价失败: %w", err)
	}

	notionalValue := quantity * price
	minNotional := t.GetMinNotional(symbol)

	if notionalValue < minNotional {
		return fmt.Errorf(
			"订单金额 %.2f USDT 低于最小要求 %.2f USDT (数量: %.4f, 价格: %.4f)",
			notionalValue, minNotional, quantity, price,
		)
	}

	return nil
}

// GetSymbolPrecision 获取交易对的数量精度
func (t *FuturesTrader) GetSymbolPrecision(symbol string) (int, error) {
	exchangeInfo, err := t.client.NewExchangeInfoService().Do(context.Background())
	if err != nil {
		return 0, fmt.Errorf("获取交易规则失败: %w", err)
	}

	for _, s := range exchangeInfo.Symbols {
		if s.Symbol == symbol {
			// 从LOT_SIZE filter获取精度
			for _, filter := range s.Filters {
				if filter["filterType"] == "LOT_SIZE" {
					stepSize := filter["stepSize"].(string)
					precision := calculatePrecision(stepSize)
					log.Printf("  %s 数量精度: %d (stepSize: %s)", symbol, precision, stepSize)
					return precision, nil
				}
			}
		}
	}

	log.Printf("  ⚠ %s 未找到精度信息，使用默认精度3", symbol)
	return 3, nil // 默认精度为3
}

// calculatePrecision 从stepSize计算精度
func calculatePrecision(stepSize string) int {
	// 去除尾部的0
	stepSize = trimTrailingZeros(stepSize)

	// 查找小数点
	dotIndex := -1
	for i := 0; i < len(stepSize); i++ {
		if stepSize[i] == '.' {
			dotIndex = i
			break
		}
	}

	// 如果没有小数点或小数点在最后，精度为0
	if dotIndex == -1 || dotIndex == len(stepSize)-1 {
		return 0
	}

	// 返回小数点后的位数
	return len(stepSize) - dotIndex - 1
}

// trimTrailingZeros 去除尾部的0
func trimTrailingZeros(s string) string {
	// 如果没有小数点，直接返回
	if !stringContains(s, ".") {
		return s
	}

	// 从后向前遍历，去除尾部的0
	for len(s) > 0 && s[len(s)-1] == '0' {
		s = s[:len(s)-1]
	}

	// 如果最后一位是小数点，也去掉
	if len(s) > 0 && s[len(s)-1] == '.' {
		s = s[:len(s)-1]
	}

	return s
}

// GetPricePrecision 获取交易对的价格精度
func (t *FuturesTrader) GetPricePrecision(symbol string) (int, error) {
	exchangeInfo, err := t.client.NewExchangeInfoService().Do(context.Background())
	if err != nil {
		return 0, fmt.Errorf("获取交易规则失败: %w", err)
	}

	for _, s := range exchangeInfo.Symbols {
		if s.Symbol == symbol {
			// 从PRICE_FILTER filter获取精度
			for _, filter := range s.Filters {
				if filter["filterType"] == "PRICE_FILTER" {
					tickSize, ok := filter["tickSize"].(string)
					if ok {
						precision := calculatePrecision(tickSize)
						log.Printf("  %s 价格精度: %d (tickSize: %s)", symbol, precision, tickSize)
						return precision, nil
					}
				}
			}
			// 如果没有找到PRICE_FILTER，尝试使用pricePrecision字段
			if s.PricePrecision > 0 {
				log.Printf("  %s 价格精度: %d (pricePrecision)", symbol, s.PricePrecision)
				return s.PricePrecision, nil
			}
		}
	}

	log.Printf("  ⚠ %s 未找到价格精度信息，使用默认精度8", symbol)
	return 8, nil // 默认精度为8
}

// FormatPrice 格式化价格到正确的精度
func (t *FuturesTrader) FormatPrice(symbol string, price float64) (float64, error) {
	precision, err := t.GetPricePrecision(symbol)
	if err != nil {
		// 如果获取失败，使用默认精度8
		precision = 8
	}

	// 使用round函数四舍五入到指定精度
	multiplier := math.Pow10(precision)
	roundedPrice := math.Round(price*multiplier) / multiplier
	return roundedPrice, nil
}

// FormatQuantity 格式化数量到正确的精度
func (t *FuturesTrader) FormatQuantity(symbol string, quantity float64) (string, error) {
	precision, err := t.GetSymbolPrecision(symbol)
	if err != nil {
		// 如果获取失败，使用默认精度3，但仍然使用round函数
		precision = 8
	}

	// 使用round函数四舍五入到指定精度
	multiplier := math.Pow10(precision)
	roundedQuantity := math.Round(quantity*multiplier) / multiplier

	format := fmt.Sprintf("%%.%df", precision)
	return fmt.Sprintf(format, roundedQuantity), nil
}

// GetUserTrades 获取账户成交历史（过去24小时）
// 参考: https://developers.binance.com/docs/zh-CN/derivatives/usds-margined-futures/trade/rest-api/Account-Trade-List
// startTime和endTime是毫秒时间戳，如果都为0则获取最近7天的数据
// symbols是可选参数，如果提供则查询这些symbols，否则查询持仓币种
// 返回格式: map[symbol][]map[string]interface{}
func (t *FuturesTrader) GetUserTrades(startTime, endTime int64, symbols ...string) (map[string][]map[string]interface{}, error) {
	// 如果没有指定时间范围，使用过去24小时
	if startTime == 0 && endTime == 0 {
		endTime = time.Now().UnixMilli()
		startTime = endTime - 24*60*60*1000 // 24小时前
	}

	// 收集需要查询的交易对
	symbolSet := make(map[string]bool)
	
	// 如果提供了symbols参数，使用这些symbols
	if len(symbols) > 0 {
		for _, symbol := range symbols {
			symbolSet[symbol] = true
		}
	} else {
		// 否则，查询持仓币种
		positions, err := t.GetPositions()
		if err != nil {
			return nil, fmt.Errorf("获取持仓失败: %w", err)
		}

		// 收集需要查询的交易对（包括所有持仓币种）
		for _, pos := range positions {
			if symbol, ok := pos["symbol"].(string); ok {
				symbolSet[symbol] = true
			}
		}
	}

	// 如果没有任何交易对，返回空结果
	if len(symbolSet) == 0 {
		return make(map[string][]map[string]interface{}), nil
	}

	// 为每个交易对查询历史订单
	result := make(map[string][]map[string]interface{})
	var wg sync.WaitGroup
	var mutex sync.Mutex

	// 限制并发数，避免API限流
	semaphore := make(chan struct{}, 5)

	for symbol := range symbolSet {
		wg.Add(1)
		semaphore <- struct{}{}

		go func(s string) {
			defer wg.Done()
			defer func() { <-semaphore }()

			trades, err := t.getUserTradesForSymbol(s, startTime, endTime)
			if err != nil {
				log.Printf("⚠️ 获取 %s 的历史订单失败: %v", s, err)
				return
			}

			if len(trades) > 0 {
				mutex.Lock()
				result[s] = trades
				mutex.Unlock()
				log.Printf("✓ 获取 %s 的历史订单: %d 条", s, len(trades))
			}
		}(symbol)
	}

	wg.Wait()
	return result, nil
}

// getUserTradesForSymbol 获取指定交易对的成交历史
func (t *FuturesTrader) getUserTradesForSymbol(symbol string, startTime, endTime int64) ([]map[string]interface{}, error) {
	// 构建请求参数
	params := url.Values{}
	params.Set("symbol", symbol)
	if startTime > 0 {
		params.Set("startTime", strconv.FormatInt(startTime, 10))
	}
	if endTime > 0 {
		params.Set("endTime", strconv.FormatInt(endTime, 10))
	}
	params.Set("limit", "1000") // 最大限制1000
	params.Set("timestamp", strconv.FormatInt(time.Now().UnixMilli(), 10))
	params.Set("recvWindow", "5000")

	// 生成签名（在添加signature之前）
	queryString := params.Encode()
	signature := t.generateSignature(queryString)
	params.Set("signature", signature)

	// 构建请求URL
	baseURL := "https://fapi.binance.com"
	reqURL := fmt.Sprintf("%s/fapi/v1/userTrades?%s", baseURL, params.Encode())

	// 创建HTTP请求
	req, err := http.NewRequest("GET", reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}

	// 设置请求头
	req.Header.Set("X-MBX-APIKEY", t.apiKey)

	// 发送请求
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	// 读取响应
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}

	// 检查响应状态
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API错误 (状态码: %d): %s", resp.StatusCode, string(body))
	}

	// 解析JSON响应
	var trades []map[string]interface{}
	if err := json.Unmarshal(body, &trades); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	// 转换为统一的字段格式（确保数据类型正确）
	result := make([]map[string]interface{}, 0, len(trades))
	for _, trade := range trades {
		normalizedTrade := make(map[string]interface{})
		
		// 复制所有字段
		for k, v := range trade {
			normalizedTrade[k] = v
		}

		// 确保关键字段存在
		if _, ok := normalizedTrade["symbol"]; !ok {
			normalizedTrade["symbol"] = symbol
		}

		result = append(result, normalizedTrade)
	}

	return result, nil
}

// 辅助函数
func contains(s, substr string) bool {
	return len(s) >= len(substr) && stringContains(s, substr)
}

func stringContains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
