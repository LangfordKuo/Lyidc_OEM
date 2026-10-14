package router

import (
	"log/slog"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/pricing"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/response"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/settings"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/store"
)

// referralHandler 处理会员端邀请返佣接口（契约 18.5）。
type referralHandler struct {
	store  *store.Store
	reader *settings.Reader
	logger *slog.Logger
}

// referralOverviewView 是我的返佣概览（GET/PUT /api/v1/referral）。
//
// 口径（契约 18.3 / 18.5）：
//   - enabled 是**会员自己的**邀请返现开关（referral_enabled），不是全局总开关；
//   - invite_code / invite_link 在「从未开通」时为空串（惰性生成，契约 18.5）；
//   - rebate_balance 是**邀请返现余额**（与消费余额 balance 完全独立，可为负）；
//   - effective_percent 是我当前的生效返佣比例：会员级特批优先于全局默认，
//     全局总开关关闭时恒为 0.00（商品级比例随订单而变，不在此处给出）。
type referralOverviewView struct {
	Enabled          bool   `json:"enabled"`
	InviteCode       string `json:"invite_code"`
	InviteLink       string `json:"invite_link"`
	RebateBalance    string `json:"rebate_balance"`
	EffectivePercent string `json:"effective_percent"`
	InviteeCount     int64  `json:"invitee_count"`
	TotalRebate      string `json:"total_rebate"`
}

// updateReferralRequest 是 PUT /api/v1/referral 请求体。
type updateReferralRequest struct {
	Enabled *bool `json:"enabled"`
}

// referralRebateView 是返佣流水对外视图（金额为定点小数字符串）。
type referralRebateView struct {
	ID             uint64 `json:"id"`
	OrderID        uint64 `json:"order_id"`
	OrderTradeNo   string `json:"order_trade_no"`
	OrderType      string `json:"order_type"`
	BuyerID        uint64 `json:"buyer_id"`
	BuyerUsername  string `json:"buyer_username"`
	BaseAmount     string `json:"base_amount"`
	DiscountAmount string `json:"discount_amount"`
	RatioPercent   string `json:"ratio_percent"`
	RatioSource    string `json:"ratio_source"`
	RebateAmount   string `json:"rebate_amount"`
	Status         string `json:"status"`
	Note           string `json:"note"`
	CreatedAt      string `json:"created_at"`
}

// referralRebateListView 是返佣流水分页列表。
type referralRebateListView struct {
	Items    []referralRebateView `json:"items"`
	Page     int                  `json:"page"`
	PageSize int                  `json:"page_size"`
	Total    int64                `json:"total"`
}

// inviteeView 是被邀请人对外视图（含消费/贡献返佣统计）。
type inviteeView struct {
	MemberID    uint64 `json:"member_id"`
	Username    string `json:"username"`
	JoinedAt    string `json:"joined_at"`
	OrderCount  int64  `json:"order_count"`
	TotalSpent  string `json:"total_spent"`
	TotalRebate string `json:"total_rebate"`
}

// inviteeListView 是被邀请人分页列表。
type inviteeListView struct {
	Items    []inviteeView `json:"items"`
	Page     int           `json:"page"`
	PageSize int           `json:"page_size"`
	Total    int64         `json:"total"`
}

// getReferral 处理 GET /api/v1/referral：我的返佣概览。
func (h *referralHandler) getReferral(c *gin.Context) {
	member, ok := memberFromContext(c)
	if !ok {
		response.Fail(c, response.CodeUnauthorized, msgInvalidCredential)
		return
	}
	view, ok := h.overview(c, member)
	if !ok {
		return
	}
	response.Success(c, view)
}

// updateReferral 处理 PUT /api/v1/referral：开通/关闭邀请返现。
// 首次开通时惰性生成唯一邀请码（契约 18.5）。
func (h *referralHandler) updateReferral(c *gin.Context) {
	member, ok := memberFromContext(c)
	if !ok {
		response.Fail(c, response.CodeUnauthorized, msgInvalidCredential)
		return
	}

	var req updateReferralRequest
	if !bindJSON(c, &req) {
		return
	}
	if req.Enabled == nil {
		response.Fail(c, response.CodeInvalidParam, "enabled 必填（true 开通 / false 关闭）")
		return
	}

	ctx := c.Request.Context()
	if *req.Enabled {
		// 惰性生成：首次开通才发邀请码，已生成则复用。
		if _, err := h.store.EnsureInviteCode(ctx, member.ID); err != nil {
			failDB(c, h.logger, err)
			return
		}
	}
	updated, err := h.store.SetMemberReferralEnabled(ctx, member.ID, *req.Enabled)
	if err != nil {
		failDB(c, h.logger, err)
		return
	}
	updatedView, ok := h.overview(c, updated)
	if !ok {
		return
	}
	h.logger.Info("邀请返现开关已更新", "member_id", member.ID, "enabled", *req.Enabled)
	// R18：是否生成邀请码由服务端惰性决定，摘要只记开关动作本身。
	if *req.Enabled {
		setOplogSummary(c, "已开通邀请返现（首次开通会生成邀请码）")
	} else {
		setOplogSummary(c, "已关闭邀请返现")
	}
	response.Success(c, updatedView)
}

// listRebates 处理 GET /api/v1/referral/rebates：我的返佣流水（分页）。
func (h *referralHandler) listRebates(c *gin.Context) {
	member, ok := memberFromContext(c)
	if !ok {
		response.Fail(c, response.CodeUnauthorized, msgInvalidCredential)
		return
	}
	page, pageSize, ok := pagingParams(c)
	if !ok {
		return
	}

	items, total, err := h.store.ListReferralRebates(c.Request.Context(), store.ReferralRebateFilter{
		InviterID: member.ID,
		Page:      page,
		PageSize:  pageSize,
	})
	if err != nil {
		failDB(c, h.logger, err)
		return
	}

	views := make([]referralRebateView, 0, len(items))
	for _, item := range items {
		views = append(views, referralRebateView{
			ID:             item.ID,
			OrderID:        item.OrderID,
			OrderTradeNo:   item.OrderTradeNo,
			OrderType:      item.OrderType,
			BuyerID:        item.BuyerID,
			BuyerUsername:  item.BuyerUsername,
			BaseAmount:     string(item.BaseAmount),
			DiscountAmount: string(item.DiscountAmount),
			RatioPercent:   string(item.RatioPercent),
			RatioSource:    item.RatioSource,
			RebateAmount:   string(item.RebateAmount),
			Status:         item.Status,
			Note:           item.Note,
			CreatedAt:      formatTime(item.CreatedAt),
		})
	}
	response.Success(c, referralRebateListView{
		Items: views, Page: page, PageSize: pageSize, Total: total,
	})
}

// listInvitees 处理 GET /api/v1/referral/invitees：我邀请的会员 + 消费/贡献返佣统计（分页）。
func (h *referralHandler) listInvitees(c *gin.Context) {
	member, ok := memberFromContext(c)
	if !ok {
		response.Fail(c, response.CodeUnauthorized, msgInvalidCredential)
		return
	}
	page, pageSize, ok := pagingParams(c)
	if !ok {
		return
	}

	items, total, err := h.store.ListInvitees(c.Request.Context(), store.InviteeFilter{
		InviterID: member.ID,
		Page:      page,
		PageSize:  pageSize,
	})
	if err != nil {
		failDB(c, h.logger, err)
		return
	}

	views := make([]inviteeView, 0, len(items))
	for _, item := range items {
		views = append(views, inviteeView{
			MemberID:    item.MemberID,
			Username:    item.Username,
			JoinedAt:    formatTime(item.JoinedAt),
			OrderCount:  item.OrderCount,
			TotalSpent:  pricing.FormatAmount(item.TotalSpent),
			TotalRebate: pricing.FormatAmount(item.TotalRebate),
		})
	}
	response.Success(c, inviteeListView{
		Items: views, Page: page, PageSize: pageSize, Total: total,
	})
}

// overview 组装我的返佣概览视图；读设置或聚合失败时写出错误响应并返回 ok=false。
func (h *referralHandler) overview(c *gin.Context, member *model.Member) (referralOverviewView, bool) {
	ctx := c.Request.Context()

	referralState, err := h.reader.Referral(ctx)
	if err != nil {
		failDB(c, h.logger, err)
		return referralOverviewView{}, false
	}
	siteState, err := h.reader.Site(ctx)
	if err != nil {
		failDB(c, h.logger, err)
		return referralOverviewView{}, false
	}
	summary, err := h.store.ReferralSummary(ctx, member.ID)
	if err != nil {
		failDB(c, h.logger, err)
		return referralOverviewView{}, false
	}

	inviteCode := ""
	if member.InviteCode != nil {
		inviteCode = *member.InviteCode
	}

	// 生效比例：会员级特批优先于全局默认；全局总开关关闭时恒为 0.00。
	effective := "0.00"
	if referralState.Value.Enabled {
		if member.RebatePercentOverride != nil {
			if _, err := pricing.ParsePercent(*member.RebatePercentOverride); err == nil {
				effective = *member.RebatePercentOverride
			}
		} else {
			effective = pricing.FormatPercent(referralState.Value.DefaultPercentOr())
		}
	}

	return referralOverviewView{
		Enabled:          member.ReferralEnabled,
		InviteCode:       inviteCode,
		InviteLink:       buildInviteLink(siteState.Value.URL, inviteCode),
		RebateBalance:    string(member.RebateBalance),
		EffectivePercent: effective,
		InviteeCount:     summary.InviteeCount,
		TotalRebate:      pricing.FormatAmount(summary.TotalRebateCents),
	}, true
}

// referralSelfInvite 判断邀请码是否指向「自己」（同用户名或同邮箱，大小写不敏感）。
// 这是能识别的最小面：注册时拿不到设备/实名等更强身份，跨账号自邀只能在邀请人侧事后审计。
func referralSelfInvite(inviter *model.Member, username, email string) bool {
	return strings.EqualFold(inviter.Username, username) || strings.EqualFold(inviter.Email, email)
}
