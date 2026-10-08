package upstream

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
)

// 上游主机/账号相关接口路径（实测见 docs/api-contract.md 8.3）。
const (
	pathCartHostInfo = "/cart/hostinfo"
	pathCartCredit   = "/cart/credit"
	pathCartSummary  = "/cart/summary"
	pathHostCloudOS  = "/host/cloudos"
)

// Hosts 拉取已购产品（主机）列表，对应 GET /cart/hostinfo。
//
// **hostIDs 必须传且非空**：实测（2026-10-08）上游在该参数缺省时返回空列表，
// 并不会「列出账号下全部主机」；要按账号批量取主机需由调用方维护上游主机 ID 清单
// （Accounts/Hosts 的同步策略放在后续阶段）。all 为 true 时请求 all=1，
// 上游会额外回带每个主机的可配置项明细（Host.OptionConfig）。
func (c *Client) Hosts(ctx context.Context, hostIDs []int, all bool) (*HostList, error) {
	params := url.Values{}
	for _, id := range hostIDs {
		if id > 0 {
			// 上游按 PHP 数组接收：hostid[]=1&hostid[]=2
			params.Add("hostid[]", strconv.Itoa(id))
		}
	}
	if all {
		params.Set("all", "1")
	}

	var out HostList
	if _, err := c.Get(ctx, pathCartHostInfo, params, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Host 按上游主机 ID 查询单个主机（内部走 /cart/hostinfo?hostid[]=<id>）。
// 上游没有单主机详情接口，这里在返回列表里按 ID 精确匹配；找不到返回 ErrBusiness。
func (c *Client) Host(ctx context.Context, hostID int) (*Host, error) {
	if hostID <= 0 {
		return nil, fmt.Errorf("%w: hostID 必须为正整数，收到 %d", ErrBusiness, hostID)
	}

	list, err := c.Hosts(ctx, []int{hostID}, true)
	if err != nil {
		return nil, err
	}
	for i := range list.Hosts {
		if list.Hosts[i].ID == hostID {
			return &list.Hosts[i], nil
		}
	}
	return nil, &BusinessError{API: pathCartHostInfo, Status: statusNotLogged,
		Msg: fmt.Sprintf("上游未返回主机 %d（可能不属于该账号）", hostID)}
}

// CloudOS 查询商品可选的操作系统列表，对应 GET /host/cloudos。
//
// osOptionID 是商品「操作系统」可配置项的 ID（ProductConfig 中 OptionName 含 os 的那一项的 ID），
// 用于让上游按商品返回可选系统；填 0 时上游按 productID 处理。
func (c *Client) CloudOS(ctx context.Context, productID, osOptionID int) (*CloudOSList, error) {
	if productID <= 0 {
		return nil, fmt.Errorf("%w: productID 必须为正整数，收到 %d", ErrBusiness, productID)
	}

	params := url.Values{}
	params.Set("productid", strconv.Itoa(productID))
	if osOptionID > 0 {
		params.Set("os_config_option_id", strconv.Itoa(osOptionID))
	}

	var out CloudOSList
	if _, err := c.Get(ctx, pathHostCloudOS, params, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Credit 查询上游账号余额，对应 GET /cart/credit。未登录时上游返回 credit=null（本包会先登录）。
func (c *Client) Credit(ctx context.Context) (*Credit, error) {
	var out Credit
	if _, err := c.Get(ctx, pathCartCredit, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Summary 查询上游 API 概览，对应 GET /cart/summary。
//
// 上游要求站点开启资源 API 且账号已开通 API，否则返回业务失败（status=400，msg=暂未开通API功能）。
// 注意返回体里的 APIPassword 是密钥明文，调用方**不得**写入日志。
func (c *Client) Summary(ctx context.Context) (*Summary, error) {
	var out Summary
	if _, err := c.Get(ctx, pathCartSummary, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
