package upstream

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
)

// HostOperation 是上游 /provision/default 支持的模块操作方法（func 参数）。
type HostOperation string

// 上游支持的模块方法（取自上游 v3.7.5 app/home/controller/ProvisionController::execute）。
const (
	OpOn           HostOperation = "on"
	OpOff          HostOperation = "off"
	OpReboot       HostOperation = "reboot"
	OpHardOff      HostOperation = "hard_off"
	OpHardReboot   HostOperation = "hard_reboot"
	OpStatus       HostOperation = "status"
	OpVNC          HostOperation = "vnc"
	OpReinstall    HostOperation = "reinstall"
	OpResetPass    HostOperation = "crack_pass"
	OpRescueSystem HostOperation = "rescue_system"
	OpSuspend      HostOperation = "suspend"
	OpUnsuspend    HostOperation = "unsuspend"
)

// CreateHostRequest 是向上游下单开通主机的参数。
type CreateHostRequest struct {
	// ProductID 上游商品 ID（Products 返回的 Product.ID）。
	ProductID int
	// BillingCycle 计费周期，如 monthly / quarterly / annually。
	BillingCycle string
	// Hostname 主机名（上游字段名为 host）。
	Hostname string
	// Password 主机密码。
	Password string
	// Quantity 数量，<=0 时按 1 处理。
	Quantity int
	// CurrencyID 上游货币 ID；<=0 时自动取 /cart/clear 返回的账号货币，再退回 /cart/credit。
	CurrencyID int
	// ConfigOptions 可配置项：键为上游配置项 ID，值为数量（数量型）或上游选项 ID（选项型）。
	ConfigOptions map[int]string
	// CustomFields 自定义字段：键为上游字段 ID，值为字段内容。
	CustomFields map[int]string

	// 以下三个字段用于上游回调下游（主机信息/工单同步），本项目按需填写，留空则不传。
	DownstreamURL   string
	DownstreamToken string
	DownstreamID    int
}

// CreateHostResult 是开通结果。
type CreateHostResult struct {
	// HostID 上游开通出的主机 ID；为 0 表示上游已受理但未回带主机 ID。
	HostID int
	// InvoiceID 上游生成的账单 ID（为 0 表示无需支付，例如零元订单）。
	InvoiceID int
	// Paid 是否已用上游余额支付成功（上游 /apply_credit 返回 status=1001）。
	Paid bool
}

// RenewHostResult 是续费结果。
type RenewHostResult struct {
	// InvoiceID 上游生成的续费账单 ID。
	InvoiceID int
	// Paid 是否已用上游余额支付成功。
	Paid bool
}

// On 开机。
func (c *Client) On(ctx context.Context, hostID int) (*ProvisionResult, error) {
	return c.provision(ctx, hostID, OpOn, nil)
}

// Off 关机（软关机）。
func (c *Client) Off(ctx context.Context, hostID int) (*ProvisionResult, error) {
	return c.provision(ctx, hostID, OpOff, nil)
}

// Reboot 重启（软重启）。
func (c *Client) Reboot(ctx context.Context, hostID int) (*ProvisionResult, error) {
	return c.provision(ctx, hostID, OpReboot, nil)
}

// HardOff 强制关机。
func (c *Client) HardOff(ctx context.Context, hostID int) (*ProvisionResult, error) {
	return c.provision(ctx, hostID, OpHardOff, nil)
}

// HardReboot 强制重启。
func (c *Client) HardReboot(ctx context.Context, hostID int) (*ProvisionResult, error) {
	return c.provision(ctx, hostID, OpHardReboot, nil)
}

// Status 查询电源/主机状态。
func (c *Client) Status(ctx context.Context, hostID int) (*ProvisionResult, error) {
	return c.provision(ctx, hostID, OpStatus, nil)
}

// VNC 获取 VNC 连接信息。
func (c *Client) VNC(ctx context.Context, hostID int) (*ProvisionResult, error) {
	return c.provision(ctx, hostID, OpVNC, nil)
}

// Reinstall 重装系统。osID 为上游操作系统 ID，port 为可选端口（0 表示不传）。
func (c *Client) Reinstall(ctx context.Context, hostID, osID, port int) (*ProvisionResult, error) {
	if osID <= 0 {
		return nil, fmt.Errorf("%w: 重装需要有效的操作系统 ID，收到 %d", ErrBusiness, osID)
	}
	extra := url.Values{}
	extra.Set("os", strconv.Itoa(osID))
	if port > 0 {
		extra.Set("port", strconv.Itoa(port))
	}
	return c.provision(ctx, hostID, OpReinstall, extra)
}

// ResetPassword 重置主机密码。
func (c *Client) ResetPassword(ctx context.Context, hostID int, password string) (*ProvisionResult, error) {
	if password == "" {
		return nil, fmt.Errorf("%w: 重置密码需要非空 password", ErrBusiness)
	}
	extra := url.Values{}
	extra.Set("password", password)
	return c.provision(ctx, hostID, OpResetPass, extra)
}

// RescueSystem 进入救援系统。system 为上游救援系统类型 ID（<=0 时不传，由上游取默认）。
func (c *Client) RescueSystem(ctx context.Context, hostID, system int) (*ProvisionResult, error) {
	extra := url.Values{}
	if system > 0 {
		extra.Set("system", strconv.Itoa(system))
	}
	return c.provision(ctx, hostID, OpRescueSystem, extra)
}

// Suspend 暂停主机。
func (c *Client) Suspend(ctx context.Context, hostID int, reason string) (*ProvisionResult, error) {
	extra := url.Values{}
	if reason != "" {
		extra.Set("reason", reason)
	}
	return c.provision(ctx, hostID, OpSuspend, extra)
}

// Unsuspend 恢复（解除暂停）主机。
func (c *Client) Unsuspend(ctx context.Context, hostID int) (*ProvisionResult, error) {
	return c.provision(ctx, hostID, OpUnsuspend, nil)
}

// CustomButton 触发上游模块自定义按钮方法，对应 POST /provision/button。
func (c *Client) CustomButton(ctx context.Context, hostID int, funcName string) (*ProvisionResult, error) {
	if funcName == "" {
		return nil, fmt.Errorf("%w: 自定义按钮需要非空 func", ErrBusiness)
	}

	params := url.Values{}
	params.Set("id", strconv.Itoa(hostID))
	params.Set("func", funcName)

	resp, err := c.Post(ctx, pathProvisionBtn, params, nil)
	if err != nil {
		return nil, err
	}
	return provisionResultOf(resp), nil
}

// 取消（终止）申请的类型，对应上游 POST /host/cancel 的 type 参数。
const (
	// CancelImmediate 立即取消（上游按自身策略决定是否立即执行终止）。
	CancelImmediate = "Immediate"
	// CancelEndOfBilling 到账单周期结束时取消。
	CancelEndOfBilling = "Endofbilling"
)

// RequestCancel 向上游提交主机取消（终止）申请，对应 POST /host/cancel。
//
// 上游前台没有「直接删除主机」的开放接口，删除需走此申请流程（按上游配置可能需人工审核），
// 因此该方法只保证「申请已被上游受理」，不保证主机立即终止：
// 实测上游以 status=202 + msg=mf_cloud_finance_termination_pending 表示「终止申请待处理」，
// 调用方可据 ProvisionResult.Status 区分 200（已生效）与 202（待上游处理）。
func (c *Client) RequestCancel(ctx context.Context, hostID int, cancelType, reason string) (*ProvisionResult, error) {
	if hostID <= 0 {
		return nil, fmt.Errorf("%w: hostID 必须为正整数，收到 %d", ErrBusiness, hostID)
	}
	if cancelType != CancelImmediate && cancelType != CancelEndOfBilling {
		return nil, fmt.Errorf("%w: cancelType 必须是 %s 或 %s，收到 %q",
			ErrBusiness, CancelImmediate, CancelEndOfBilling, cancelType)
	}
	if reason == "" {
		return nil, fmt.Errorf("%w: 取消申请需要填写 reason", ErrBusiness)
	}

	params := url.Values{}
	params.Set("id", strconv.Itoa(hostID))
	params.Set("type", cancelType)
	params.Set("reason", reason)

	resp, err := c.Post(ctx, pathHostCancel, params, nil)
	if err != nil {
		return nil, err
	}
	return provisionResultOf(resp), nil
}

// provision 是生命周期方法的公共实现，对应 POST /provision/default。
func (c *Client) provision(ctx context.Context, hostID int, op HostOperation, extra url.Values) (*ProvisionResult, error) {
	if hostID <= 0 {
		return nil, fmt.Errorf("%w: hostID 必须为正整数，收到 %d", ErrBusiness, hostID)
	}

	params := url.Values{}
	params.Set("func", string(op))
	params.Set("id", strconv.Itoa(hostID))
	for key, values := range extra {
		for _, value := range values {
			params.Set(key, value)
		}
	}

	resp, err := c.Post(ctx, pathProvisionDef, params, nil)
	if err != nil {
		return nil, err
	}
	return provisionResultOf(resp), nil
}

// provisionResultOf 把上游响应整理为 ProvisionResult（外层状态 + 原始 data）。
func provisionResultOf(resp *Response) *ProvisionResult {
	return &ProvisionResult{
		Status:  resp.Status,
		Msg:     resp.Msg,
		Data:    resp.Data,
		HostIDs: resp.HostIDs(),
	}
}

// CreateHost 在上游下单开通主机（上游没有单独的「开通」接口，须走 下单 → 余额支付 两步）。
//
// 步骤与上游官方下游实现一致（见 docs/api-contract.md 8.3）：
//
//	POST /cart/clear      清理上游购物车，拿到账号货币与待支付账单
//	POST /cart/add_to_shop 按商品与配置项加入购物车（有历史待支付账单时跳过）
//	POST /cart/settle      结算生成账单
//	POST /apply_credit     用上游账号余额支付（status=1001 表示支付成功）
//
// 返回 Paid=false 且 error 非 nil 表示上游已受理但支付未完成（通常是上游余额不足）。
func (c *Client) CreateHost(ctx context.Context, req CreateHostRequest) (*CreateHostResult, error) {
	if req.ProductID <= 0 {
		return nil, fmt.Errorf("%w: ProductID 必须为正整数，收到 %d", ErrBusiness, req.ProductID)
	}
	if req.BillingCycle == "" {
		return nil, fmt.Errorf("%w: BillingCycle 不能为空（如 monthly/quarterly/annually）", ErrBusiness)
	}
	quantity := req.Quantity
	if quantity <= 0 {
		quantity = 1
	}

	result := &CreateHostResult{}

	// 1. 清理上游购物车（同时拿到账号货币与可能存在的历史待支付账单）。
	clearResp, err := c.Post(ctx, pathCartClear, c.downstreamParams(req), nil)
	if err != nil {
		return nil, err
	}
	if ids := clearResp.HostIDs(); len(ids) > 0 {
		result.HostID = ids[0]
	}
	result.InvoiceID = invoiceIDOf(clearResp)

	currencyID := req.CurrencyID
	if currencyID <= 0 && clearResp.User != nil {
		currencyID = clearResp.User.Currency
	}
	if currencyID <= 0 {
		credit, err := c.Credit(ctx)
		if err != nil {
			return nil, err
		}
		currencyID = credit.Currency.ID
	}

	// 2. 已有待支付账单时直接支付，否则重新下单。
	// enough 的取值与上游官方下游实现一致：/cart/clear 已带回账单时 enough=1，
	// 走 add_to_shop + settle 下单时为 0（余额不足时由上游按自有规则处理）。
	enough := true
	if result.InvoiceID <= 0 {
		orderParams := url.Values{}
		orderParams.Set("pid", strconv.Itoa(req.ProductID))
		orderParams.Set("billingcycle", req.BillingCycle)
		orderParams.Set("qty", strconv.Itoa(quantity))
		if req.Hostname != "" {
			orderParams.Set("host", req.Hostname)
		}
		if req.Password != "" {
			orderParams.Set("password", req.Password)
		}
		if currencyID > 0 {
			orderParams.Set("currencyid", strconv.Itoa(currencyID))
		}
		for id, value := range req.ConfigOptions {
			orderParams.Set(fmt.Sprintf("configoption[%d]", id), value)
		}
		for id, value := range req.CustomFields {
			orderParams.Set(fmt.Sprintf("customfield[%d]", id), value)
		}
		for key, values := range c.downstreamParams(req) {
			for _, value := range values {
				orderParams.Set(key, value)
			}
		}

		if _, err := c.Post(ctx, pathCartAddShop, orderParams, nil); err != nil {
			return nil, err
		}

		// 上游 /cart/settle 读取的是 cart_data 数组（配置项键名为 configoptions）。
		settleParams := url.Values{}
		settleParams.Set("cart_data[pid]", strconv.Itoa(req.ProductID))
		settleParams.Set("cart_data[billingcycle]", req.BillingCycle)
		settleParams.Set("cart_data[qty]", strconv.Itoa(quantity))
		if req.Hostname != "" {
			settleParams.Set("cart_data[host]", req.Hostname)
		}
		if req.Password != "" {
			settleParams.Set("cart_data[password]", req.Password)
		}
		if currencyID > 0 {
			settleParams.Set("cart_data[currencyid]", strconv.Itoa(currencyID))
		}
		for id, value := range req.ConfigOptions {
			settleParams.Set(fmt.Sprintf("cart_data[configoptions][%d]", id), value)
		}
		for id, value := range req.CustomFields {
			settleParams.Set(fmt.Sprintf("cart_data[customfield][%d]", id), value)
		}
		for key, values := range c.downstreamParams(req) {
			for _, value := range values {
				settleParams.Set(key, value)
			}
		}

		settleResp, err := c.Post(ctx, pathCartSettle, settleParams, nil)
		if err != nil {
			return nil, err
		}
		if ids := settleResp.HostIDs(); len(ids) > 0 {
			result.HostID = ids[0]
		}
		result.InvoiceID = invoiceIDOf(settleResp)
		enough = false
	}

	// 3. 用上游余额支付。
	if result.InvoiceID <= 0 {
		return result, fmt.Errorf("%w: 上游下单成功但未返回账单 ID，无法确认开通结果", ErrUpstream)
	}

	payResp, err := c.applyCredit(ctx, result.InvoiceID, req, enough)
	if err != nil {
		return result, err
	}
	if payResp.Status != statusPaid {
		return result, &BusinessError{API: pathApplyCredit, Status: payResp.Status,
			Msg: payResp.Msg + "（上游余额支付未完成，通常是余额不足）"}
	}

	result.Paid = true
	if ids := payResp.HostIDs(); len(ids) > 0 {
		result.HostID = ids[0]
	}
	if result.HostID == 0 {
		return result, fmt.Errorf("%w: 上游支付成功但未返回主机 ID", ErrUpstream)
	}
	return result, nil
}

// RenewHost 续费上游主机：先用 /host/renew 生成账单，再用 /apply_credit 余额支付。
// billingCycle 为空时沿用主机当前周期（由上游决定）。
func (c *Client) RenewHost(ctx context.Context, hostID int, billingCycle string) (*RenewHostResult, error) {
	if hostID <= 0 {
		return nil, fmt.Errorf("%w: hostID 必须为正整数，收到 %d", ErrBusiness, hostID)
	}

	params := url.Values{}
	params.Set("hostid", strconv.Itoa(hostID))
	if billingCycle != "" {
		params.Set("billingcycles", billingCycle)
	}

	renewResp, err := c.Post(ctx, pathHostRenew, params, nil)
	if err != nil {
		return nil, err
	}

	result := &RenewHostResult{InvoiceID: invoiceIDOf(renewResp)}
	if result.InvoiceID <= 0 {
		return result, fmt.Errorf("%w: 上游续费下单成功但未返回账单 ID", ErrUpstream)
	}

	payParams := url.Values{}
	payParams.Set("invoiceid", strconv.Itoa(result.InvoiceID))
	payParams.Set("use_credit", "1")

	payResp, err := c.Post(ctx, pathApplyCredit, payParams, nil)
	if err != nil {
		return result, err
	}
	if payResp.Status != statusPaid {
		return result, &BusinessError{API: pathApplyCredit, Status: payResp.Status,
			Msg: payResp.Msg + "（上游余额支付未完成，通常是余额不足）"}
	}

	result.Paid = true
	return result, nil
}

// applyCredit 用上游余额支付账单。enough 对应上游参数 enough（1/0），
// 取值与上游官方下游实现一致：已有账单直接支付传 1，本次下单生成的账单传 0。
func (c *Client) applyCredit(ctx context.Context, invoiceID int, req CreateHostRequest, enough bool) (*Response, error) {
	params := url.Values{}
	params.Set("invoiceid", strconv.Itoa(invoiceID))
	params.Set("use_credit", "1")
	if enough {
		params.Set("enough", "1")
	} else {
		params.Set("enough", "0")
	}
	for key, values := range c.downstreamParams(req) {
		for _, value := range values {
			params.Set(key, value)
		}
	}
	return c.Post(ctx, pathApplyCredit, params, nil)
}

// downstreamParams 构造上游回调下游所需的参数（未配置时返回空集合）。
func (c *Client) downstreamParams(req CreateHostRequest) url.Values {
	params := url.Values{}
	if req.DownstreamURL != "" {
		params.Set("downstream_url", req.DownstreamURL)
	}
	if req.DownstreamToken != "" {
		params.Set("downstream_token", req.DownstreamToken)
	}
	if req.DownstreamID > 0 {
		params.Set("downstream_id", strconv.Itoa(req.DownstreamID))
	}
	return params
}

// invoiceIDOf 解析账单 ID：优先 data.invoiceid，其次顶层 invoiceid（上游两种写法都出现过）。
func invoiceIDOf(resp *Response) int {
	if resp == nil {
		return 0
	}
	if len(resp.Data) > 0 && string(resp.Data) != "null" {
		var body struct {
			InvoiceID json.Number `json:"invoiceid"`
		}
		if err := json.Unmarshal(resp.Data, &body); err == nil && body.InvoiceID.String() != "" {
			if id, err := strconv.Atoi(body.InvoiceID.String()); err == nil && id > 0 {
				return id
			}
		}
	}
	if resp.InvoiceID.String() != "" {
		if id, err := strconv.Atoi(resp.InvoiceID.String()); err == nil && id > 0 {
			return id
		}
	}
	return 0
}
