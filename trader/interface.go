package trader

// Trader 交易器统一接口
// 支持多个交易平台（币安、Hyperliquid等）
type Trader interface {
	// GetBalance 获取账户余额
	GetBalance() (map[string]interface{}, error)

	// GetPositions 获取所有持仓
	GetPositions() ([]map[string]interface{}, error)

	// OpenLong 开多仓
	OpenLong(symbol string, quantity float64, leverage int) (map[string]interface{}, error)

	// OpenShort 开空仓
	OpenShort(symbol string, quantity float64, leverage int) (map[string]interface{}, error)

	// CloseLong 平多仓（quantity=0表示全部平仓）
	CloseLong(symbol string, quantity float64) (map[string]interface{}, error)

	// CloseShort 平空仓（quantity=0表示全部平仓）
	CloseShort(symbol string, quantity float64) (map[string]interface{}, error)

	// SetLeverage 设置杠杆
	SetLeverage(symbol string, leverage int) error

	// SetMarginMode 设置仓位模式 (true=全仓, false=逐仓)
	SetMarginMode(symbol string, isCrossMargin bool) error

	// GetMarketPrice 获取市场价格
	GetMarketPrice(symbol string) (float64, error)

	// SetStopLoss 设置止损单
	SetStopLoss(symbol string, positionSide string, quantity, stopPrice float64) error

	// SetTakeProfit 设置止盈单
	SetTakeProfit(symbol string, positionSide string, quantity, takeProfitPrice float64) error

	// CancelStopLossOrders 仅取消止损单（修复 BUG：调整止损时不删除止盈）
	CancelStopLossOrders(symbol string) error

	// CancelTakeProfitOrders 仅取消止盈单（修复 BUG：调整止盈时不删除止损）
	CancelTakeProfitOrders(symbol string) error

	// CancelAllOrders 取消该币种的所有挂单
	CancelAllOrders(symbol string) error

	// CancelStopOrders 取消该币种的止盈/止损单（用于调整止盈止损位置）
	CancelStopOrders(symbol string) error

	// GetOpenOrders 获取所有挂单（返回 map[symbol][]order，用于清理孤儿订单）
	// 返回格式: map[string][]map[string]interface{}
	// 其中 order 包含: symbol, orderId, type, positionSide 等字段
	GetOpenOrders() (map[string][]map[string]interface{}, error)

	// GetUserTrades 获取账户成交历史（过去24小时）
	// 返回格式: map[symbol][]map[string]interface{}
	// 其中 trade 包含: symbol, orderId, price, qty, quoteQty, realizedPnl, side, positionSide, time 等字段
	GetUserTrades(startTime, endTime int64) (map[string][]map[string]interface{}, error)

	// FormatQuantity 格式化数量到正确的精度
	FormatQuantity(symbol string, quantity float64) (string, error)
}
