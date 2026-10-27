export default {
  "refresh": "刷新",
  "authorization": {
    "invalid": "BPS 凭据已失效，需要重新授权",
    "primary": "当前使用账号的主 Excel 授权，请在账号 OAuth 设置中管理",
    "title": "Excel 独立授权",
    "hint": "请授权同一 ChatGPT 用户和工作区。Excel 凭证独立加密保存，不会覆盖 Codex 凭证。",
    "ready": "已保存 Excel 授权",
    "required": "需要 Excel 授权",
    "refreshRequired": "下次 BPS 请求会刷新凭证",
    "authorize": "生成授权链接",
    "openLogin": "打开官方 Excel 登录页",
    "callback": "登录后粘贴完整回调网址",
    "save": "保存 Excel 授权",
    "revoke": "移除 Excel 授权",
    "importHint": "粘贴包含 client_id、access_token、refresh_token 和 id_token 的 Excel OAuth 凭证 JSON。不能通过修改 client_id 将 Codex 凭证转换为 Excel 凭证。",
    "failed": "Excel 授权失败",
    "invalidCallback": "请粘贴 bps.openai.com 的完整回调网址",
    "import": "导入 Excel 凭证"
  },
  "globalEnabled": "启用 BPS 网关",
  "globalHint": "全站运行总开关。开启后，仅账号中启用并选定的模型走 BPS；账号策略在账号设置中保存。图片处理预算仍由功能设置统一管理。"
}
