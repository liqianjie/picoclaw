---
name: huoying-checkin
description: "火影忍者手游情报社微信小程序每日签到。自动打开微信，通过 发现→小程序→火影情报社 进入小程序，在福利站页面完成每日签到。适用于：每日签到、领取签到奖励等场景。"
metadata: {"nanobot":{"emoji":"🍥"}}
---

# 火影忍者手游情报社 — 每日签到

微信小程序 AppID：`wxc47b57c32a7fe64b`
微信包名：`com.tencent.mm`
手机分辨率参考：1080×2412（实际以 screenshot_vision 返回的 screen_width/screen_height 为准）

## 核心原则

1. **全程使用 `accessibility_action` 工具**，通过无障碍服务操控手机
2. **优先用文字点击（`click` + `target`）**，避免硬编码坐标
3. **每一步操作后用 `screenshot_vision` 验证结果**，根据实际画面决定下一步
4. **不使用任何文字输入操作**，全程通过 UI 点击导航
5. **遇到弹窗/广告/活动提示时，优先关闭再继续主流程**

## 触发指令

当用户说以下话语时，使用此 skill：
- "火影签到"
- "火影打卡"
- "帮我火影签到"
- "火影情报社签到"
- "每日签到"（上下文涉及火影时）
- "去火影小程序签到"

## 签到完整流程

### 第一阶段：打开微信

```json
{"action": "launch_app", "package": "com.tencent.mm"}
```

等待微信启动：
```json
{"action": "wait", "duration": 2000}
```

截图确认微信已打开：
```json
{"action": "screenshot_vision", "prompt": "当前是否在微信主界面？底部是否有 微信/通讯录/发现/我 四个TAB？"}
```

**判断逻辑**：
- 如果不在微信主界面（可能在聊天页面等），点击底部 "微信" TAB 回到主界面，或按返回键：
  ```json
  {"action": "global_action", "global_action_id": "back"}
  ```

### 第二阶段：进入小程序列表

点击底部"发现"TAB：
```json
{"action": "click", "target": "发现"}
```

等待页面切换：
```json
{"action": "wait", "duration": 1000}
```

截图确认进入发现页：
```json
{"action": "screenshot_vision", "prompt": "当前是否在微信的发现页面？能否看到 朋友圈、视频号、小程序 等入口？"}
```

点击"小程序"入口：
```json
{"action": "click", "target": "小程序"}
```

等待小程序列表加载：
```json
{"action": "wait", "duration": 2000}
```

截图确认进入小程序列表：
```json
{"action": "screenshot_vision", "prompt": "当前是否在小程序列表页面？能否看到最近使用的小程序列表？有没有看到'火影'相关的小程序？"}
```

### 第三阶段：打开火影情报社小程序

在小程序列表中找到并点击火影情报社。小程序名称可能显示为"火影忍者手游情报社"或简称"火影情报社"：

```json
{"action": "click", "target": "火影"}
```

> **备选方案**：如果 `click` 按"火影"找不到，尝试：
> - `{"action": "click", "target": "火影忍者"}`
> - `{"action": "click", "target": "火影情报社"}`
> - `{"action": "click", "target": "火影忍者手游情报社"}`
> - 如果仍找不到，使用 `screenshot_vision` 截图分析页面，可能需要向下滚动查找：
>   ```json
>   {"action": "scroll", "direction": "down"}
>   ```

等待小程序加载（小程序启动通常较慢）：
```json
{"action": "wait", "duration": 5000}
```

截图确认小程序已打开：
```json
{"action": "screenshot_vision", "prompt": "当前页面是什么内容？是否已进入火影忍者手游情报社小程序？底部有哪些TAB页？能否看到 首页/资讯/赛事/福利站 等TAB？"}
```

**异常处理**：
- 如果看到"该小程序需要授权"等弹窗，点击"允许"或"确认"
- 如果看到更新提示，关闭或确认更新
- 如果加载失败，重试一次

### 第三阶段补充：关闭小程序启动弹窗/广告

小程序刚打开时，经常会弹出各种广告弹窗、活动推广、公告通知等。**必须先关闭所有弹窗，再进行后续操作**。

截图检查是否有弹窗：
```json
{"action": "screenshot_vision", "prompt": "当前页面是否有弹窗、广告、活动推广、公告通知等覆盖层？如果有，请描述弹窗内容，以及弹窗上有哪些按钮（如关闭×、我知道了、确定、跳过、暂不参与等）？弹窗的关闭按钮在什么位置？"}
```

**弹窗关闭策略**（按优先级尝试）：

1. 如果看到弹窗上有"×"关闭按钮，点击关闭：
   ```json
   {"action": "click", "target": "×"}
   ```
   > 备选：`{"action": "click", "target": "关闭"}`

2. 如果看到"我知道了"、"知道了"、"确定"、"确认"等按钮：
   ```json
   {"action": "click", "target": "我知道了"}
   ```
   > 备选：`{"action": "click", "target": "知道了"}` / `{"action": "click", "target": "确定"}`

3. 如果看到"跳过"、"暂不参与"、"以后再说"等按钮：
   ```json
   {"action": "click", "target": "跳过"}
   ```
   > 备选：`{"action": "click", "target": "暂不参与"}` / `{"action": "click", "target": "以后再说"}`

4. 如果以上都找不到，尝试按返回键关闭弹窗：
   ```json
   {"action": "global_action", "global_action_id": "back"}
   ```

等待弹窗关闭：
```json
{"action": "wait", "duration": 1000}
```

**重要：关闭一个弹窗后，必须再次截图检查是否还有其他弹窗**。可能会有多层弹窗叠加，需要逐个关闭，直到看到正常的小程序页面为止：
```json
{"action": "screenshot_vision", "prompt": "弹窗是否已关闭？当前页面是否还有其他弹窗或广告？能否看到小程序的正常内容和底部TAB（首页/资讯/赛事/福利站）？"}
```

如果仍有弹窗，重复上述关闭操作。最多重复 3 次。

### 第四阶段：进入福利站

点击底部"福利站"TAB：
```json
{"action": "click", "target": "福利站"}
```

等待福利站页面加载：
```json
{"action": "wait", "duration": 3000}
```

截图分析福利站页面状态：
```json
{"action": "screenshot_vision", "prompt": "当前是否在福利站页面？页面上有哪些内容？是否能看到'一键签到'按钮？或者是否有弹窗出现？是否显示'今日已签'？请详细描述页面内容。"}
```

**判断逻辑**：
- 如果看到 **"今日已签"** 或 **"已签到"** → 今天已经签过了，任务完成
- 如果看到 **"一键签到"** 按钮 → 继续第五阶段
- 如果自动弹出了签到弹窗（含"立即签到"按钮）→ 跳到第五阶段的弹窗处理
- 如果看到其他弹窗/广告 → 先关闭弹窗，再查找签到入口

### 第五阶段：执行签到

#### 5.1 点击"一键签到"按钮

```json
{"action": "click", "target": "一键签到"}
```

等待签到弹窗出现：
```json
{"action": "wait", "duration": 2000}
```

截图确认弹窗状态：
```json
{"action": "screenshot_vision", "prompt": "是否弹出了签到弹窗？弹窗上有什么按钮？能否看到'立即签到'按钮？"}
```

#### 5.2 点击"立即签到"按钮

```json
{"action": "click", "target": "立即签到"}
```

等待签到完成：
```json
{"action": "wait", "duration": 2000}
```

截图确认签到结果：
```json
{"action": "screenshot_vision", "prompt": "签到是否成功？页面上显示了什么？有没有签到成功的提示？有没有'我知道了'或'知道了'之类的确认按钮？"}
```

#### 5.3 关闭签到成功弹窗

如果看到"我知道了"或"知道了"按钮：
```json
{"action": "click", "target": "知道了"}
```

> **备选**：如果文字不完全匹配，尝试：
> - `{"action": "click", "target": "我知道了"}`
> - `{"action": "click", "target": "确定"}`
> - `{"action": "click", "target": "确认"}`

等待弹窗关闭：
```json
{"action": "wait", "duration": 1000}
```

#### 5.4 处理消息提醒弹窗

签到后可能弹出"开启消息提醒"等弹窗，需要关闭：

```json
{"action": "screenshot_vision", "prompt": "当前页面有什么弹窗？是否有消息提醒、订阅消息相关的弹窗？有没有'残忍拒绝'、'取消'、'暂不开启'等按钮？"}
```

如果看到消息提醒弹窗，点击拒绝：
```json
{"action": "click", "target": "残忍拒绝"}
```

> **备选**：
> - `{"action": "click", "target": "取消"}`
> - `{"action": "click", "target": "暂不开启"}`
> - `{"action": "click", "target": "拒绝"}`

### 第六阶段：确认完成并报告

最终截图确认签到状态：
```json
{"action": "screenshot_vision", "prompt": "签到流程是否完成？当前页面显示的签到状态是什么？能否看到今日已签或签到天数等信息？"}
```

向用户发送截图报告签到结果：
```json
{"action": "screenshot"}
```

**报告内容应包括**：
- ✅ 签到是否成功
- 📅 当前签到天数（如果页面可见）
- 🎁 获得的奖励（如果页面可见）
- 如果今天已签过，告知用户"今日已签到，无需重复签到"

## 异常处理

### 找不到目标元素

当 `click` 操作找不到目标文字时：
1. 使用 `screenshot_vision` 截图并分析当前页面
2. 根据 OCR 结果中的实际文字调整 `target`
3. 如果页面需要滚动才能看到目标，使用 `scroll`：
   ```json
   {"action": "scroll", "direction": "down"}
   ```
4. 最多重试 3 次，仍失败则向用户报告当前页面截图

### 小程序加载失败

1. 等待更长时间（增加到 8 秒）
2. 如果超时仍未加载，返回微信主界面重新进入
3. 最多重试 2 次

### 意外弹窗

遇到任何非预期的弹窗（广告、活动推送、更新提示等）：
1. 优先寻找"关闭"、"×"、"取消"、"跳过"等按钮点击
2. 如果找不到关闭按钮，尝试按返回键：
   ```json
   {"action": "global_action", "global_action_id": "back"}
   ```
3. 关闭弹窗后继续主流程

### 微信不在前台

如果 `launch_app` 后微信不在主界面：
1. 多次按返回键回到主界面：
   ```json
   {"action": "global_action", "global_action_id": "back"}
   ```
2. 或直接回到 Home 后重新打开：
   ```json
   {"action": "global_action", "global_action_id": "home"}
   ```
   ```json
   {"action": "launch_app", "package": "com.tencent.mm"}
   ```

## 注意事项

- 微信小程序加载速度取决于网络状况，`wait` 时间可能需要动态调整
- 小程序界面可能随版本更新而变化，需根据 `screenshot_vision` 返回的实际内容灵活应对
- 如果"发现"页面中"小程序"入口需要向下滚动才能看到，请先滚动再点击
- 全程**禁止使用 `input` 操作**，微信中文字输入问题尚未解决
- 如果小程序不在最近使用列表中，可能需要在小程序列表中搜索，但搜索涉及输入操作，此时建议用户先手动打开一次小程序使其出现在最近列表中
