package router

import (
	"net/http"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/response"
)

// ── 提现 / 转余额集成测试（契约 18.1.1 / 18.5，R10-B）────────────────────
//
// 账务口径：
//   - withdraw「提交即冻结」：提交时 rebate_balance 立即扣减，reject 退回；
//   - convert「审核通过时才换算」：提交不动钱，approve 同一事务两端变动 + 两条 ledger。

// payoutBody 组装提现请求体。
func payoutBody(amount, channel, name, no string) map[string]any {
	return map[string]any{
		"amount":       amount,
		"channel":      channel,
		"account_name": name,
		"account_no":   no,
	}
}

// conversionBody 组装转余额请求体。
func conversionBody(amount string) map[string]any {
	return map[string]any{"amount": amount}
}

// postPayout 发起会员端申请并断言成功，返回申请对象。
func postPayout(t *testing.T, engine http.Handler, token, path string, body map[string]any) payoutView {
	t.Helper()
	rec, envelope := doAPI(t, engine, http.MethodPost, path, token, body)
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("提交申请失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	return decodeData[payoutView](t, envelope)
}

// expectPayoutFail 断言申请被拒（HTTP 400 + 指定业务码），返回 message。
func expectPayoutFail(t *testing.T, engine http.Handler, token, path string,
	body map[string]any, wantCode int) string {
	t.Helper()
	rec, envelope := doAPI(t, engine, http.MethodPost, path, token, body)
	if rec.Code != http.StatusBadRequest || envelope.Code != wantCode {
		t.Fatalf("申请被拒口径不对: HTTP %d code %d，期望 400/%d（body=%s）",
			rec.Code, envelope.Code, wantCode, rec.Body.String())
	}
	return envelope.Message
}

// reviewPayout 调管理端审核接口，返回响应记录器与响应包（不做成功断言）。
func reviewPayout(t *testing.T, engine http.Handler, token, path string,
	body map[string]any) (int, apiEnvelope) {
	t.Helper()
	rec, envelope := doAPI(t, engine, http.MethodPost, path, token, body)
	return rec.Code, envelope
}

// rebateBalanceOf 直接读库取返现余额字符串。
func rebateBalanceOf(t *testing.T, gdb *gorm.DB, memberID uint64) string {
	t.Helper()
	return string(refMemberByID(t, gdb, memberID).RebateBalance)
}

// balanceOf 直接读库取消费余额字符串。
func balanceOf(t *testing.T, gdb *gorm.DB, memberID uint64) string {
	t.Helper()
	return string(refMemberByID(t, gdb, memberID).Balance)
}

// payoutByID 直接读库取出账申请。
func payoutByID(t *testing.T, gdb *gorm.DB, id uint64) *model.ReferralPayout {
	t.Helper()
	var row model.ReferralPayout
	if err := gdb.First(&row, id).Error; err != nil {
		t.Fatalf("查询出账申请 %d 失败: %v", id, err)
	}
	return &row
}

// ledgerRefOf 按 ref_type 查某会员的全部流水（断言「两端各一条」用）。
func ledgerRefOf(t *testing.T, gdb *gorm.DB, memberID uint64, refType string) []model.Ledger {
	t.Helper()
	var rows []model.Ledger
	if err := gdb.Where("member_id = ? AND ref_type = ?", memberID, refType).
		Order("id ASC").Find(&rows).Error; err != nil {
		t.Fatalf("查询流水失败: %v", err)
	}
	return rows
}

// seedReferralAdmin 建一个 admin 管理员并登录，返回 token。
func seedReferralAdmin(t *testing.T, engine http.Handler, gdb *gorm.DB, username string) string {
	t.Helper()
	seedAdmin(t, gdb, username, "admin123456", model.RoleAdmin, model.StatusActive)
	token, _ := loginAdmin(t, engine, username, "admin123456")
	return token
}

// ── 提现：校验 ──────────────────────────────────────────────────────────

func TestReferralWithdrawalValidation(t *testing.T) {
	gdb := testDatabase(t)
	engine := newStage4Engine(t, gdb, "", nil)
	refCouponSetting(t, gdb, true, "20.00", "100.00")

	token, member := memberTokenFor(t, engine, "wdvalid")
	refEnableReferral(t, engine, token)
	refSetRebateBalance(t, gdb, member.ID, "100.00")

	const path = "/api/v1/referral/withdrawals"

	// 金额写法非法（非十进制 / 超两位小数 / 带负号）→ 40001。
	for _, amount := range []string{"abc", "10.005", "-5.00"} {
		expectPayoutFail(t, engine, token, path,
			payoutBody(amount, "wechat", "张三", "6222021234567890"),
			response.CodeInvalidParam)
	}

	// 金额 = 0 → 40002（格式合法但必须 > 0）。
	expectPayoutFail(t, engine, token, path,
		payoutBody("0", "wechat", "张三", "6222021234567890"),
		response.CodeValidationFailed)

	// 渠道非法 → 40001。
	expectPayoutFail(t, engine, token, path, payoutBody("10.00", "paypal", "张三", "6222021234567890"),
		response.CodeInvalidParam)

	// 账号字段空 / 超长 → 40001。
	expectPayoutFail(t, engine, token, path, payoutBody("10.00", "wechat", "  ", "6222021234567890"),
		response.CodeInvalidParam)
	expectPayoutFail(t, engine, token, path, payoutBody("10.00", "wechat", "张三", ""),
		response.CodeInvalidParam)

	// 余额不足 → 40002（message 含可用余额）。
	message := expectPayoutFail(t, engine, token, path,
		payoutBody("100.01", "wechat", "张三", "6222021234567890"),
		response.CodeValidationFailed)
	if !containsAll(message, "余额不足", "100.00") {
		t.Fatalf("余额不足 message = %q，应明确提示可用余额 100.00", message)
	}

	// 合法 → 通过。
	view := postPayout(t, engine, token, path, payoutBody("10.00", "wechat", "张三", "6222021234567890"))
	if view.Amount != "10.00" || view.Channel != "wechat" || view.Status != "pending" {
		t.Fatalf("申请 = %+v，期望 amount=10.00 channel=wechat status=pending", view)
	}
	_ = view
}

func TestReferralNegativeBalanceBlocksPayout(t *testing.T) {
	gdb := testDatabase(t)
	engine := newStage4Engine(t, gdb, "", nil)
	refCouponSetting(t, gdb, true, "20.00", "100.00")

	token, member := memberTokenFor(t, engine, "wddnegative")
	refEnableReferral(t, engine, token)
	refSetRebateBalance(t, gdb, member.ID, "-5.00")

	message := expectPayoutFail(t, engine, token, "/api/v1/referral/withdrawals",
		payoutBody("1.00", "wechat", "张三", "6222021234567890"),
		response.CodeValidationFailed)
	if !containsAll(message, "为负") {
		t.Fatalf("负数余额 message = %q，应明确提示为负", message)
	}
	expectPayoutFail(t, engine, token, "/api/v1/referral/conversions",
		conversionBody("1.00"), response.CodeValidationFailed)
}

// ── 提现：提交即冻结 → 审核通过 → 标记打款 ──────────────────────────────

func TestReferralWithdrawalFullFlow(t *testing.T) {
	gdb := testDatabase(t)
	engine := newStage4Engine(t, gdb, "", nil)
	refCouponSetting(t, gdb, true, "20.00", "100.00")
	adminToken := seedReferralAdmin(t, engine, gdb, "wdadmin")

	token, member := memberTokenFor(t, engine, "wdfull")
	refEnableReferral(t, engine, token)
	refSetRebateBalance(t, gdb, member.ID, "100.00")

	// 1) 提交即冻结：rebate_balance 立刻扣减 + 写负向 ledger。
	view := postPayout(t, engine, token, "/api/v1/referral/withdrawals",
		payoutBody("30.00", "alipay", "李四", "6222021234567890"))
	if got := rebateBalanceOf(t, gdb, member.ID); got != "70.00" {
		t.Fatalf("提交后 rebate_balance = %s，期望 70.00（已冻结 30.00）", got)
	}
	freeze := ledgerRefOf(t, gdb, member.ID, model.LedgerRefReferralWithdrawal)
	if len(freeze) != 1 || freeze[0].Amount != "-30.00" {
		t.Fatalf("冻结流水 = %+v，期望 1 条 -30.00", freeze)
	}
	if freeze[0].BalanceBefore != "100.00" || freeze[0].BalanceAfter != "70.00" {
		t.Fatalf("冻结流水前后值 = %s→%s，期望 100.00→70.00",
			freeze[0].BalanceBefore, freeze[0].BalanceAfter)
	}
	// 敏感字段回显脱敏（契约 18.5）。
	if view.AccountName != "李**" || view.AccountNo != "62****90" {
		t.Fatalf("脱敏回显 = %q/%q，期望 李**/62****90", view.AccountName, view.AccountNo)
	}

	// 2) 审核通过：只改状态，不动余额。
	code, envelope := reviewPayout(t, engine, adminToken,
		"/api/v1/admin/referral/withdrawals/"+itoa(view.ID)+"/review",
		map[string]any{"action": "approve", "review_note": "资料齐全"})
	if code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("审核通过失败: HTTP %d, body=%s", code, envelope.Message)
	}
	if got := rebateBalanceOf(t, gdb, member.ID); got != "70.00" {
		t.Fatalf("通过后 rebate_balance = %s，期望 70.00（钱已冻结，不再动）", got)
	}
	if row := payoutByID(t, gdb, view.ID); row.Status != model.PayoutStatusApproved ||
		row.ReviewedBy == 0 || row.ReviewedAt == nil {
		t.Fatalf("通过后行 = %+v，期望 approved 且有审核人/时间", row)
	}

	// 3) 重复审核幂等：already=true，不重复产生资金动作。
	code, envelope = reviewPayout(t, engine, adminToken,
		"/api/v1/admin/referral/withdrawals/"+itoa(view.ID)+"/review",
		map[string]any{"action": "approve"})
	if code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("重复审核失败: HTTP %d, body=%s", code, envelope.Message)
	}
	result := decodeData[reviewResultView](t, envelope)
	if !result.Already {
		t.Fatalf("重复审核应返回 already=true，实际 %+v", result)
	}
	if got := rebateBalanceOf(t, gdb, member.ID); got != "70.00" {
		t.Fatalf("重复审核后 rebate_balance = %s，期望 70.00", got)
	}
	if len(ledgerRefOf(t, gdb, member.ID, model.LedgerRefReferralWithdrawal)) != 1 {
		t.Fatalf("重复审核产生了多余流水")
	}

	// 4) 标记打款 → paid + paid_at。
	code, envelope = reviewPayout(t, engine, adminToken,
		"/api/v1/admin/referral/withdrawals/"+itoa(view.ID)+"/mark-paid", nil)
	if code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("标记打款失败: HTTP %d, body=%s", code, envelope.Message)
	}
	paid := payoutByID(t, gdb, view.ID)
	if paid.Status != model.PayoutStatusPaid || paid.PaidAt == nil {
		t.Fatalf("打款后行 = %+v，期望 paid 且有 paid_at", paid)
	}
	firstPaidAt := *paid.PaidAt

	// 5) 重复打款幂等：already=true，**不覆盖**首次 paid_at。
	time.Sleep(15 * time.Millisecond)
	code, envelope = reviewPayout(t, engine, adminToken,
		"/api/v1/admin/referral/withdrawals/"+itoa(view.ID)+"/mark-paid", nil)
	if code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("重复打款失败: HTTP %d, body=%s", code, envelope.Message)
	}
	result = decodeData[reviewResultView](t, envelope)
	if !result.Already {
		t.Fatalf("重复打款应返回 already=true")
	}
	again := payoutByID(t, gdb, view.ID)
	if !again.PaidAt.Equal(firstPaidAt) {
		t.Fatalf("paid_at 被覆盖：%v → %v", firstPaidAt, *again.PaidAt)
	}
	if got := rebateBalanceOf(t, gdb, member.ID); got != "70.00" {
		t.Fatalf("打款后 rebate_balance = %s，期望 70.00（冻结额不退回）", got)
	}
}

func TestReferralWithdrawalRejectRefunds(t *testing.T) {
	gdb := testDatabase(t)
	engine := newStage4Engine(t, gdb, "", nil)
	refCouponSetting(t, gdb, true, "20.00", "100.00")
	adminToken := seedReferralAdmin(t, engine, gdb, "wdreject")

	token, member := memberTokenFor(t, engine, "wdrej")
	refEnableReferral(t, engine, token)
	refSetRebateBalance(t, gdb, member.ID, "100.00")

	view := postPayout(t, engine, token, "/api/v1/referral/withdrawals",
		payoutBody("40.00", "wechat", "王五", "6222021234567890"))
	if got := rebateBalanceOf(t, gdb, member.ID); got != "60.00" {
		t.Fatalf("提交后 rebate_balance = %s，期望 60.00", got)
	}

	// 驳回 → 退回冻结额（加回 + 正向 ledger）。
	code, envelope := reviewPayout(t, engine, adminToken,
		"/api/v1/admin/referral/withdrawals/"+itoa(view.ID)+"/review",
		map[string]any{"action": "reject", "review_note": "账号信息不符"})
	if code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("驳回失败: HTTP %d, body=%s", code, envelope.Message)
	}
	if got := rebateBalanceOf(t, gdb, member.ID); got != "100.00" {
		t.Fatalf("驳回后 rebate_balance = %s，期望退回 100.00", got)
	}
	refund := ledgerRefOf(t, gdb, member.ID, model.LedgerRefReferralWithdrawal)
	if len(refund) != 2 || refund[1].Amount != "40.00" {
		t.Fatalf("驳回流水 = %+v，期望 2 条（第二条 +40.00）", refund)
	}

	// 重复驳回幂等：already=true，不再退回。
	code, envelope = reviewPayout(t, engine, adminToken,
		"/api/v1/admin/referral/withdrawals/"+itoa(view.ID)+"/review",
		map[string]any{"action": "reject"})
	if code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("重复驳回失败: HTTP %d, body=%s", code, envelope.Message)
	}
	if result := decodeData[reviewResultView](t, envelope); !result.Already {
		t.Fatalf("重复驳回应返回 already=true")
	}
	if got := rebateBalanceOf(t, gdb, member.ID); got != "100.00" {
		t.Fatalf("重复驳回后 rebate_balance = %s，期望 100.00（不重复退回）", got)
	}
	if len(ledgerRefOf(t, gdb, member.ID, model.LedgerRefReferralWithdrawal)) != 2 {
		t.Fatalf("重复驳回产生了多余流水")
	}
}

func TestReferralWithdrawalStateConflicts(t *testing.T) {
	gdb := testDatabase(t)
	engine := newStage4Engine(t, gdb, "", nil)
	refCouponSetting(t, gdb, true, "20.00", "100.00")
	adminToken := seedReferralAdmin(t, engine, gdb, "wdstate")

	token, member := memberTokenFor(t, engine, "wdst")
	refEnableReferral(t, engine, token)
	refSetRebateBalance(t, gdb, member.ID, "100.00")
	base := "/api/v1/admin/referral/withdrawals/"

	// pending 不能直接标记打款。
	a := postPayout(t, engine, token, "/api/v1/referral/withdrawals",
		payoutBody("10.00", "wechat", "张三", "6222021234567890"))
	if code, env := reviewPayout(t, engine, adminToken, base+itoa(a.ID)+"/mark-paid", nil); code != http.StatusBadRequest {
		t.Fatalf("pending 标记打款 = HTTP %d code %d，期望 400/40002", code, env.Code)
	}

	// 通过后不能驳回。
	reviewPayout(t, engine, adminToken, base+itoa(a.ID)+"/review", map[string]any{"action": "approve"})
	if code, env := reviewPayout(t, engine, adminToken, base+itoa(a.ID)+"/review",
		map[string]any{"action": "reject"}); code != http.StatusBadRequest || env.Code != response.CodeValidationFailed {
		t.Fatalf("approved 后驳回 = HTTP %d code %d，期望 400/40002", code, env.Code)
	}

	// rejected 是终态：不可 approve / mark-paid。
	b := postPayout(t, engine, token, "/api/v1/referral/withdrawals",
		payoutBody("10.00", "wechat", "张三", "6222021234567890"))
	reviewPayout(t, engine, adminToken, base+itoa(b.ID)+"/review", map[string]any{"action": "reject"})
	if code, _ := reviewPayout(t, engine, adminToken, base+itoa(b.ID)+"/review",
		map[string]any{"action": "approve"}); code != http.StatusBadRequest {
		t.Fatalf("rejected 后 approve = HTTP %d，期望 400", code)
	}
	if code, _ := reviewPayout(t, engine, adminToken, base+itoa(b.ID)+"/mark-paid", nil); code != http.StatusBadRequest {
		t.Fatalf("rejected 标记打款 = HTTP %d，期望 400", code)
	}

	// paid 是终态：不可再审。
	reviewPayout(t, engine, adminToken, base+itoa(a.ID)+"/mark-paid", nil)
	if code, _ := reviewPayout(t, engine, adminToken, base+itoa(a.ID)+"/review",
		map[string]any{"action": "approve"}); code != http.StatusBadRequest {
		t.Fatalf("paid 后 approve = HTTP %d，期望 400", code)
	}
}

// ── 转余额：审核通过才换算 ──────────────────────────────────────────────

func TestReferralConversionApproveSwapsBalances(t *testing.T) {
	gdb := testDatabase(t)
	engine := newStage4Engine(t, gdb, "", nil)
	refCouponSetting(t, gdb, true, "20.00", "100.00")
	adminToken := seedReferralAdmin(t, engine, gdb, "cvadmin")

	token, member := memberTokenFor(t, engine, "cvswap")
	refEnableReferral(t, engine, token)
	refSetRebateBalance(t, gdb, member.ID, "100.00")

	// 1) 提交时**不动钱**。
	view := postPayout(t, engine, token, "/api/v1/referral/conversions", conversionBody("40.00"))
	if got := rebateBalanceOf(t, gdb, member.ID); got != "100.00" {
		t.Fatalf("提交后 rebate_balance = %s，期望 100.00（审核通过前不动钱）", got)
	}
	if got := balanceOf(t, gdb, member.ID); got != "0.00" {
		t.Fatalf("提交后 balance = %s，期望 0.00", got)
	}

	// 2) 审核通过 → 同一事务换算：rebate 减、balance 加、两端各一条 ledger。
	code, envelope := reviewPayout(t, engine, adminToken,
		"/api/v1/admin/referral/conversions/"+itoa(view.ID)+"/review",
		map[string]any{"action": "approve", "review_note": "ok"})
	if code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("转换审核失败: HTTP %d, body=%s", code, envelope.Message)
	}
	if got := rebateBalanceOf(t, gdb, member.ID); got != "60.00" {
		t.Fatalf("换算后 rebate_balance = %s，期望 60.00", got)
	}
	if got := balanceOf(t, gdb, member.ID); got != "40.00" {
		t.Fatalf("换算后 balance = %s，期望 40.00", got)
	}

	// 两条 ledger：返现侧 referral_rebate 负向、消费侧 adjust 正向。
	rebateSide := refLedgerOf(t, gdb, member.ID, model.LedgerTypeReferralRebate)
	if len(rebateSide) != 1 || rebateSide[0].Amount != "-40.00" ||
		rebateSide[0].RefType != model.LedgerRefReferralConversion {
		t.Fatalf("返现侧流水 = %+v，期望 1 条 -40.00/referral_conversion", rebateSide)
	}
	if rebateSide[0].BalanceBefore != "100.00" || rebateSide[0].BalanceAfter != "60.00" {
		t.Fatalf("返现侧前后值 = %s→%s，期望 100.00→60.00",
			rebateSide[0].BalanceBefore, rebateSide[0].BalanceAfter)
	}
	consumeSide := refLedgerOf(t, gdb, member.ID, model.LedgerTypeAdjust)
	if len(consumeSide) != 1 || consumeSide[0].Amount != "40.00" ||
		consumeSide[0].RefType != model.LedgerRefReferralConversion {
		t.Fatalf("消费侧流水 = %+v，期望 1 条 +40.00/referral_conversion", consumeSide)
	}
	if consumeSide[0].BalanceBefore != "0.00" || consumeSide[0].BalanceAfter != "40.00" {
		t.Fatalf("消费侧前后值 = %s→%s，期望 0.00→40.00",
			consumeSide[0].BalanceBefore, consumeSide[0].BalanceAfter)
	}

	// 3) 重复审核只生效一次。
	code, envelope = reviewPayout(t, engine, adminToken,
		"/api/v1/admin/referral/conversions/"+itoa(view.ID)+"/review",
		map[string]any{"action": "approve"})
	if code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("重复转换审核失败: HTTP %d, body=%s", code, envelope.Message)
	}
	if result := decodeData[reviewResultView](t, envelope); !result.Already {
		t.Fatalf("重复审核应返回 already=true")
	}
	if got := rebateBalanceOf(t, gdb, member.ID); got != "60.00" || balanceOf(t, gdb, member.ID) != "40.00" {
		t.Fatalf("重复审核后余额被改：%s / %s", rebateBalanceOf(t, gdb, member.ID), balanceOf(t, gdb, member.ID))
	}
	if len(refLedgerOf(t, gdb, member.ID, model.LedgerTypeReferralRebate)) != 1 ||
		len(refLedgerOf(t, gdb, member.ID, model.LedgerTypeAdjust)) != 1 {
		t.Fatalf("重复审核产生了多余流水")
	}
}

func TestReferralConversionRejectLeavesBalances(t *testing.T) {
	gdb := testDatabase(t)
	engine := newStage4Engine(t, gdb, "", nil)
	refCouponSetting(t, gdb, true, "20.00", "100.00")
	adminToken := seedReferralAdmin(t, engine, gdb, "cvreject")

	token, member := memberTokenFor(t, engine, "cvrej")
	refEnableReferral(t, engine, token)
	refSetRebateBalance(t, gdb, member.ID, "100.00")

	view := postPayout(t, engine, token, "/api/v1/referral/conversions", conversionBody("40.00"))
	code, envelope := reviewPayout(t, engine, adminToken,
		"/api/v1/admin/referral/conversions/"+itoa(view.ID)+"/review",
		map[string]any{"action": "reject", "review_note": "不符合条件"})
	if code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("驳回失败: HTTP %d, body=%s", code, envelope.Message)
	}

	// 驳回不动余额（提交时就没扣过）。
	if got := rebateBalanceOf(t, gdb, member.ID); got != "100.00" {
		t.Fatalf("驳回后 rebate_balance = %s，期望 100.00（不动）", got)
	}
	if got := balanceOf(t, gdb, member.ID); got != "0.00" {
		t.Fatalf("驳回后 balance = %s，期望 0.00（不动）", got)
	}
	if len(ledgerRefOf(t, gdb, member.ID, model.LedgerRefReferralConversion)) != 0 {
		t.Fatalf("驳回产生了流水，期望 0 条")
	}
}

func TestReferralConversionAvailableBalanceCountsPending(t *testing.T) {
	gdb := testDatabase(t)
	engine := newStage4Engine(t, gdb, "", nil)
	refCouponSetting(t, gdb, true, "20.00", "100.00")

	token, member := memberTokenFor(t, engine, "cvavail")
	refEnableReferral(t, engine, token)
	refSetRebateBalance(t, gdb, member.ID, "100.00")

	// 在途 convert 60 → 可用 40；再申请 50 被拒，40 放行。
	postPayout(t, engine, token, "/api/v1/referral/conversions", conversionBody("60.00"))
	message := expectPayoutFail(t, engine, token, "/api/v1/referral/conversions",
		conversionBody("50.00"), response.CodeValidationFailed)
	if !containsAll(message, "余额不足") {
		t.Fatalf("可用余额不足 message = %q，应明确提示", message)
	}
	postPayout(t, engine, token, "/api/v1/referral/conversions", conversionBody("40.00"))

	// 在途 withdraw 已冻结，不再占用可用额度：冻结后 rebate=0，可用=0。
	refSetRebateBalance(t, gdb, member.ID, "20.00")
	// 清掉在途 convert，避免干扰。
	if err := gdb.Model(&model.ReferralPayout{}).
		Where("member_id = ? AND kind = ?", member.ID, model.PayoutKindConvert).
		UpdateColumn("status", model.PayoutStatusRejected).Error; err != nil {
		t.Fatalf("清理在途申请失败: %v", err)
	}
	postPayout(t, engine, token, "/api/v1/referral/withdrawals",
		payoutBody("20.00", "wechat", "张三", "6222021234567890"))
	if got := rebateBalanceOf(t, gdb, member.ID); got != "0.00" {
		t.Fatalf("冻结后 rebate_balance = %s，期望 0.00", got)
	}
	// 此时可用 = 0 − 0 = 0，任何金额都被拒。
	expectPayoutFail(t, engine, token, "/api/v1/referral/conversions",
		conversionBody("0.01"), response.CodeValidationFailed)
}

// ── 权限 ────────────────────────────────────────────────────────────────

func TestReferralPayoutPermissions(t *testing.T) {
	gdb := testDatabase(t)
	engine := newStage4Engine(t, gdb, "", nil)
	refCouponSetting(t, gdb, true, "20.00", "100.00")

	seedAdmin(t, gdb, "padmin", "admin123456", model.RoleAdmin, model.StatusActive)
	seedAdmin(t, gdb, "pfinance", "admin123456", model.RoleFinance, model.StatusActive)
	seedAdmin(t, gdb, "psupport", "admin123456", model.RoleSupport, model.StatusActive)
	adminToken, _ := loginAdmin(t, engine, "padmin", "admin123456")
	financeToken, _ := loginAdmin(t, engine, "pfinance", "admin123456")
	supportToken, _ := loginAdmin(t, engine, "psupport", "admin123456")

	token, member := memberTokenFor(t, engine, "pmember")
	refEnableReferral(t, engine, token)
	refSetRebateBalance(t, gdb, member.ID, "100.00")
	view := postPayout(t, engine, token, "/api/v1/referral/withdrawals",
		payoutBody("10.00", "wechat", "张三", "6222021234567890"))
	base := "/api/v1/admin/referral/withdrawals/"

	// 查看类：admin / finance 可读，support 403。
	for _, tc := range []struct {
		name  string
		token string
		code  int
	}{
		{"admin", adminToken, http.StatusOK},
		{"finance", financeToken, http.StatusOK},
		{"support", supportToken, http.StatusForbidden},
	} {
		rec, _ := doAPI(t, engine, http.MethodGet, "/api/v1/admin/referral/withdrawals", tc.token, nil)
		if rec.Code != tc.code {
			t.Fatalf("%s 查看提现列表 = HTTP %d，期望 %d", tc.name, rec.Code, tc.code)
		}
	}

	// 审核与打款：仅 admin。
	for _, tc := range []struct {
		name  string
		token string
		path  string
		body  map[string]any
	}{
		{"finance 审核", financeToken, base + itoa(view.ID) + "/review", map[string]any{"action": "approve"}},
		{"support 审核", supportToken, base + itoa(view.ID) + "/review", map[string]any{"action": "approve"}},
		{"finance 打款", financeToken, base + itoa(view.ID) + "/mark-paid", nil},
		{"support 打款", supportToken, base + itoa(view.ID) + "/mark-paid", nil},
	} {
		rec, _ := doAPI(t, engine, http.MethodPost, tc.path, tc.token, tc.body)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("%s = HTTP %d，期望 403", tc.name, rec.Code)
		}
	}

	// 会员 token 访问管理端一律 401。
	rec, _ := doAPI(t, engine, http.MethodGet, "/api/v1/admin/referral/withdrawals", token, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("会员 token 访问管理端 = HTTP %d，期望 401", rec.Code)
	}

	// 管理员能正常审（收尾，确认不是全被挡）。
	rec, _ = doAPI(t, engine, http.MethodPost, base+itoa(view.ID)+"/review",
		adminToken, map[string]any{"action": "approve"})
	if rec.Code != http.StatusOK {
		t.Fatalf("admin 审核 = HTTP %d，期望 200", rec.Code)
	}
}

// ── 管理端会员特批比例（契约 6.3）───────────────────────────────────────

func TestAdminMemberRebateOverrideTriState(t *testing.T) {
	gdb := testDatabase(t)
	engine := newStage4Engine(t, gdb, "", nil)
	refCouponSetting(t, gdb, true, "5.00", "100.00")
	adminToken := seedReferralAdmin(t, engine, gdb, "ovadmin")

	_, member := memberTokenFor(t, engine, "ovmember")
	path := "/api/v1/admin/members/" + itoa(member.ID)

	// 1) 缺席 = 不改（至少给一个字段，否则 40001）。
	rec, envelope := doAPI(t, engine, http.MethodPut, path, adminToken, map[string]any{})
	if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeInvalidParam {
		t.Fatalf("空更新 = HTTP %d code %d，期望 400/40001", rec.Code, envelope.Code)
	}

	// 2) 字符串 = 设值。
	rec, envelope = doAPI(t, engine, http.MethodPut, path, adminToken,
		map[string]any{"rebate_percent_override": "25.00"})
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("设值失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	view := decodeData[memberView](t, envelope)
	if view.RebatePercentOverride == nil || *view.RebatePercentOverride != "25.00" {
		t.Fatalf("rebate_percent_override = %v，期望 25.00", view.RebatePercentOverride)
	}

	// 3) null = 清空（回到走全局）。
	rec, envelope = doAPI(t, engine, http.MethodPut, path, adminToken,
		map[string]any{"rebate_percent_override": nil})
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("清空失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}
	view = decodeData[memberView](t, envelope)
	if view.RebatePercentOverride != nil {
		t.Fatalf("清空后 = %v，期望 null", *view.RebatePercentOverride)
	}

	// 4) 只传不改字段语义：同一请求里既有设值又再查一次，断言「不改」——
	//    先设 12.00，再发一个只含 status 的请求应保持 12.00（这里用「缺席即不改」的另一半）。
	doAPI(t, engine, http.MethodPut, path, adminToken,
		map[string]any{"rebate_percent_override": "12.00"})
	rec, envelope = doAPI(t, engine, http.MethodPut, path, adminToken,
		map[string]any{"rebate_percent_override": "12.00"})
	view = decodeData[memberView](t, envelope)
	if view.RebatePercentOverride == nil || *view.RebatePercentOverride != "12.00" {
		t.Fatalf("重复设值后 = %v，期望 12.00", view.RebatePercentOverride)
	}
}

func TestAdminMemberRebateOverrideRejectsInvalid(t *testing.T) {
	gdb := testDatabase(t)
	engine := newStage4Engine(t, gdb, "", nil)
	adminToken := seedReferralAdmin(t, engine, gdb, "ovbad")

	_, member := memberTokenFor(t, engine, "ovbadmember")
	path := "/api/v1/admin/members/" + itoa(member.ID)

	// 类型非法（数字）→ 40001。
	rec, envelope := doAPI(t, engine, http.MethodPut, path, adminToken,
		map[string]any{"rebate_percent_override": 25})
	if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeInvalidParam {
		t.Fatalf("数字类型 = HTTP %d code %d，期望 400/40001", rec.Code, envelope.Code)
	}
	// 格式非法（非十进制）→ 40001。
	rec, envelope = doAPI(t, engine, http.MethodPut, path, adminToken,
		map[string]any{"rebate_percent_override": "abc"})
	if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeInvalidParam {
		t.Fatalf("格式非法 = HTTP %d code %d，期望 400/40001", rec.Code, envelope.Code)
	}
	// 越界（> 100）→ 40002。
	rec, envelope = doAPI(t, engine, http.MethodPut, path, adminToken,
		map[string]any{"rebate_percent_override": "150.00"})
	if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeValidationFailed {
		t.Fatalf("越界 = HTTP %d code %d，期望 400/40002", rec.Code, envelope.Code)
	}
	// 负数 → 40002。
	rec, envelope = doAPI(t, engine, http.MethodPut, path, adminToken,
		map[string]any{"rebate_percent_override": "-1.00"})
	if rec.Code != http.StatusBadRequest || envelope.Code != response.CodeValidationFailed {
		t.Fatalf("负数 = HTTP %d code %d，期望 400/40002", rec.Code, envelope.Code)
	}
	// 会员不存在 → 404。
	rec, envelope = doAPI(t, engine, http.MethodPut, "/api/v1/admin/members/999999", adminToken,
		map[string]any{"rebate_percent_override": "10.00"})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("会员不存在 = HTTP %d，期望 404", rec.Code)
	}
	// support → 403。
	seedAdmin(t, gdb, "ovsupport", "admin123456", model.RoleSupport, model.StatusActive)
	supportToken, _ := loginAdmin(t, engine, "ovsupport", "admin123456")
	rec, _ = doAPI(t, engine, http.MethodPut, path, supportToken,
		map[string]any{"rebate_percent_override": "10.00"})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("support 改特批比例 = HTTP %d，期望 403", rec.Code)
	}
}

func TestAdminMemberRebateOverrideAffectsGrantRatio(t *testing.T) {
	gdb := testDatabase(t)
	engine := newStage4Engine(t, gdb, "", nil)
	// 全局默认 5.00，特批 20.00 → 发放时按 20.00（会员级优先于全局）。
	refCouponSetting(t, gdb, true, "5.00", "100.00")
	adminToken := seedReferralAdmin(t, engine, gdb, "ovgrant")
	product := seedOrderProduct(t, gdb, model.ProductStatusOn)

	_, inviter := memberTokenFor(t, engine, "ovinviter")
	buyerToken, buyer := refSeedBoundBuyer(t, engine, gdb, "ovbuyer", inviter.ID)

	// 走管理端接口设特批比例（不是直写库）。
	rec, envelope := doAPI(t, engine, http.MethodPut,
		"/api/v1/admin/members/"+itoa(inviter.ID), adminToken,
		map[string]any{"rebate_percent_override": "20.00"})
	if rec.Code != http.StatusOK || envelope.Code != response.CodeSuccess {
		t.Fatalf("设置特批比例失败: HTTP %d, body=%s", rec.Code, rec.Body.String())
	}

	order := refCreatePaidOrder(t, engine, gdb, buyerToken, buyer.ID, refMonthOrder(product.ID, ""))
	rebate := refRebateByOrder(t, gdb, order.ID)
	if rebate == nil {
		t.Fatalf("未查到返佣流水")
	}
	if string(rebate.RatioPercent) != "20.00" || rebate.RatioSource != model.RebateSourceMember {
		t.Fatalf("特批后发放 = %s/%s，期望 20.00/member", rebate.RatioPercent, rebate.RatioSource)
	}
	if string(rebate.RebateAmount) != "20.00" {
		t.Fatalf("返佣 = %s，期望 20.00", rebate.RebateAmount)
	}

	// 清空特批 → 回到全局 5.00。
	doAPI(t, engine, http.MethodPut, "/api/v1/admin/members/"+itoa(inviter.ID), adminToken,
		map[string]any{"rebate_percent_override": nil})
	order = refCreatePaidOrder(t, engine, gdb, buyerToken, buyer.ID, refMonthOrder(product.ID, ""))
	rebate = refRebateByOrder(t, gdb, order.ID)
	if rebate == nil || string(rebate.RatioPercent) != "5.00" ||
		rebate.RatioSource != model.RebateSourceGlobal {
		t.Fatalf("清空特批后发放 = %+v，期望 5.00/global", rebate)
	}
}
