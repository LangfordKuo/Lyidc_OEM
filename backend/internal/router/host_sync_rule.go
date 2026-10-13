package router

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/LangfordKuo/Lyidc_OEM/backend/internal/model"
)

// 上游状态回推（R16）的纯逻辑：签名算法与状态映射（契约 22.3）。
//
// 本文件的算法与上游（魔方财务）PHP 侧**逐字符一致**：任何改动都必须同步契约 22.3 的
// golden 向量（`host_sync_rule_test.go` 逐字符断言三个算例）。

// hostSyncSignature 计算上游状态回推的签名（契约 22.3）：
//
//	sign = upper( md5( json_encode( ksort({ "id": <收到的原始 id>, "rand_str": …, "token": … }, SORT_STRING) ) ) )
//
// 三个必须守住的细节：
//  1. `id` 以**字符串**参与签名——上游 PHP 侧 `$_POST['id']` 是字符串，`json_encode` 后带引号
//     （`{"id":"42",…}`，不是 `{"id":42,…}`）；
//  2. 键按字典序（`id` < `rand_str` < `token`）：Go 的 `map[string]string` 序列化天然按 key 排序，
//     等价于 PHP 的 `ksort(..., SORT_STRING)`；
//  3. **必须关掉 HTML 转义**：`json.Marshal` 会把 `<` / `>` / `&` 写成 Unicode 转义形式而与 PHP 不一致，
//     因此用 `json.Encoder` + `SetEscapeHTML(false)`，并去掉 `Encode` 追加的换行。
func hostSyncSignature(id, randStr, token string) string {
	sum := md5.Sum([]byte(hostSyncSignPayload(id, randStr, token)))
	return strings.ToUpper(hex.EncodeToString(sum[:]))
}

// hostSyncSignPayload 构造**参与签名的 JSON 文本**（契约 22.3 的 `json_encode(ksort(...))`）。
// 单独拆出来是为了让单测能逐字符断言签名字符串，而不只是断言哈希。
func hostSyncSignPayload(id, randStr, token string) string {
	payload := map[string]string{
		"id":       id,
		"rand_str": randStr,
		"token":    token,
	}
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(payload); err != nil {
		// map[string]string 不可能序列化失败；返回不可能匹配的签名（绝不放过请求）。
		return ""
	}
	return string(bytes.TrimSuffix(buf.Bytes(), []byte("\n")))
}

// hostSyncStatus 把上游 `domainstatus` 映射为本地 `instances.status`（契约 22.3 状态映射表）。
//
// 比较不区分大小写并忽略首尾空白（上游首字母大写、本地枚举小写）。
// 返回 ok=false 表示**不该改本地状态**（只更新 `upstream_status` 原文）：
// `Cancelled` / `Pending` / `Fraud` 以及一切未知值都走这条分支。
//
// `Cancelled` **刻意不映射**到本地 `cancelled`（依据契约 15.8.1 的 5c 定稿）：`instances.status`
// 只反映主机的**真实可用状态**——上游仍在运行时即使已申请取消也保持 `active`/`suspended`，
// 而 `cancelled` 是「保留但不再写入」的预留枚举、「取消申请在途」由 `cancel_status=pending` 表达。
// 若照抄上游枚举把它映射成 `cancelled`，按契约 15.1 的操作矩阵（只有 `active` 可操作）
// 会员会在主机还活着的时候失去操作能力；真正的终止收敛只认 `Deleted`（→ `terminated`）。
// **不要按上游枚举照抄改回去**。
func hostSyncStatus(domainStatus string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(domainStatus)) {
	case "active":
		return model.InstanceStatusActive, true
	case "suspended":
		return model.InstanceStatusSuspended, true
	case "deleted":
		return model.InstanceStatusTerminated, true
	default:
		return "", false
	}
}
