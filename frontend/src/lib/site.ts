// 站点常量。
//
// TODO(待接后端)：站点名真实来源是数据库 `settings.site.name`（安装向导第 4 步写入，
// 见 docs/api-contract.md 12.1 与 13.3），但契约中**没有面向会员端的公开站点信息接口**
// （只有安装模式下的 `GET /install/api/status` 会回带 site_name）。因此前端一期先用常量，
// 待后端提供公开站点信息接口（例如 `GET /api/v1/site`）后再替换为接口读取。
export const SITE_NAME = '岭云互联'

// 首页/页脚的一句话定位文案。
export const SITE_TAGLINE = '稳定可靠的云服务器与 IDC 服务商'

export const SITE_DESCRIPTION =
  '岭云互联提供香港、内地多区域云服务器与独立服务器，支持月付到三年付六种计费周期，下单后自动开通，在线支付即时到账。'
