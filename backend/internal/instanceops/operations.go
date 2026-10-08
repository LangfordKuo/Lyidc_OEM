package instanceops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/delivery"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/upstream"
)

// PowerOp 是电源操作（契约 15.2：soft_on / soft_off / reboot / hard_off / hard_reboot）。
type PowerOp string

// 支持的电源操作；硬操作（hard_off / hard_reboot）为强制断电/复位，
// 可能造成主机数据损坏，契约中标注为高风险操作，由调用方（前端二次确认）与用户自负。
const (
	// PowerSoftOn 软开机。
	PowerSoftOn PowerOp = "soft_on"
	// PowerSoftOff 软关机（上游 off）。
	PowerSoftOff PowerOp = "soft_off"
	// PowerReboot 软重启（上游 reboot）。
	PowerReboot PowerOp = "reboot"
	// PowerHardOff 强制关机（直接断电，风险操作）。
	PowerHardOff PowerOp = "hard_off"
	// PowerHardReboot 强制重启（硬复位，风险操作）。
	PowerHardReboot PowerOp = "hard_reboot"
)

// IsValidPowerOp 判断电源操作取值是否合法。
func IsValidPowerOp(op string) bool {
	switch PowerOp(op) {
	case PowerSoftOn, PowerSoftOff, PowerReboot, PowerHardOff, PowerHardReboot:
		return true
	default:
		return false
	}
}

// AuditAction 把电源操作映射为审计动作（instance_operation_logs.action）；
// 同时作为对外返回的 action 字段值。
func (op PowerOp) AuditAction() string {
	switch op {
	case PowerSoftOn:
		return model.ActionPowerOn
	case PowerSoftOff:
		return model.ActionPowerOff
	case PowerReboot:
		return model.ActionReboot
	case PowerHardOff:
		return model.ActionHardOff
	case PowerHardReboot:
		return model.ActionHardReboot
	default:
		return string(op)
	}
}

// label 返回中文说明（用于审计 message 与对外提示）。
func (op PowerOp) label() string {
	switch op {
	case PowerSoftOn:
		return "开机"
	case PowerSoftOff:
		return "关机"
	case PowerReboot:
		return "重启"
	case PowerHardOff:
		return "强制关机"
	case PowerHardReboot:
		return "强制重启"
	default:
		return string(op)
	}
}

// Power 执行一次电源操作（仅 active 实例；成功后写审计并返回提示文案）。
//
// 上游调用是**异步语义**：上游受理（status=200）即返回成功，实际电源状态由上游执行，
// 可用管理端同步接口查看状态（契约 15.2）。
func (s *Service) Power(ctx context.Context, instance *model.Instance, actor Actor, op PowerOp) (string, error) {
	if err := s.requireOperable(ctx, instance, actor, op.AuditAction(), op.label()); err != nil {
		return "", err
	}

	err := s.upstreamCall(ctx, instance, actor, op.AuditAction(), func(client *upstream.Client) error {
		var callErr error
		switch op {
		case PowerSoftOn:
			_, callErr = client.On(ctx, instance.HostID)
		case PowerSoftOff:
			_, callErr = client.Off(ctx, instance.HostID)
		case PowerReboot:
			_, callErr = client.Reboot(ctx, instance.HostID)
		case PowerHardOff:
			_, callErr = client.HardOff(ctx, instance.HostID)
		case PowerHardReboot:
			_, callErr = client.HardReboot(ctx, instance.HostID)
		default:
			callErr = fmt.Errorf("%w：不支持的电源操作 %q", ErrNotOperable, string(op))
		}
		return callErr
	})
	if err != nil {
		return "", err
	}

	message := op.label() + "指令已提交（上游异步执行）"
	s.audit(ctx, instance.ID, actor, op.AuditAction(), model.InstanceOpSuccess, message)
	return message, nil
}

// Reinstall 发起重装系统（仅 active 实例；上游异步执行，受理即返回成功）。
//
// osID 为**重装可选系统列表**（ReinstallOptions）返回的系统 ID；port 为可选端口（0 表示不传）。
func (s *Service) Reinstall(ctx context.Context, instance *model.Instance, actor Actor, osID, port int) (string, error) {
	if osID <= 0 {
		return "", fmt.Errorf("%w：os_id 必须为正整数（取自重装系统列表）", ErrNotOperable)
	}
	if err := s.requireOperable(ctx, instance, actor, model.ActionReinstall, "重装系统"); err != nil {
		return "", err
	}

	err := s.upstreamCall(ctx, instance, actor, model.ActionReinstall, func(client *upstream.Client) error {
		_, callErr := client.Reinstall(ctx, instance.HostID, osID, port)
		return callErr
	})
	if err != nil {
		return "", err
	}

	message := fmt.Sprintf("重装系统已发起（系统 ID %d，上游异步执行，过程中主机不可用）", osID)
	s.audit(ctx, instance.ID, actor, model.ActionReinstall, model.InstanceOpSuccess, message)
	return message, nil
}

// ReinstallOptions 返回该实例可重装的系统列表与分组（上游 /host/cloudos 按商品返回）。
//
// 上游要求 `os_config_option_id`（商品「操作系统」可配置项的 ID）才返回列表，
// 该项 ID 从商品 config_json 的 config_groups[].options[].option_name（`os|操作系统`）解析。
// 返回的 CloudOS.ID 即重装接口 os 参数（阶段 2 真机已验证该口径）。
func (s *Service) ReinstallOptions(ctx context.Context, instance *model.Instance) (*upstream.CloudOSList, error) {
	product, err := s.store.ProductByID(ctx, instance.ProductID)
	if err != nil {
		return nil, fmt.Errorf("商品 %d 查询失败：%w", instance.ProductID, err)
	}
	osOptionID, err := osConfigOptionID(product.ConfigJSON)
	if err != nil {
		return nil, err
	}

	client, err := s.client(ctx)
	if err != nil {
		return nil, err
	}
	list, err := client.CloudOS(ctx, product.UpstreamPID, osOptionID)
	if err != nil {
		return nil, fmt.Errorf("%w：%v", ErrUpstreamFailed, err)
	}
	return list, nil
}

// osConfigOptionID 从商品配置缓存解析「操作系统」可配置项的 ID。
// 上游 option_name 形如 `os|操作系统`（键名|中文名）；找不到时报 ErrNoOSOption。
func osConfigOptionID(configJSON string) (int, error) {
	if strings.TrimSpace(configJSON) == "" {
		return 0, ErrNoOSOption
	}
	var cache struct {
		ConfigGroups []struct {
			Options []struct {
				ID         int    `json:"id"`
				OptionName string `json:"option_name"`
			} `json:"options"`
		} `json:"config_groups"`
	}
	if err := json.Unmarshal([]byte(configJSON), &cache); err != nil {
		return 0, fmt.Errorf("%w：商品配置缓存解析失败", ErrNoOSOption)
	}
	for _, group := range cache.ConfigGroups {
		for _, option := range group.Options {
			key, _, _ := strings.Cut(option.OptionName, "|")
			if strings.EqualFold(strings.TrimSpace(key), "os") && option.ID > 0 {
				return option.ID, nil
			}
		}
	}
	return 0, ErrNoOSOption
}

// 密码口径（契约 15.2 定稿）：不传密码时自动生成 16 位强密码（与开通密码同规则）；
// 传入密码时要求 8-64 个字符且同时包含字母与数字（拒绝纯数字/纯字母/过短密码）。
const (
	PasswordMinLength = 8
	PasswordMaxLength = 64
)

// ResetPassword 重置主机密码并落库（仅 active 实例）。
//
// password 为空串时自动生成 16 位强密码；非空时按口径校验（不满足返回 ErrWeakPassword）。
// 返回最终生效的密码（上游若回传密码则以回传为准）；密码**不写日志、不写审计**。
func (s *Service) ResetPassword(ctx context.Context, instance *model.Instance, actor Actor, password string) (string, error) {
	newPassword, err := PreparePassword(password)
	if err != nil {
		s.auditFail(ctx, instance, actor, model.ActionResetPassword, err)
		return "", err
	}
	if err := s.requireOperable(ctx, instance, actor, model.ActionResetPassword, "重置密码"); err != nil {
		return "", err
	}

	var upstreamPassword string
	err = s.upstreamCall(ctx, instance, actor, model.ActionResetPassword, func(client *upstream.Client) error {
		result, callErr := client.ResetPassword(ctx, instance.HostID, newPassword)
		if callErr != nil {
			return callErr
		}
		if value := result.DataField("password"); value != "" {
			upstreamPassword = value
		}
		return nil
	}, newPassword)
	if err != nil {
		return "", err
	}

	final := newPassword
	if upstreamPassword != "" {
		final = upstreamPassword
	}
	if err := s.store.UpdateInstancePassword(ctx, instance.ID, final); err != nil {
		s.auditFail(ctx, instance, actor, model.ActionResetPassword, err, final)
		return "", fmt.Errorf("密码已重置但落库失败：%w", err)
	}

	s.audit(ctx, instance.ID, actor, model.ActionResetPassword, model.InstanceOpSuccess,
		"重置密码成功（新密码已落库，仅会员详情可见）", final)
	return final, nil
}

// ErrWeakPassword 指定密码不满足强度口径。
var ErrWeakPassword = errors.New("密码不符合强度要求")

// PreparePassword 返回最终要提交上游的密码：空串时自动生成，否则按口径校验。
func PreparePassword(password string) (string, error) {
	if strings.TrimSpace(password) == "" {
		return delivery.GeneratePassword()
	}
	if err := ValidatePassword(password); err != nil {
		return "", err
	}
	return password, nil
}

// ValidatePassword 校验指定密码：长度 8-64（按字符），且同时包含字母与数字。
func ValidatePassword(password string) error {
	length := len([]rune(password))
	if length < PasswordMinLength || length > PasswordMaxLength {
		return fmt.Errorf("%w：长度需为 %d-%d 个字符", ErrWeakPassword, PasswordMinLength, PasswordMaxLength)
	}
	var hasLetter, hasDigit bool
	for _, r := range password {
		switch {
		case unicode.IsLetter(r):
			hasLetter = true
		case unicode.IsDigit(r):
			hasDigit = true
		}
	}
	if !hasLetter || !hasDigit {
		return fmt.Errorf("%w：需同时包含字母与数字", ErrWeakPassword)
	}
	return nil
}
