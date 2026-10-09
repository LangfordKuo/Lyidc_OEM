-- 0012 商品简介数据口径固化（R5：上游 description 拉取 + 商品卡配置列表展示）。
-- 时间语义：本迁移不动任何 DATETIME 列。
--
-- 背景与取舍：
--   1) products.description 列**自 0004 建立时即存在**（TEXT NULL），因此本迁移不做
--      ADD COLUMN（重复加列会失败）；也不改动 0004，只把 R5 起的数据口径固化进列注释。
--   2) 口径：导入链路落库前对上游原文做一次 HTML 实体反转义（标准库 html.UnescapeString），
--      即存「解码后的原始 HTML」：`&lt;li&gt;CPU:2核心&lt;/li&gt;` → `<li>CPU:2核心</li>`；
--      接口层再解析为 description_lines 行数组下发（契约 10.3），前端无需再反转义。
--   3) 存量数据（R5 前的转义原文）由下一次商品导入自然覆盖刷新；解析工具本身
--      对「转义/未转义」两种输入都兼容，不依赖数据先刷新。
--
-- 幂等性：MODIFY COLUMN 只改注释，不改类型与可空性，重复执行安全。
ALTER TABLE `products`
    MODIFY COLUMN `description` TEXT NULL
        COMMENT '商品描述（上游原文经 HTML 实体反转义后的原始 HTML；接口层解析为 description_lines 行数组）';
