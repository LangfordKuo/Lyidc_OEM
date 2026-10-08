package router

import (
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/store"
)

// 本地单号规则（契约 12.2.5）：<前缀> + UTC 时间（yyyyMMddHHmmss）+ 6 位随机大写字母/数字，
// 如 O20261008143015K7Q2ZP。前缀 O=订单 / R=充值单，同时也是渠道 out_trade_no——
// 支付回调据此判定命中哪张表（契约 12.2.3）。
const (
	tradeNoRandomLength = 6
	// tradeNoTimeLayout 是单号里的时间部分（UTC）。
	tradeNoTimeLayout = "20060102150405"
	// maxTradeNoAttempts 是单号唯一键冲突时的最大尝试次数。
	maxTradeNoAttempts = 5
)

// tradeNoAlphabet 是随机部分的字符集（去掉易混淆字母 I/O，仍属 [0-9A-Z]）。
const tradeNoAlphabet = "0123456789ABCDEFGHJKLMNPQRSTUVWXYZ"

// createWithTradeNo 生成本地单号并落库；唯一键冲突（ErrTradeNoTaken）时换号重试，
// 最多 maxTradeNoAttempts 次；其余错误原样返回。
func createWithTradeNo[T any](prefix string, create func(tradeNo string) (T, error)) (T, error) {
	var zero T
	for attempt := 0; attempt < maxTradeNoAttempts; attempt++ {
		tradeNo, err := newTradeNo(prefix)
		if err != nil {
			return zero, err
		}
		value, err := create(tradeNo)
		if err == nil {
			return value, nil
		}
		if !errors.Is(err, store.ErrTradeNoTaken) {
			return zero, err
		}
	}
	return zero, fmt.Errorf("%w: 连续 %d 次生成的单号都已存在", store.ErrTradeNoTaken, maxTradeNoAttempts)
}

// newTradeNo 生成本地单号。
func newTradeNo(prefix string) (string, error) {
	random, err := randomTradeNoToken(tradeNoRandomLength)
	if err != nil {
		return "", err
	}
	return prefix + time.Now().UTC().Format(tradeNoTimeLayout) + random, nil
}

// randomTradeNoToken 用 crypto/rand 生成指定长度的随机串。
func randomTradeNoToken(length int) (string, error) {
	limit := big.NewInt(int64(len(tradeNoAlphabet)))
	out := make([]byte, length)
	for i := range out {
		index, err := rand.Int(rand.Reader, limit)
		if err != nil {
			return "", fmt.Errorf("生成随机单号失败: %w", err)
		}
		out[i] = tradeNoAlphabet[index.Int64()]
	}
	return string(out), nil
}
