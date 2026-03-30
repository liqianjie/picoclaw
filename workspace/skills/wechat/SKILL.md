---
name: wechat
description: "微信（WeChat）操作技能。当用户需要发消息、分享内容、扫码、支付、打开微信功能时使用。优先通过 Intent/Scheme 直接调用微信能力，避免依赖无障碍服务的 UI 操作。适用于：发送文字/图片/视频给好友、收藏内容、分享到朋友圈/视频号、扫一扫、微信支付、打开小程序等场景。"
---

# 微信（WeChat）操作技能

包名：`com.tencent.mm`

操作微信时，**优先使用 Intent/Scheme 直接调用**，比 UI 操作更快更稳定。仅当 Intent 无法覆盖的场景才回退到无障碍 UI 操作。

## 核心原则

1. **发文字/图片/文件给好友** → 用 `SEND` Intent（自动弹出联系人选择）
2. **打开微信特定页面** → 用 `weixin://` scheme
3. **输入文字到微信输入框** → 用 `paste_input`（微信不支持标准 `input`）
4. **其他 UI 交互** → 才使用 click/scroll 等无障碍操作

## 一、分享内容给好友（⭐ 最常用）

通过 `android.intent.action.SEND` 直接分享，微信会弹出联系人选择界面，文本已自动填好。

### 分享文字

```json
{
  "action": "intent",
  "action_type": "android.intent.action.SEND",
  "package": "com.tencent.mm",
  "class_name": "com.tencent.mm.ui.tools.ShareImgUI",
  "mime_type": "text/plain",
  "extras": {"android.intent.extra.TEXT": "要发送的文字内容"}
}
```

### 分享图片

```json
{
  "action": "intent",
  "action_type": "android.intent.action.SEND",
  "package": "com.tencent.mm",
  "class_name": "com.tencent.mm.ui.tools.ShareImgUI",
  "mime_type": "image/*",
  "extras": {"android.intent.extra.STREAM": "content://或file://图片URI"}
}
```

### 分享视频/音频/文件

同上，修改 `mime_type` 为对应类型（`video/*`、`audio/*`、`application/*`）。

## 二、收藏到微信

```json
{
  "action": "intent",
  "action_type": "android.intent.action.SEND",
  "package": "com.tencent.mm",
  "class_name": "com.tencent.mm.ui.tools.AddFavoriteUI",
  "mime_type": "text/plain",
  "extras": {"android.intent.extra.TEXT": "要收藏的内容"}
}
```

支持类型：image、video、text、application、audio。

## 三、分享到朋友圈

```json
{
  "action": "intent",
  "action_type": "android.intent.action.SEND",
  "package": "com.tencent.mm",
  "class_name": "com.tencent.mm.ui.tools.ShareToTimeLineUI",
  "mime_type": "image/*",
  "extras": {"android.intent.extra.STREAM": "content://图片URI"}
}
```

> 朋友圈仅支持图片分享。

## 四、分享到视频号

```json
{
  "action": "intent",
  "action_type": "android.intent.action.SEND",
  "package": "com.tencent.mm",
  "class_name": "com.tencent.mm.ui.tools.ShareToStatusUI",
  "mime_type": "video/*",
  "extras": {"android.intent.extra.STREAM": "content://视频URI"}
}
```

支持类型：image、video。

## 五、URI Scheme（深度链接）

### 打开微信（通用入口）

```json
{
  "action": "intent",
  "action_type": "android.intent.action.VIEW",
  "data": "weixin://"
}
```

### 扫一扫

```json
{
  "action": "intent",
  "action_type": "android.intent.action.VIEW",
  "data": "weixin://scanqrcode"
}
```

### 打开指定二维码内容

```json
{
  "action": "intent",
  "action_type": "android.intent.action.VIEW",
  "data": "weixin://qr/二维码内容",
  "package": "com.tencent.mm",
  "class_name": "com.tencent.mm.plugin.setting.ui.qrcode.GetQRCodeInfoUI"
}
```

### 微信故障恢复

```json
{
  "action": "intent",
  "action_type": "android.intent.action.VIEW",
  "data": "wechat://recovery"
}
```

## 六、微信自定义 Action

### 公众号/小程序快捷方式

```json
{
  "action": "intent",
  "action_type": "com.tencent.mm.action.BIZSHORTCUT",
  "package": "com.tencent.mm",
  "class_name": "com.tencent.mm.ui.LauncherUI"
}
```

### 微信快捷方式入口

```json
{
  "action": "intent",
  "action_type": "com.tencent.mm.action.WX_SHORTCUT",
  "package": "com.tencent.mm",
  "class_name": "com.tencent.mm.plugin.base.stub.WXShortcutEntryActivity"
}
```

### 微信支付

```json
{
  "action": "intent",
  "action_type": "com.tencent.mm.gwallet.ACTION_PAY_REQUEST",
  "package": "com.tencent.mm",
  "class_name": "com.tencent.mm.plugin.gwallet.GWalletUI"
}
```

## 七、微信输入最佳实践

微信使用自定义控件，**标准 `input` action 无法工作**。

⚠️ **action 名称必须严格为 `paste_input`**，这是唯一支持的输入方式：

```json
{
  "action": "paste_input",
  "text": "要输入的文字",
  "x": 540,
  "y": 2100
}
```

> - `x`/`y` 为微信输入框的坐标位置，需先通过 `get_screen` 或截图确认。
> - ⚠️ action 字段的值必须严格为 `"paste_input"`，不要使用任何其他名称。

## 八、常见操作流程

### 给某人发消息（推荐方式）

直接使用 SEND Intent，跳过打开微信 → 找联系人 → 点输入框的复杂流程：

```json
{"action": "intent", "action_type": "android.intent.action.SEND", "package": "com.tencent.mm", "class_name": "com.tencent.mm.ui.tools.ShareImgUI", "mime_type": "text/plain", "extras": {"android.intent.extra.TEXT": "消息内容"}}
```

然后在弹出的联系人选择页面点击目标联系人即可。

### 如果已经在聊天页面需要输入

⚠️ action 必须严格为 `paste_input`：

```json
{"action": "paste_input", "text": "消息内容", "x": 540, "y": 2100}
```

然后点击发送按钮：

```json
{"action": "click", "target": "发送"}
```

## 九、可用组件速查表

| 组件 | 类名 | 用途 |
|------|------|------|
| 分享给好友 | `.ui.tools.ShareImgUI` | SEND 文字/图片/视频/音频/文件 |
| 收藏 | `.ui.tools.AddFavoriteUI` | SEND 收藏内容 |
| 朋友圈 | `.ui.tools.ShareToTimeLineUI` | SEND 图片到朋友圈 |
| 视频号 | `.ui.tools.ShareToStatusUI` | SEND 图片/视频到视频号 |
| 深度链接 | `.plugin.base.stub.WXCustomSchemeEntryActivity` | weixin:// scheme 处理 |
| 二维码 | `.plugin.setting.ui.qrcode.GetQRCodeInfoUI` | 扫码/二维码信息 |
| 主界面 | `.ui.LauncherUI` | 启动微信/公众号快捷方式 |
| 快捷方式 | `.plugin.base.stub.WXShortcutEntryActivity` | WX_SHORTCUT 入口 |
| 支付 | `.plugin.gwallet.GWalletUI` | 支付请求/查询 |
| 恢复 | `.recovery.ui.RecoveryUI` | wechat://recovery |
| NFC | `.plugin.nfc_open.ui.NfcDeepLinkUI` | NFC 深度链接 |
| 小程序素材 | `.pluginsdk.ui.tools.AppBrandOpenMaterialUI` | QB_OPEN_MATERIAL |
