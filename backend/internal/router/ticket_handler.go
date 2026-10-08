package router

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/response"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/store"
)

// 工单相关提示文案（契约 16.3）。
const (
	msgTicketMissing = "工单不存在"
	msgTicketClosed  = "工单已关闭，如需继续请新开工单"
	// msgTicketAuthorUnknown 是作者账号已不存在时的 author_name 占位符。
	msgTicketAuthorUnknown = "-"
)

// ticketHandler 处理工单接口：会员端提单/列表/详情/回复/关闭，
// 管理端（admin + support）列表/详情/回复（含内部备注）/关闭（阶段 6a）。
//
// 通知接线（阶段 6b，契约 17.4）：本 handler 是工单事件的唯一产生处——创建、会员回复、
// 客服公开回复、**客服关闭**落库后调用 notify 投递通知（异步、失败不影响工单业务）。
// 不产生通知的路径：管理员**内部备注**（不对外）、会员自行关闭（定稿：只有客服关闭才通知会员）。
type ticketHandler struct {
	store  *store.Store
	notify NotificationTrigger
	logger *slog.Logger
}

// createTicketRequest 是 POST /api/v1/tickets 请求体。
type createTicketRequest struct {
	Subject    string  `json:"subject"`
	Content    string  `json:"content"`
	Category   string  `json:"category"`
	InstanceID *uint64 `json:"instance_id"`
}

// replyTicketRequest 是会员端回复请求体。
type replyTicketRequest struct {
	Content string `json:"content"`
}

// adminReplyTicketRequest 是管理端回复请求体：Internal=true 表示内部备注（会员端不可见）。
type adminReplyTicketRequest struct {
	Content  string `json:"content"`
	Internal bool   `json:"internal"`
}

// ticketInstanceView 是工单关联实例的**概要**（不含主机账号密码等敏感字段）。
type ticketInstanceView struct {
	ID          uint64 `json:"id"`
	Name        string `json:"name"`
	ProductName string `json:"product_name"`
	Status      string `json:"status"`
}

// ticketMemberView 是工单会员概要（仅管理端输出）。
type ticketMemberView struct {
	ID       uint64 `json:"id"`
	Username string `json:"username"`
	Nickname string `json:"nickname"`
}

// ticketView 是工单对外视图（会员端与管理端共用部分）。
type ticketView struct {
	ID          uint64              `json:"id"`
	TradeNo     string              `json:"trade_no"`
	Subject     string              `json:"subject"`
	Category    string              `json:"category"`
	Status      string              `json:"status"`
	InstanceID  *uint64             `json:"instance_id"`
	Instance    *ticketInstanceView `json:"instance"`
	LastReplyAt string              `json:"last_reply_at"`
	ClosedAt    *string             `json:"closed_at"`
	CreatedAt   string              `json:"created_at"`
	UpdatedAt   string              `json:"updated_at"`
}

// adminTicketView 是管理端工单视图：多会员概要。
type adminTicketView struct {
	ticketView
	MemberID uint64           `json:"member_id"`
	Member   ticketMemberView `json:"member"`
}

// ticketMessageView 是工单消息视图。
//
// Internal 仅在管理端可能为 true：会员端接口既不返回内部备注消息，
// 也不在任何响应中输出 internal=true（newTicketMessageViews 统一过滤，契约 16.3）。
type ticketMessageView struct {
	ID         uint64 `json:"id"`
	AuthorType string `json:"author_type"`
	AuthorID   uint64 `json:"author_id"`
	AuthorName string `json:"author_name"`
	Content    string `json:"content"`
	Internal   bool   `json:"internal"`
	CreatedAt  string `json:"created_at"`
}

// ticketDetailView 是会员端工单详情（消息流**不含**内部备注）。
type ticketDetailView struct {
	ticketView
	Messages []ticketMessageView `json:"messages"`
}

// adminTicketDetailView 是管理端工单详情（消息流**含**内部备注）。
type adminTicketDetailView struct {
	adminTicketView
	Messages []ticketMessageView `json:"messages"`
}

// ticketListView 是会员端工单分页列表。
type ticketListView struct {
	Items    []ticketView `json:"items"`
	Page     int          `json:"page"`
	PageSize int          `json:"page_size"`
	Total    int64        `json:"total"`
}

// adminTicketListView 是管理端工单分页列表。
type adminTicketListView struct {
	Items    []adminTicketView `json:"items"`
	Page     int               `json:"page"`
	PageSize int               `json:"page_size"`
	Total    int64             `json:"total"`
}

// ticketReplyView 是会员端回复响应。
type ticketReplyView struct {
	Ticket  ticketView        `json:"ticket"`
	Message ticketMessageView `json:"message"`
}

// adminTicketReplyView 是管理端回复响应。
type adminTicketReplyView struct {
	Ticket  adminTicketView   `json:"ticket"`
	Message ticketMessageView `json:"message"`
}

// ticketCloseView 是会员端关闭响应（already_closed 表示本次是幂等重复关闭）。
type ticketCloseView struct {
	Ticket        ticketView `json:"ticket"`
	AlreadyClosed bool       `json:"already_closed"`
}

// adminTicketCloseView 是管理端关闭响应。
type adminTicketCloseView struct {
	Ticket        adminTicketView `json:"ticket"`
	AlreadyClosed bool            `json:"already_closed"`
}

// createTicket 处理 POST /api/v1/tickets：会员提单（契约 16.3）。
//
// 校验顺序：字段校验 → 实例归属（防越权关联，他人实例统一 404）→ 未关闭工单上限 → 落库。
func (h *ticketHandler) createTicket(c *gin.Context) {
	member, ok := memberFromContext(c)
	if !ok {
		response.Fail(c, response.CodeUnauthorized, msgInvalidCredential)
		return
	}

	var req createTicketRequest
	if !bindJSON(c, &req) {
		return
	}
	subject, err := validateTicketSubject(req.Subject)
	if err != nil {
		failRuleError(c, err)
		return
	}
	content, err := validateTicketContent(req.Content)
	if err != nil {
		failRuleError(c, err)
		return
	}
	category := strings.TrimSpace(req.Category)
	if !model.IsValidTicketCategory(category) {
		response.Fail(c, response.CodeInvalidParam,
			fmt.Sprintf("category 只能是 %s", strings.Join(model.TicketCategories, " / ")))
		return
	}

	ctx := c.Request.Context()
	var instance *model.Instance
	if req.InstanceID != nil {
		if *req.InstanceID == 0 {
			response.Fail(c, response.CodeInvalidParam, "instance_id 必须为正整数（本地实例 ID）")
			return
		}
		// 只允许关联**本人**实例：他人实例与不存在的实例统一 404（不暴露他人实例的存在性）。
		instance, err = h.store.InstanceByIDForMember(ctx, *req.InstanceID, member.ID)
		switch {
		case errors.Is(err, store.ErrNotFound):
			response.Fail(c, response.CodeNotFound, msgInstanceMissing)
			return
		case err != nil:
			failDB(c, h.logger, err)
			return
		}
	}

	// 限流（契约 16.4 第 1 条）：未关闭工单（open / replied）数达上限即拒绝。
	count, err := h.store.CountOpenTickets(ctx, member.ID)
	if err != nil {
		failDB(c, h.logger, err)
		return
	}
	if count >= maxOpenTicketsPerMember {
		response.Fail(c, response.CodeValidationFailed,
			fmt.Sprintf("未关闭工单数已达上限 %d，请先关闭既有工单", maxOpenTicketsPerMember))
		return
	}

	created, err := createWithTradeNo(model.TicketTradeNoPrefix, func(tradeNo string) (ticketCreation, error) {
		ticket, message, err := h.store.CreateTicketWithMessage(ctx, store.TicketInput{
			TradeNo:    tradeNo,
			MemberID:   member.ID,
			InstanceID: req.InstanceID,
			Category:   category,
			Subject:    subject,
			Content:    content,
		})
		return ticketCreation{ticket: ticket, message: message}, err
	})
	if err != nil {
		failDB(c, h.logger, err)
		return
	}

	h.logger.Info("工单已创建", "ticket_id", created.ticket.ID, "trade_no", created.ticket.TradeNo,
		"member_id", member.ID, "category", category, "instance_id", instanceIDRef(req.InstanceID))
	// 6b 通知接线：工单创建 → 通知客服（站内扇出 admin+support；邮件发站点 admin_email）。
	h.ticketCreated(created.ticket.ID)

	names := map[ticketAuthorKey]string{
		{AuthorType: model.TicketAuthorMember, AuthorID: member.ID}: member.Username,
	}
	response.Success(c, ticketReplyView{
		Ticket:  newTicketView(created.ticket, instance),
		Message: newTicketMessageView(created.message, names),
	})
}

// ticketCreation 是 createWithTradeNo 的返回载荷（工单 + 首条消息）。
type ticketCreation struct {
	ticket  *model.Ticket
	message *model.TicketMessage
}

// listMyTickets 处理 GET /api/v1/tickets：本人工单分页（最近活动在前），可按 status 过滤。
func (h *ticketHandler) listMyTickets(c *gin.Context) {
	member, ok := memberFromContext(c)
	if !ok {
		response.Fail(c, response.CodeUnauthorized, msgInvalidCredential)
		return
	}

	page, pageSize, ok := ticketPaging(c)
	if !ok {
		return
	}
	status, ok := ticketStatusQuery(c)
	if !ok {
		return
	}

	items, total, err := h.store.ListTickets(c.Request.Context(), store.TicketFilter{
		MemberID: member.ID,
		Status:   status,
		Page:     page,
		PageSize: pageSize,
	})
	if err != nil {
		failDB(c, h.logger, err)
		return
	}

	instances, err := h.instancesOfTickets(c.Request.Context(), items)
	if err != nil {
		failDB(c, h.logger, err)
		return
	}
	views := make([]ticketView, 0, len(items))
	for i := range items {
		views = append(views, newTicketView(&items[i], instances[instanceKeyOf(&items[i])]))
	}
	response.Success(c, ticketListView{Items: views, Page: page, PageSize: pageSize, Total: total})
}

// getMyTicket 处理 GET /api/v1/tickets/:id：本人工单详情 + 消息流（**不含内部备注**）。
func (h *ticketHandler) getMyTicket(c *gin.Context) {
	member, ok := memberFromContext(c)
	if !ok {
		response.Fail(c, response.CodeUnauthorized, msgInvalidCredential)
		return
	}
	id, ok := ticketIDParam(c)
	if !ok {
		return
	}

	ctx := c.Request.Context()
	ticket, err := h.store.TicketByIDForMember(ctx, id, member.ID)
	switch {
	case errors.Is(err, store.ErrNotFound):
		response.Fail(c, response.CodeNotFound, msgTicketMissing)
		return
	case err != nil:
		failDB(c, h.logger, err)
		return
	}

	// includeInternal=false：内部备注在查询层就被过滤（视图层再兜底一次）。
	messages, err := h.store.ListTicketMessages(ctx, ticket.ID, false)
	if err != nil {
		failDB(c, h.logger, err)
		return
	}
	names, err := ticketAuthorNames(ctx, h.store, messages)
	if err != nil {
		failDB(c, h.logger, err)
		return
	}
	instance, err := h.instanceOfTicket(ctx, ticket)
	if err != nil {
		failDB(c, h.logger, err)
		return
	}

	response.Success(c, ticketDetailView{
		ticketView: newTicketView(ticket, instance),
		Messages:   newTicketMessageViews(messages, names, false),
	})
}

// replyMyTicket 处理 POST /api/v1/tickets/:id/reply：会员回复（状态回 open）。
func (h *ticketHandler) replyMyTicket(c *gin.Context) {
	member, ok := memberFromContext(c)
	if !ok {
		response.Fail(c, response.CodeUnauthorized, msgInvalidCredential)
		return
	}
	id, ok := ticketIDParam(c)
	if !ok {
		return
	}
	var req replyTicketRequest
	if !bindJSON(c, &req) {
		return
	}
	content, err := validateTicketContent(req.Content)
	if err != nil {
		failRuleError(c, err)
		return
	}

	ctx := c.Request.Context()
	ticket, err := h.store.TicketByIDForMember(ctx, id, member.ID)
	switch {
	case errors.Is(err, store.ErrNotFound):
		response.Fail(c, response.CodeNotFound, msgTicketMissing)
		return
	case err != nil:
		failDB(c, h.logger, err)
		return
	}

	updated, message, err := h.store.AppendTicketReply(ctx, ticket.ID, store.TicketReplyInput{
		AuthorType: model.TicketAuthorMember,
		AuthorID:   member.ID,
		Content:    content,
		Internal:   false,
		NewStatus:  model.TicketStatusOpen,
	})
	switch {
	case errors.Is(err, store.ErrStateConflict):
		response.Fail(c, response.CodeValidationFailed, msgTicketClosed)
		return
	case errors.Is(err, store.ErrNotFound):
		response.Fail(c, response.CodeNotFound, msgTicketMissing)
		return
	case err != nil:
		failDB(c, h.logger, err)
		return
	}

	h.logger.Info("工单收到会员回复", "ticket_id", updated.ID, "trade_no", updated.TradeNo,
		"member_id", member.ID, "status", updated.Status)
	// 6b 通知接线：会员回复 → 通知客服（ticket_replied 的管理端侧）。
	h.ticketRepliedByMember(updated.ID)

	instance, err := h.instanceOfTicket(ctx, updated)
	if err != nil {
		failDB(c, h.logger, err)
		return
	}
	names := map[ticketAuthorKey]string{
		{AuthorType: model.TicketAuthorMember, AuthorID: member.ID}: member.Username,
	}
	response.Success(c, ticketReplyView{
		Ticket:  newTicketView(updated, instance),
		Message: newTicketMessageView(message, names),
	})
}

// closeMyTicket 处理 POST /api/v1/tickets/:id/close：会员关闭自己的工单（幂等）。
func (h *ticketHandler) closeMyTicket(c *gin.Context) {
	member, ok := memberFromContext(c)
	if !ok {
		response.Fail(c, response.CodeUnauthorized, msgInvalidCredential)
		return
	}
	id, ok := ticketIDParam(c)
	if !ok {
		return
	}

	ctx := c.Request.Context()
	ticket, err := h.store.TicketByIDForMember(ctx, id, member.ID)
	switch {
	case errors.Is(err, store.ErrNotFound):
		response.Fail(c, response.CodeNotFound, msgTicketMissing)
		return
	case err != nil:
		failDB(c, h.logger, err)
		return
	}
	h.closeTicket(c, ticket, model.TicketAuthorMember, member.ID)
}

// listAdminTickets 处理 GET /api/v1/admin/tickets：全站工单分页（status/category/member_id/keyword 筛选）。
func (h *ticketHandler) listAdminTickets(c *gin.Context) {
	page, pageSize, ok := ticketPaging(c)
	if !ok {
		return
	}
	status, ok := ticketStatusQuery(c)
	if !ok {
		return
	}
	category := strings.TrimSpace(c.Query("category"))
	if category != "" && !model.IsValidTicketCategory(category) {
		response.Fail(c, response.CodeInvalidParam,
			fmt.Sprintf("category 只能是 %s", strings.Join(model.TicketCategories, " / ")))
		return
	}
	memberID, err := parseUint64Query("member_id", c.Query("member_id"))
	if err != nil {
		failRuleError(c, err)
		return
	}

	ctx := c.Request.Context()
	items, total, err := h.store.ListTickets(ctx, store.TicketFilter{
		MemberID: memberID,
		Status:   status,
		Category: category,
		Keyword:  strings.TrimSpace(c.Query("keyword")),
		Page:     page,
		PageSize: pageSize,
	})
	if err != nil {
		failDB(c, h.logger, err)
		return
	}

	instances, err := h.instancesOfTickets(ctx, items)
	if err != nil {
		failDB(c, h.logger, err)
		return
	}
	members, err := h.membersOfTickets(ctx, items)
	if err != nil {
		failDB(c, h.logger, err)
		return
	}

	views := make([]adminTicketView, 0, len(items))
	for i := range items {
		views = append(views, newAdminTicketView(&items[i],
			instances[instanceKeyOf(&items[i])], members[items[i].MemberID]))
	}
	response.Success(c, adminTicketListView{Items: views, Page: page, PageSize: pageSize, Total: total})
}

// getAdminTicket 处理 GET /api/v1/admin/tickets/:id：工单详情 + **完整**消息流（含内部备注）。
func (h *ticketHandler) getAdminTicket(c *gin.Context) {
	id, ok := ticketIDParam(c)
	if !ok {
		return
	}

	ctx := c.Request.Context()
	ticket, err := h.store.TicketByID(ctx, id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		response.Fail(c, response.CodeNotFound, msgTicketMissing)
		return
	case err != nil:
		failDB(c, h.logger, err)
		return
	}

	messages, err := h.store.ListTicketMessages(ctx, ticket.ID, true)
	if err != nil {
		failDB(c, h.logger, err)
		return
	}
	names, err := ticketAuthorNames(ctx, h.store, messages)
	if err != nil {
		failDB(c, h.logger, err)
		return
	}
	instance, err := h.instanceOfTicket(ctx, ticket)
	if err != nil {
		failDB(c, h.logger, err)
		return
	}
	member, err := h.memberOfTicket(ctx, ticket)
	if err != nil {
		failDB(c, h.logger, err)
		return
	}

	response.Success(c, adminTicketDetailView{
		adminTicketView: newAdminTicketView(ticket, instance, member),
		Messages:        newTicketMessageViews(messages, names, true),
	})
}

// adminReplyTicket 处理 POST /api/v1/admin/tickets/:id/reply：客服回复或内部备注。
//
// internal=false → 状态转 replied（待会员）；internal=true → **状态不变**（契约 16.2 定稿第 2 条）。
func (h *ticketHandler) adminReplyTicket(c *gin.Context) {
	admin, ok := adminFromContext(c)
	if !ok {
		response.Fail(c, response.CodeUnauthorized, msgInvalidCredential)
		return
	}
	id, ok := ticketIDParam(c)
	if !ok {
		return
	}
	var req adminReplyTicketRequest
	if !bindJSON(c, &req) {
		return
	}
	content, err := validateTicketContent(req.Content)
	if err != nil {
		failRuleError(c, err)
		return
	}

	ctx := c.Request.Context()
	ticket, err := h.store.TicketByID(ctx, id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		response.Fail(c, response.CodeNotFound, msgTicketMissing)
		return
	case err != nil:
		failDB(c, h.logger, err)
		return
	}

	newStatus := model.TicketStatusReplied
	if req.Internal {
		newStatus = "" // 内部备注不改变状态（只推进 last_reply_at）
	}
	updated, message, err := h.store.AppendTicketReply(ctx, ticket.ID, store.TicketReplyInput{
		AuthorType: model.TicketAuthorAdmin,
		AuthorID:   admin.ID,
		Content:    content,
		Internal:   req.Internal,
		NewStatus:  newStatus,
	})
	switch {
	case errors.Is(err, store.ErrStateConflict):
		response.Fail(c, response.CodeValidationFailed, msgTicketClosed)
		return
	case errors.Is(err, store.ErrNotFound):
		response.Fail(c, response.CodeNotFound, msgTicketMissing)
		return
	case err != nil:
		failDB(c, h.logger, err)
		return
	}

	h.logger.Info("工单收到管理员回复", "ticket_id", updated.ID, "trade_no", updated.TradeNo,
		"admin_id", admin.ID, "role", admin.Role, "internal", req.Internal, "status", updated.Status)
	// 6b 通知接线：管理员**公开**回复 → 通知会员（ticket_replied 的会员侧）；
	// 内部备注（internal=true）不产生任何对外通知。
	if !req.Internal {
		h.ticketRepliedByAdmin(updated.ID)
	}

	instance, err := h.instanceOfTicket(ctx, updated)
	if err != nil {
		failDB(c, h.logger, err)
		return
	}
	member, err := h.memberOfTicket(ctx, updated)
	if err != nil {
		failDB(c, h.logger, err)
		return
	}
	names := map[ticketAuthorKey]string{
		{AuthorType: model.TicketAuthorAdmin, AuthorID: admin.ID}: admin.Username,
	}
	response.Success(c, adminTicketReplyView{
		Ticket:  newAdminTicketView(updated, instance, member),
		Message: newTicketMessageView(message, names),
	})
}

// adminCloseTicket 处理 POST /api/v1/admin/tickets/:id/close：客服关闭工单（幂等）。
func (h *ticketHandler) adminCloseTicket(c *gin.Context) {
	admin, ok := adminFromContext(c)
	if !ok {
		response.Fail(c, response.CodeUnauthorized, msgInvalidCredential)
		return
	}
	id, ok := ticketIDParam(c)
	if !ok {
		return
	}

	ctx := c.Request.Context()
	ticket, err := h.store.TicketByID(ctx, id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		response.Fail(c, response.CodeNotFound, msgTicketMissing)
		return
	case err != nil:
		failDB(c, h.logger, err)
		return
	}
	h.closeTicket(c, ticket, model.TicketAuthorAdmin, admin.ID)
}

// closeTicket 是会员端/管理端共用的关闭实现：幂等关闭并按视角组装响应。
// authorType/authorID 用于日志与「客服关闭才通知会员」的分支判定。
func (h *ticketHandler) closeTicket(c *gin.Context, ticket *model.Ticket, authorType string, authorID uint64) {
	ctx := c.Request.Context()
	updated, changed, err := h.store.CloseTicket(ctx, ticket.ID)
	if err != nil {
		failDB(c, h.logger, err)
		return
	}

	h.logger.Info("工单已关闭", "ticket_id", updated.ID, "trade_no", updated.TradeNo,
		"author_type", authorType, "author_id", authorID, "already_closed", !changed)
	// 6b 通知接线：**客服**关闭 → 通知会员（契约 17.4 定稿；会员自行关闭不通知客服，
	// 幂等重复关闭同样不再打扰会员）。
	if authorType == model.TicketAuthorAdmin && changed {
		h.ticketClosedByAdmin(updated.ID)
	}

	instance, err := h.instanceOfTicket(ctx, updated)
	if err != nil {
		failDB(c, h.logger, err)
		return
	}
	member, err := h.memberOfTicket(ctx, updated)
	if err != nil {
		failDB(c, h.logger, err)
		return
	}

	if authorType == model.TicketAuthorAdmin {
		response.Success(c, adminTicketCloseView{
			Ticket:        newAdminTicketView(updated, instance, member),
			AlreadyClosed: !changed,
		})
		return
	}
	response.Success(c, ticketCloseView{
		Ticket:        newTicketView(updated, instance),
		AlreadyClosed: !changed,
	})
}

// ---------------------------------------------------------------------------
// 查询辅助：批量取实例 / 会员概要，解析消息作者名
// ---------------------------------------------------------------------------

// instanceKeyOf 返回工单关联实例的主键（未关联为 0）。
func instanceKeyOf(ticket *model.Ticket) uint64 {
	if ticket.InstanceID == nil {
		return 0
	}
	return *ticket.InstanceID
}

// instancesOfTickets 批量取工单关联实例（一次查询组装列表概要）。
func (h *ticketHandler) instancesOfTickets(ctx context.Context, tickets []model.Ticket) (map[uint64]*model.Instance, error) {
	ids := make([]uint64, 0, len(tickets))
	for i := range tickets {
		if key := instanceKeyOf(&tickets[i]); key != 0 {
			ids = append(ids, key)
		}
	}
	items, err := h.store.InstancesByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	byID := make(map[uint64]*model.Instance, len(items))
	for i := range items {
		byID[items[i].ID] = &items[i]
	}
	return byID, nil
}

// instanceOfTicket 取单个工单的关联实例（未关联返回 nil,nil）。
func (h *ticketHandler) instanceOfTicket(ctx context.Context, ticket *model.Ticket) (*model.Instance, error) {
	if instanceKeyOf(ticket) == 0 {
		return nil, nil
	}
	byID, err := h.instancesOfTickets(ctx, []model.Ticket{*ticket})
	if err != nil {
		return nil, err
	}
	return byID[instanceKeyOf(ticket)], nil
}

// membersOfTickets 批量取工单所属会员（管理端视图用）。
func (h *ticketHandler) membersOfTickets(ctx context.Context, tickets []model.Ticket) (map[uint64]*model.Member, error) {
	ids := make([]uint64, 0, len(tickets))
	for i := range tickets {
		ids = append(ids, tickets[i].MemberID)
	}
	items, err := h.store.MembersByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	byID := make(map[uint64]*model.Member, len(items))
	for i := range items {
		byID[items[i].ID] = &items[i]
	}
	return byID, nil
}

// memberOfTicket 取单个工单的所属会员（查不到时返回零值视图而不报错）。
func (h *ticketHandler) memberOfTicket(ctx context.Context, ticket *model.Ticket) (*model.Member, error) {
	byID, err := h.membersOfTickets(ctx, []model.Ticket{*ticket})
	if err != nil {
		return nil, err
	}
	return byID[ticket.MemberID], nil
}

// ticketAuthorKey 是消息作者名解析的键（author_type + author_id）。
type ticketAuthorKey struct {
	AuthorType string
	AuthorID   uint64
}

// ticketAuthorNames 解析消息作者账号名（会员查 members、管理员查 admins）；
// 账号已不存在时由视图层回退占位符。
func ticketAuthorNames(ctx context.Context, st *store.Store, messages []model.TicketMessage) (map[ticketAuthorKey]string, error) {
	memberIDs := make([]uint64, 0, len(messages))
	adminIDs := make([]uint64, 0, len(messages))
	for i := range messages {
		switch messages[i].AuthorType {
		case model.TicketAuthorMember:
			memberIDs = append(memberIDs, messages[i].AuthorID)
		case model.TicketAuthorAdmin:
			adminIDs = append(adminIDs, messages[i].AuthorID)
		}
	}

	names := make(map[ticketAuthorKey]string, len(messages))
	members, err := st.MembersByIDs(ctx, memberIDs)
	if err != nil {
		return nil, err
	}
	for i := range members {
		names[ticketAuthorKey{AuthorType: model.TicketAuthorMember, AuthorID: members[i].ID}] = members[i].Username
	}
	admins, err := st.AdminsByIDs(ctx, adminIDs)
	if err != nil {
		return nil, err
	}
	for i := range admins {
		names[ticketAuthorKey{AuthorType: model.TicketAuthorAdmin, AuthorID: admins[i].ID}] = admins[i].Username
	}
	return names, nil
}

// ---------------------------------------------------------------------------
// 视图组装与参数解析
// ---------------------------------------------------------------------------

// newTicketView 组装会员端工单视图（instance 为 nil 时输出 null）。
func newTicketView(ticket *model.Ticket, instance *model.Instance) ticketView {
	return ticketView{
		ID:          ticket.ID,
		TradeNo:     ticket.TradeNo,
		Subject:     ticket.Subject,
		Category:    ticket.Category,
		Status:      ticket.Status,
		InstanceID:  ticket.InstanceID,
		Instance:    newTicketInstanceView(instance),
		LastReplyAt: formatTime(ticket.LastReplyAt),
		ClosedAt:    formatTimePtr(ticket.ClosedAt),
		CreatedAt:   formatTime(ticket.CreatedAt),
		UpdatedAt:   formatTime(ticket.UpdatedAt),
	}
}

// newAdminTicketView 组装管理端工单视图（多会员概要；会员查不到时输出零值）。
func newAdminTicketView(ticket *model.Ticket, instance *model.Instance, member *model.Member) adminTicketView {
	view := adminTicketView{ticketView: newTicketView(ticket, instance), MemberID: ticket.MemberID}
	if member != nil {
		view.Member = ticketMemberView{ID: member.ID, Username: member.Username, Nickname: member.Nickname}
	}
	return view
}

// newTicketInstanceView 组装实例概要（**不含**主机账号密码等敏感字段）。
func newTicketInstanceView(instance *model.Instance) *ticketInstanceView {
	if instance == nil {
		return nil
	}
	return &ticketInstanceView{
		ID:          instance.ID,
		Name:        instance.Name,
		ProductName: instance.ProductName,
		Status:      instance.Status,
	}
}

// newTicketMessageViews 组装消息视图；includeInternal 为 false 时**过滤掉内部备注**
// （会员端绝不返回，契约 16.3）。
func newTicketMessageViews(messages []model.TicketMessage, names map[ticketAuthorKey]string, includeInternal bool) []ticketMessageView {
	views := make([]ticketMessageView, 0, len(messages))
	for i := range messages {
		if messages[i].Internal && !includeInternal {
			continue
		}
		views = append(views, newTicketMessageView(&messages[i], names))
	}
	return views
}

// newTicketMessageView 组装单条消息视图（创建/回复接口直接复用刚写入的消息）。
func newTicketMessageView(message *model.TicketMessage, names map[ticketAuthorKey]string) ticketMessageView {
	name := names[ticketAuthorKey{AuthorType: message.AuthorType, AuthorID: message.AuthorID}]
	if strings.TrimSpace(name) == "" {
		name = msgTicketAuthorUnknown
	}
	return ticketMessageView{
		ID:         message.ID,
		AuthorType: message.AuthorType,
		AuthorID:   message.AuthorID,
		AuthorName: name,
		Content:    message.Content,
		Internal:   message.Internal,
		CreatedAt:  formatTime(message.CreatedAt),
	}
}

// ticketPaging 解析分页参数（缺省 1 / 20，上限 100）；非法时写出 40001 并返回 ok=false。
func ticketPaging(c *gin.Context) (int, int, bool) {
	return pagingParams(c)
}

// ticketStatusQuery 解析并校验 status 查询参数；非法时写出 40001 并返回 ok=false。
func ticketStatusQuery(c *gin.Context) (string, bool) {
	status := strings.TrimSpace(c.Query("status"))
	if status != "" && !model.IsValidTicketStatus(status) {
		response.Fail(c, response.CodeInvalidParam, "status 只能是 open / replied / closed")
		return "", false
	}
	return status, true
}

// ticketIDParam 解析路径参数 :id。
func ticketIDParam(c *gin.Context) (uint64, bool) {
	id, err := strconv.ParseUint(strings.TrimSpace(c.Param("id")), 10, 64)
	if err != nil || id == 0 {
		response.Fail(c, response.CodeInvalidParam, "工单 ID 必须为正整数")
		return 0, false
	}
	return id, true
}

// instanceIDRef 输出可空实例 ID 的日志值（nil 输出 "-"）。
func instanceIDRef(id *uint64) any {
	if id == nil {
		return "-"
	}
	return *id
}

// ---------------------------------------------------------------------------
// 通知触发点（阶段 6b）：nil 保护后转交 NotificationTrigger
// ---------------------------------------------------------------------------

// ticketCreated 触发「新工单」通知（未接线时静默跳过）。
func (h *ticketHandler) ticketCreated(ticketID uint64) {
	if h.notify == nil {
		return
	}
	h.notify.TicketCreated(ticketID)
}

// ticketRepliedByMember 触发「会员回复工单」通知（发给客服）。
func (h *ticketHandler) ticketRepliedByMember(ticketID uint64) {
	if h.notify == nil {
		return
	}
	h.notify.TicketRepliedByMember(ticketID)
}

// ticketRepliedByAdmin 触发「客服回复工单」通知（发给会员）。
func (h *ticketHandler) ticketRepliedByAdmin(ticketID uint64) {
	if h.notify == nil {
		return
	}
	h.notify.TicketRepliedByAdmin(ticketID)
}

// ticketClosedByAdmin 触发「工单被客服关闭」通知（发给会员）。
func (h *ticketHandler) ticketClosedByAdmin(ticketID uint64) {
	if h.notify == nil {
		return
	}
	h.notify.TicketClosedByAdmin(ticketID)
}
