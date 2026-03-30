package tools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/sipeed/picoclaw/pkg/logger"
	"github.com/sipeed/picoclaw/pkg/media"
	"github.com/sipeed/picoclaw/pkg/providers/protocoltypes"
)

// ActionExecuteCallback 是向 Android 客户端发送无障碍操作指令的回调函数。
// channel: 目标通道（如 "pico"）
// chatID: 目标会话 ID
// requestID: 请求唯一 ID，用于匹配响应
// action: 操作 JSON 参数
type ActionExecuteCallback func(channel, chatID, requestID string, action map[string]any) error

// VisionProvider 是视觉模型调用接口，与 providers.LLMProvider 兼容。
// 独立定义以避免循环依赖。
type VisionProvider interface {
	Chat(
		ctx context.Context,
		messages []protocoltypes.Message,
		tools []protocoltypes.ToolDefinition,
		model string,
		options map[string]any,
	) (*protocoltypes.LLMResponse, error)
}

// AccessibilityTool 允许 AI 通过无障碍服务控制 Android 设备。
// 支持的操作包括：点击、长按、滑动、输入文字、打开应用、全局操作、获取屏幕元素。
type AccessibilityTool struct {
	callback ActionExecuteCallback
	// pendingResults 存储等待 Android 端返回结果的 channel
	pendingResults sync.Map // requestID → chan ActionResult
	// mediaStore 用于截图保存和发送给用户
	mediaStore media.MediaStore
	// visionProvider 用于调用视觉模型（如 glm-4v-flash）分析截图
	visionProvider VisionProvider
	// visionModel 视觉模型名称（如 "glm-4v-flash"）
	visionModel string
}

// ActionResult 是 Android 端返回的无障碍操作执行结果。
type ActionResult struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

func NewAccessibilityTool() *AccessibilityTool {
	return &AccessibilityTool{}
}

func (t *AccessibilityTool) Name() string {
	return "accessibility_action"
}

func (t *AccessibilityTool) Description() string {
	return `Execute an accessibility action on the connected Android device. This tool allows you to interact with the device screen through the Accessibility Service.

Supported actions:
- "click": Click an element by its text. Params: {"action": "click", "target": "text on screen"}
- "click_position": Click at screen coordinates. Params: {"action": "click_position", "x": 540, "y": 960}
- "long_click": Long press an element by text. Params: {"action": "long_click", "target": "text"}
- "scroll": Scroll the screen. Params: {"action": "scroll", "direction": "up|down|left|right"}
- "input": Type text into the focused input field using standard accessibility API (ACTION_SET_TEXT). WARNING: This action DOES NOT WORK in many apps (WeChat, QQ, Douyin, games, UE4/Unity apps, etc.) because they use custom input controls. Use "paste_input" instead for these apps. Only use "input" for simple/standard Android apps with native EditText. Params: {"action": "input", "text": "hello"}
- "launch_app": Open an app. Params: {"action": "launch_app", "package": "com.tencent.mm"} or {"action": "launch_app", "target": "微信"}
- "global_action": Perform global action. Params: {"action": "global_action", "global": "back|home|recents|notifications|quick_settings|power_dialog|lock_screen|take_screenshot|volume_up|volume_down|mute"}
- "find_and_click": Find and click by resource ID or text. Params: {"action": "find_and_click", "resource_id": "com.example:id/btn"} or {"action": "find_and_click", "target": "text"}
- "get_screen": Get current screen elements for analysis. Params: {"action": "get_screen"}
- "drag": Drag from one point to another on screen (useful for game joysticks, sliders, moving objects). Params: {"action": "drag", "start_x": 200, "start_y": 800, "end_x": 300, "end_y": 600, "duration": 500}. Duration is in milliseconds (default 500ms). For game joystick control, use shorter duration (300-500ms). For precise dragging/sliding, use longer duration (800-1500ms).
- "shell_tap": [FOR GAMES/UE4/Unity] Tap at screen coordinates via system input injection. Unlike click_position which uses AccessibilityService gestures, shell_tap injects touch events through InputManagerService (same path as real finger touch). Use this when click_position doesn't work on game engines. Params: {"action": "shell_tap", "x": 540, "y": 960}
- "shell_swipe": [FOR GAMES/UE4/Unity] Swipe on screen via system input injection. Params: {"action": "shell_swipe", "start_x": 200, "start_y": 800, "end_x": 300, "end_y": 600, "duration": 300}. Duration in ms (default 300ms).
- "shell_drag": [FOR GAMES/UE4/Unity] Drag on screen via system input injection (for game joysticks, sliders). Params: {"action": "shell_drag", "start_x": 200, "start_y": 800, "end_x": 300, "end_y": 600, "duration": 500}. Duration in ms (default 500ms). Use longer duration for smooth joystick movement.
- "screenshot": Take a screenshot and send it to the user as an image. You (the AI) will NOT see the image content. Use this when the user asks to "take a screenshot" or "show me the screen". Params: {"action": "screenshot"}
- "screenshot_vision": Take a screenshot and analyze it. Uses local OCR for fast text recognition (~200-500ms). Returns recognized text with position info. If a vision model is configured AND (OCR finds no text OR force_vision=true), the screenshot will also be sent to the vision model for detailed visual analysis (slower but understands icons/images/layout). Use this when you need to understand what's displayed on screen. Params: {"action": "screenshot_vision"} or {"action": "screenshot_vision", "force_vision": true} (force_vision only works when vision model is configured)
- "intent": Execute a generic Android Intent to open URLs, make calls, send messages, set alarms, etc. Params: {"action": "intent", "action_type": "android.intent.action.VIEW", "data": "https://www.baidu.com"}. Optional params: package, class_name, mime_type, extras (object), flags (int). Common examples: open webpage (VIEW+https://), dial number (DIAL+tel:), send SMS (SENDTO+smsto:), set alarm (SET_ALARM+extras for hour/minutes).
- "wait": Wait for a specified duration (useful after page transitions). Params: {"action": "wait", "duration": 3000}. Duration in milliseconds (100-30000ms).
- "paste_input": [PREFERRED INPUT METHOD] The recommended way to input text in most apps. Sets clipboard and pastes text via accessibility service. Works reliably in ALL apps including WeChat, QQ, Douyin, games (UE4/Unity), and any app with custom input controls. ALWAYS prefer this over "input" unless you are certain the target app uses standard Android EditText. You can optionally provide x/y coordinates to click and focus the input field first (recommended for chat apps like WeChat). If no coordinates are provided and a vision model is configured, it will auto-detect the input field. If neither coordinates nor vision model are available, it will try to paste into the currently focused input. Params: {"action": "paste_input", "text": "hello"} or {"action": "paste_input", "text": "hello", "x": 540, "y": 2100}.

Note: The accessibility service must be enabled on the device. If it's not enabled, the tool will return an error. Screenshot requires Android 11 (API 30) or higher.`
}

func (t *AccessibilityTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
"description": "The action to perform: click, click_position, long_click, scroll, input, launch_app, global_action, find_and_click, get_screen, drag, shell_tap, shell_swipe, shell_drag, screenshot, screenshot_vision, intent, wait, paste_input. For game engines (UE4/Unity), use shell_tap/shell_swipe/shell_drag instead of click_position/scroll/drag. For text input: ALWAYS prefer paste_input (works in all apps). Only use input for simple standard apps with native EditText.",
				"enum":        []string{"click", "click_position", "long_click", "scroll", "input", "launch_app", "global_action", "find_and_click", "get_screen", "drag", "shell_tap", "shell_swipe", "shell_drag", "screenshot", "screenshot_vision", "intent", "wait", "paste_input"},
			},
			"target": map[string]any{
				"type":        "string",
				"description": "Target text to find on screen (for click, long_click, find_and_click, launch_app)",
			},
			"x": map[string]any{
				"type":        "number",
				"description": "X coordinate for click_position or shell_tap",
			},
			"y": map[string]any{
				"type":        "number",
				"description": "Y coordinate for click_position or shell_tap",
			},
			"text": map[string]any{
				"type":        "string",
				"description": "Text to input (for input action)",
			},
			"package": map[string]any{
				"type":        "string",
				"description": "App package name (for launch_app action, e.g. com.tencent.mm for WeChat)",
			},
			"direction": map[string]any{
				"type":        "string",
				"description": "Scroll direction: up, down, left, right",
				"enum":        []string{"up", "down", "left", "right"},
			},
			"global": map[string]any{
				"type":        "string",
				"description": "Global action: back, home, recents, notifications, quick_settings, power_dialog, lock_screen, take_screenshot, volume_up, volume_down, mute",
				"enum":        []string{"back", "home", "recents", "notifications", "quick_settings", "power_dialog", "lock_screen", "take_screenshot", "volume_up", "volume_down", "mute"},
			},
			"resource_id": map[string]any{
				"type":        "string",
				"description": "Android resource ID for find_and_click (e.g. com.example:id/button)",
			},
			"start_x": map[string]any{
				"type":        "number",
				"description": "Start X coordinate for drag/shell_swipe/shell_drag",
			},
			"start_y": map[string]any{
				"type":        "number",
				"description": "Start Y coordinate for drag/shell_swipe/shell_drag",
			},
			"end_x": map[string]any{
				"type":        "number",
				"description": "End X coordinate for drag/shell_swipe/shell_drag",
			},
			"end_y": map[string]any{
				"type":        "number",
				"description": "End Y coordinate for drag/shell_swipe/shell_drag",
			},
			"duration": map[string]any{
				"type":        "number",
				"description": "Duration in milliseconds for drag/shell_swipe/shell_drag/wait. Default: drag=500ms, shell_swipe=300ms, shell_drag=500ms. For wait action: 100-30000ms.",
			},
			"force_vision": map[string]any{
				"type":        "boolean",
				"description": "For screenshot_vision only: force using vision model for analysis even when OCR has text. Use this on a second call when OCR-only result was insufficient.",
			},
			"action_type": map[string]any{
				"type":        "string",
				"description": "Intent action type (for intent action). Examples: android.intent.action.VIEW, android.intent.action.DIAL, android.intent.action.SENDTO, android.intent.action.SET_ALARM",
			},
			"data": map[string]any{
				"type":        "string",
				"description": "Intent URI data (for intent action). Examples: https://www.example.com, tel:10086, smsto:10086",
			},
			"class_name": map[string]any{
				"type":        "string",
				"description": "Target Activity class name (for intent action, requires package). Example: com.tencent.mm.ui.LauncherUI",
			},
			"mime_type": map[string]any{
				"type":        "string",
				"description": "Intent MIME type (for intent action). Examples: text/plain, image/jpeg",
			},
			"extras": map[string]any{
				"type":        "object",
				"description": "Intent extras as key-value pairs (for intent action). Values can be string, number, or boolean. Example: {\"android.intent.extra.alarm.HOUR\": 8, \"android.intent.extra.alarm.MINUTES\": 30}",
			},
			"flags": map[string]any{
				"type":        "number",
				"description": "Intent flags (for intent action). Default: 268435456 (FLAG_ACTIVITY_NEW_TASK). See Android Intent flags documentation.",
			},
		},
		"required": []string{"action"},
	}
}

// SetActionCallback 设置向 Android 客户端发送操作指令的回调函数。
func (t *AccessibilityTool) SetActionCallback(callback ActionExecuteCallback) {
	t.callback = callback
}

// SetMediaStore 设置用于截图保存的 MediaStore。
func (t *AccessibilityTool) SetMediaStore(store media.MediaStore) {
	t.mediaStore = store
}

// SetVisionProvider 设置视觉模型 Provider，用于 screenshot_vision 时调用视觉模型分析截图。
// 如果未设置，screenshot_vision 将使用本地 OCR 纯文本方案。
func (t *AccessibilityTool) SetVisionProvider(provider VisionProvider, model string) {
	t.visionProvider = provider
	t.visionModel = model
	logger.InfoCF("tool", "Vision model configured for accessibility tool", map[string]any{
		"model": model,
	})
}

// DeliverResult 由 PicoChannel 调用，将 Android 端返回的执行结果投递到等待的工具调用。
func (t *AccessibilityTool) DeliverResult(requestID string, result ActionResult) {
	if ch, ok := t.pendingResults.Load(requestID); ok {
		select {
		case ch.(chan ActionResult) <- result:
		default:
			// channel 已满或已关闭，忽略
		}
	}
}

func (t *AccessibilityTool) Execute(ctx context.Context, args map[string]any) *ToolResult {
	if t.callback == nil {
		return ErrorResult("Accessibility action not available: no connected Android device supports this feature")
	}

	action, _ := args["action"].(string)
	if action == "" {
		return ErrorResult("Parameter 'action' is required")
	}

	// 修正 LLM 可能生成的畸形 action 格式
	// 某些 LLM（如智谱 GLM）会将参数用 XML 标签嵌入 action 字符串：
	// "click<arg_key>target</arg_key><arg_value>领取" → action="click", target="领取"
	if strings.Contains(action, "<arg_key>") || strings.Contains(action, "<arg_value>") {
		fixedAction, extractedArgs := fixMalformedAction(action)
		logger.WarnCF("tool", "Fixed malformed action format from LLM", map[string]any{
			"original": action, "fixed": fixedAction, "extracted_args": extractedArgs,
		})
		action = fixedAction
		args["action"] = fixedAction
		for k, v := range extractedArgs {
			if _, exists := args[k]; !exists {
				args[k] = v
			}
		}
	}

	// 修正已废弃的 action 别名（LLM 可能仍会自行生成这些名称）
	actionAliases := map[string]string{
		"wechat_input": "paste_input",
	}
	if canonical, ok := actionAliases[action]; ok {
		logger.WarnCF("tool", "Corrected deprecated action alias", map[string]any{
			"original": action, "corrected": canonical,
		})
		action = canonical
		args["action"] = canonical
	}

	// 获取当前通道和会话信息
	channel := ToolChannel(ctx)
	chatID := ToolChatID(ctx)
	if channel == "" || chatID == "" {
		return ErrorResult("Cannot execute accessibility action: no active channel/chat context")
	}

	// ===== paste_input 特殊处理：先通过视觉模型识别输入框位置 =====
	if action == "paste_input" {
		return t.handlePasteInput(ctx, args, channel, chatID)
	}

	// 其他 action 走通用转发流程
	return t.sendAndWaitAction(ctx, action, args, channel, chatID)
}

// sendAndWaitAction 将操作转发给 Android 端并等待结果（通用流程）。
func (t *AccessibilityTool) sendAndWaitAction(ctx context.Context, action string, args map[string]any, channel, chatID string) *ToolResult {
	requestID := uuid.New().String()
	resultCh := make(chan ActionResult, 1)
	t.pendingResults.Store(requestID, resultCh)
	defer func() {
		t.pendingResults.Delete(requestID)
		close(resultCh)
	}()

	actionPayload := make(map[string]any)
	for k, v := range args {
		actionPayload[k] = v
	}

	if err := t.callback(channel, chatID, requestID, actionPayload); err != nil {
		return ErrorResult(fmt.Sprintf("Failed to send accessibility action to device: %v", err))
	}

	timeout := 15 * time.Second
	select {
	case result := <-resultCh:
		if !result.Success {
			return ErrorResult(fmt.Sprintf("Accessibility action '%s' failed: %s", action, result.Message))
		}

		switch action {
		case "screenshot":
			return t.handleScreenshotForUser(result, channel, chatID)
		case "screenshot_vision":
			forceVision, _ := args["force_vision"].(bool)
			return t.handleScreenshotForVision(result, forceVision)
		default:
			return SilentResult(fmt.Sprintf("Accessibility action '%s' succeeded: %s", action, result.Message))
		}
	case <-time.After(timeout):
		return ErrorResult(fmt.Sprintf("Accessibility action '%s' timed out after %v - the device may not have responded", action, timeout))
	case <-ctx.Done():
		return ErrorResult(fmt.Sprintf("Accessibility action '%s' was cancelled", action))
	}
}

// handlePasteInput 处理 paste_input action：通过剪贴板粘贴文字到输入框。
// 流程：
//   1. 如果 LLM 已提供 x/y 坐标 → 直接使用这些坐标
//   2. 如果有视觉模型 → 截图 + 视觉模型识别输入框位置
//   3. 如果都没有 → 直接转发给 Android 端（Android 端会查找 focused/editable node）
// Android 端的 pasteInputWithCoordinates 实现了多层回退策略（点击聚焦→剪贴板粘贴→
// ACTION_SET_TEXT→查找editable node→长按粘贴菜单→keyevent PASTE）。
func (t *AccessibilityTool) handlePasteInput(ctx context.Context, args map[string]any, channel, chatID string) *ToolResult {
	text, _ := args["text"].(string)
	if text == "" {
		return ErrorResult("paste_input requires 'text' parameter")
	}

	// 检查 LLM 是否已经提供了 x/y 坐标
	userX, hasX := toFloat64(args["x"])
	userY, hasY := toFloat64(args["y"])
	hasCoordinates := hasX && hasY && userX > 0 && userY > 0

	if hasCoordinates {
		// LLM 已经提供了坐标（例如从 wechat SKILL.md 指引中获得），直接使用
		logger.InfoCF("tool", "paste_input: using LLM-provided coordinates", map[string]any{
			"x": userX, "y": userY, "text": text,
		})
		pasteArgs := map[string]any{
			"action": "paste_input",
			"text":   text,
			"x":      int(userX),
			"y":      int(userY),
		}
		return t.sendAndWaitAction(ctx, "paste_input", pasteArgs, channel, chatID)
	}

	// 没有用户提供的坐标，尝试通过视觉模型定位输入框
	if t.visionProvider != nil {
		logger.InfoCF("tool", "paste_input: taking screenshot for vision model to locate input field", map[string]any{
			"text": text,
		})

		// 步骤1：发送 screenshot_vision 获取截图 + OCR
		screenshotResult := t.sendAndWaitRaw(ctx, map[string]any{"action": "screenshot_vision"}, channel, chatID)
		if screenshotResult != nil && screenshotResult.Success {
			var ocrResult struct {
				OCRText      string `json:"ocr_text"`
				ScreenWidth  int    `json:"screen_width"`
				ScreenHeight int    `json:"screen_height"`
				ImageBase64  string `json:"image_base64"`
			}
			if err := json.Unmarshal([]byte(screenshotResult.Message), &ocrResult); err == nil && ocrResult.ImageBase64 != "" {
				logger.InfoCF("tool", "paste_input: calling vision model to locate input field", map[string]any{
					"model":         t.visionModel,
					"screen_width":  ocrResult.ScreenWidth,
					"screen_height": ocrResult.ScreenHeight,
				})

				inputX, inputY, err := t.locateInputFieldByVision(ocrResult.ImageBase64, ocrResult.OCRText, ocrResult.ScreenWidth, ocrResult.ScreenHeight)
				if err == nil {
					logger.InfoCF("tool", "paste_input: vision model located input field", map[string]any{
						"x": inputX, "y": inputY,
						"screen_width": ocrResult.ScreenWidth, "screen_height": ocrResult.ScreenHeight,
					})
					pasteArgs := map[string]any{
						"action": "paste_input",
						"text":   text,
						"x":      int(inputX),
						"y":      int(inputY),
					}
					return t.sendAndWaitAction(ctx, "paste_input", pasteArgs, channel, chatID)
				}
				logger.WarnCF("tool", "paste_input: vision model failed to locate input field, falling back to no-coordinate mode", map[string]any{
					"error": err.Error(),
				})
			}
		}
	}

	// 最终回退：直接转发给 Android 端，不提供坐标
	// Android 端的 pasteInputWithCoordinates 会查找 focused node / editable node 来执行粘贴
	logger.InfoCF("tool", "paste_input: forwarding to Android without coordinates (will rely on focused/editable node detection)", map[string]any{
		"text":           text,
		"has_vision":     t.visionProvider != nil,
	})
	pasteArgs := map[string]any{
		"action": "paste_input",
		"text":   text,
	}
	return t.sendAndWaitAction(ctx, "paste_input", pasteArgs, channel, chatID)
}

// sendAndWaitRaw 发送操作给 Android 端并返回原始 ActionResult（不经过特殊处理）。
// 用于 paste_input 流程中获取截图等中间步骤。
func (t *AccessibilityTool) sendAndWaitRaw(ctx context.Context, args map[string]any, channel, chatID string) *ActionResult {
	requestID := uuid.New().String()
	resultCh := make(chan ActionResult, 1)
	t.pendingResults.Store(requestID, resultCh)
	defer func() {
		t.pendingResults.Delete(requestID)
		close(resultCh)
	}()

	if err := t.callback(channel, chatID, requestID, args); err != nil {
		return &ActionResult{Success: false, Message: err.Error()}
	}

	timeout := 15 * time.Second
	select {
	case result := <-resultCh:
		return &result
	case <-time.After(timeout):
		return &ActionResult{Success: false, Message: "timed out waiting for device response"}
	case <-ctx.Done():
		return &ActionResult{Success: false, Message: "cancelled"}
	}
}

// locateInputFieldByVision 调用视觉模型分析截图，识别输入框的坐标位置。
// 返回 (x, y, error)，坐标是输入框中心点的屏幕绝对像素坐标。
func (t *AccessibilityTool) locateInputFieldByVision(imageBase64, ocrText string, screenWidth, screenHeight int) (float64, float64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	prompt := fmt.Sprintf(`Analyze this Android phone screenshot. I need to find the text input field (where the user can type text) on the screen.

Please identify the input field/text box and return its CENTER coordinates in pixels.
The screen resolution is %dx%d.

IMPORTANT: You MUST respond with ONLY a JSON object in this exact format, no other text:
{"x": <number>, "y": <number>, "description": "<brief description of the input field>"}

If there are multiple input fields, choose the most prominent/active one (usually at the bottom for chat apps, or the currently focused one).
If you cannot find any input field, respond with:
{"x": -1, "y": -1, "description": "no input field found"}`, screenWidth, screenHeight)

	if ocrText != "" {
		prompt += fmt.Sprintf("\n\nFor reference, OCR detected the following text on screen:\n%s", ocrText)
	}

	imageURL := fmt.Sprintf("data:image/jpeg;base64,%s", imageBase64)
	messages := []protocoltypes.Message{
		{
			Role:    "user",
			Content: prompt,
			Media:   []string{imageURL},
		},
	}

	resp, err := t.visionProvider.Chat(ctx, messages, nil, t.visionModel, map[string]any{
		"max_tokens":  256,
		"temperature": 0.1,
	})
	if err != nil {
		return 0, 0, fmt.Errorf("vision model API call failed: %w", err)
	}
	if resp == nil || resp.Content == "" {
		return 0, 0, fmt.Errorf("vision model returned empty response")
	}

	logger.InfoCF("tool", "Vision model input field detection response", map[string]any{
		"response": resp.Content,
		"model":    t.visionModel,
	})

	// 解析视觉模型返回的 JSON
	x, y, err := parseInputFieldCoordinates(resp.Content)
	if err != nil {
		return 0, 0, err
	}

	// 校验坐标是否在屏幕范围内
	if x < 0 || y < 0 || (screenWidth > 0 && x > float64(screenWidth)) || (screenHeight > 0 && y > float64(screenHeight)) {
		return 0, 0, fmt.Errorf("vision model returned out-of-bounds coordinates: (%.0f, %.0f), screen: %dx%d", x, y, screenWidth, screenHeight)
	}

	return x, y, nil
}

// parseInputFieldCoordinates 从视觉模型的响应文本中提取输入框坐标。
// 支持从 JSON 字符串或包含 JSON 的 Markdown 代码块中提取。
func parseInputFieldCoordinates(response string) (float64, float64, error) {
	response = strings.TrimSpace(response)

	// 尝试从 Markdown 代码块中提取 JSON
	jsonStr := response
	if idx := strings.Index(response, "```json"); idx >= 0 {
		start := idx + len("```json")
		if end := strings.Index(response[start:], "```"); end >= 0 {
			jsonStr = strings.TrimSpace(response[start : start+end])
		}
	} else if idx := strings.Index(response, "```"); idx >= 0 {
		start := idx + len("```")
		if end := strings.Index(response[start:], "```"); end >= 0 {
			jsonStr = strings.TrimSpace(response[start : start+end])
		}
	}

	// 尝试从花括号提取 JSON
	if !strings.HasPrefix(jsonStr, "{") {
		if lbrace := strings.Index(jsonStr, "{"); lbrace >= 0 {
			if rbrace := strings.LastIndex(jsonStr, "}"); rbrace > lbrace {
				jsonStr = jsonStr[lbrace : rbrace+1]
			}
		}
	}

	var result struct {
		X           float64 `json:"x"`
		Y           float64 `json:"y"`
		Description string  `json:"description"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		return 0, 0, fmt.Errorf("failed to parse vision model response as JSON: %w (response: %s)", err, response)
	}

	if result.X == -1 && result.Y == -1 {
		desc := result.Description
		if desc == "" {
			desc = "no input field found on screen"
		}
		return 0, 0, fmt.Errorf("vision model could not find input field: %s", desc)
	}

	return result.X, result.Y, nil
}

// handleScreenshotForUser 处理截图并通过 MediaStore 发送图片给用户。
// 如果 MediaStore 不可用，回退为将图片作为 LLMMedia 返回，让多模态 LLM 直接"看到"截图。
func (t *AccessibilityTool) handleScreenshotForUser(result ActionResult, channel, chatID string) *ToolResult {
	// Android 端 screenshot 返回纯 base64 字符串（不带 data URI 前缀）
	// 构建 data URL 供 LLMMedia 回退使用
	raw := result.Message
	imageURL := raw
	if strings.HasPrefix(raw, "data:") {
		// 兼容旧版本可能带前缀的情况
		imageURL = raw
	} else {
		imageURL = "data:image/jpeg;base64," + raw
	}

	if t.mediaStore == nil {
		// 没有 MediaStore，回退为 LLMMedia（让多模态 LLM 直接看到图片）
		tr := SilentResult("Screenshot taken. The image is attached for your analysis.")
		tr.LLMMedia = []string{imageURL}
		return tr
	}

	// 将 base64 解码并写入临时文件
	tmpPath, err := t.saveBase64ToTempFile(result.Message, "screenshot", ".jpg")
	if err != nil {
		// 解码失败，回退为 LLMMedia
		logger.WarnCF("tool", "Failed to save screenshot to temp file, falling back to LLMMedia", map[string]any{
			"error": err.Error(),
		})
		tr := SilentResult("Screenshot taken. The image is attached for your analysis.")
		tr.LLMMedia = []string{imageURL}
		return tr
	}

	// 注册到 MediaStore
	scope := fmt.Sprintf("tool:screenshot:%s:%s", channel, chatID)
	ref, err := t.mediaStore.Store(tmpPath, media.MediaMeta{
		Filename:    "screenshot.jpg",
		ContentType: "image/jpeg",
		Source:      "tool:accessibility_action:screenshot",
	}, scope)
	if err != nil {
		os.Remove(tmpPath)
		// MediaStore 失败，回退为 LLMMedia
		logger.WarnCF("tool", "Failed to store screenshot in media store, falling back to LLMMedia", map[string]any{
			"error": err.Error(),
		})
		tr := SilentResult("Screenshot taken. The image is attached for your analysis.")
		tr.LLMMedia = []string{imageURL}
		return tr
	}

	// 返回 MediaResult：图片通过 bus 发送给用户，LLM 只收到文本通知
	return MediaResult("Screenshot taken and sent to user as an image. You (the AI) cannot see the image content.", []string{ref}).WithResponseHandled()
}

// handleScreenshotForVision 处理截图 + 本地 OCR 的结果，采用分层策略：
// 1. 如果 OCR 有有效文本内容 → 只返回 OCR 文本（快速，不调视觉模型）
// 2. 如果 OCR 无有效内容 → 调用视觉模型分析截图（慢，但能理解图形/图标）
// 3. 如果 forceVision=true → 强制调用视觉模型（LLM 认为 OCR 结果不够时的重试）
func (t *AccessibilityTool) handleScreenshotForVision(result ActionResult, forceVision bool) *ToolResult {
	// result.Message 是 Android 端返回的 OCR JSON：
	// {"ocr_text": "全部文字", "blocks": [...], "block_count": N, "screen_width": W, "screen_height": H, "image_base64": "..."}

	var ocrResult struct {
		OCRText      string `json:"ocr_text"`
		BlockCount   int    `json:"block_count"`
		ScreenWidth  int    `json:"screen_width"`
		ScreenHeight int    `json:"screen_height"`
		ImageBase64  string `json:"image_base64"`
		Blocks       []struct {
			Text   string `json:"text"`
			Bounds struct {
				Left   int `json:"left"`
				Top    int `json:"top"`
				Right  int `json:"right"`
				Bottom int `json:"bottom"`
			} `json:"bounds"`
		} `json:"blocks"`
	}

	if err := json.Unmarshal([]byte(result.Message), &ocrResult); err != nil {
		// JSON 解析失败，可能是旧版 Android 端返回的 base64 图片数据
		return SilentResult("Screenshot captured, but OCR result parsing failed. The device may need to be updated.")
	}

	// 构建 OCR 文本描述
	ocrDesc := t.buildOCRDescription(ocrResult.OCRText, ocrResult.ScreenWidth, ocrResult.ScreenHeight,
		ocrResult.BlockCount, ocrResult.Blocks)

	// 判断 OCR 是否有有效内容（去除空白后有文字，且至少有 1 个文字块）
	ocrHasContent := strings.TrimSpace(ocrResult.OCRText) != "" && ocrResult.BlockCount > 0

	// 分层策略：
	// - OCR 有内容 且 不强制视觉 → 只返回 OCR 文本（快速路径）
	// - OCR 无内容 或 强制视觉 → 调用视觉模型（慢速路径）
	needVision := forceVision || !ocrHasContent

	if needVision && t.visionProvider != nil && ocrResult.ImageBase64 != "" {
		logger.InfoCF("tool", "Calling vision model for screenshot analysis", map[string]any{
			"reason":       map[bool]string{true: "force_vision requested", false: "OCR has no effective content"}[forceVision],
			"ocr_has_text": ocrHasContent,
			"model":        t.visionModel,
		})

		visionDesc, err := t.callVisionModel(ocrResult.ImageBase64, ocrResult.OCRText)
		if err != nil {
			logger.WarnCF("tool", "Vision model call failed, falling back to OCR only", map[string]any{
				"error": err.Error(),
				"model": t.visionModel,
			})
			// 视觉模型失败，回退到纯 OCR
			if ocrHasContent {
				return SilentResult(ocrDesc + "\n\n[Note: Vision model analysis was requested but failed. Above is OCR-only result.]")
			}
			return SilentResult(ocrDesc)
		}

		// 视觉模型分析成功，合并结果
		combined := fmt.Sprintf("=== Vision Model Analysis (by %s) ===\n%s\n\n=== OCR Text Details ===\n%s",
			t.visionModel, visionDesc, ocrDesc)
		return SilentResult(combined)
	}

	// 快速路径：OCR 有内容，直接返回文本
	if ocrHasContent {
		logger.InfoCF("tool", "Returning OCR-only result (fast path)", map[string]any{
			"block_count": ocrResult.BlockCount,
			"text_len":    len(ocrResult.OCRText),
		})
		// 只有配置了视觉模型时，才提示 LLM 可以用 force_vision 重试
		if t.visionProvider != nil {
			hint := "\n\n[Tip: This is OCR-only result (fast). If you cannot determine the screen content from the text above (e.g. the screen has images/icons/graphics that matter), call screenshot_vision again with force_vision=true for detailed vision model analysis.]"
			return SilentResult(ocrDesc + hint)
		}
		return SilentResult(ocrDesc)
	}

	// OCR 无内容，且没有视觉模型可用
	return SilentResult(ocrDesc)
}

// buildOCRDescription 将 OCR 结果构建为 LLM 可读的文本描述。
func (t *AccessibilityTool) buildOCRDescription(ocrText string, screenWidth, screenHeight, blockCount int,
	blocks []struct {
		Text   string `json:"text"`
		Bounds struct {
			Left   int `json:"left"`
			Top    int `json:"top"`
			Right  int `json:"right"`
			Bottom int `json:"bottom"`
		} `json:"bounds"`
	}) string {

	if ocrText == "" {
		return fmt.Sprintf(
			"Screenshot captured (screen: %dx%d). No text was recognized on the screen. The screen may contain mostly images/graphics. Try using accessibility elements (get_screen) for more information.",
			screenWidth, screenHeight,
		)
	}

	desc := fmt.Sprintf("Screenshot OCR result (screen: %dx%d, %d text blocks detected):\n\n",
		screenWidth, screenHeight, blockCount)

	for i, block := range blocks {
		if i >= 30 {
			desc += fmt.Sprintf("\n... and %d more blocks (truncated)", blockCount-30)
			break
		}
		desc += fmt.Sprintf("[%d] \"%s\" (at y:%d~%d, x:%d~%d)\n",
			i+1, block.Text,
			block.Bounds.Top, block.Bounds.Bottom,
			block.Bounds.Left, block.Bounds.Right,
		)
	}

	desc += fmt.Sprintf("\n--- Full screen text ---\n%s", ocrText)
	return desc
}

// callVisionModel 调用视觉模型分析截图，返回模型的文本描述。
// 这是一个独立的 API 调用，不会影响主 LLM 的对话上下文。
func (t *AccessibilityTool) callVisionModel(imageBase64, ocrContext string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 构建视觉模型的提示
	prompt := "Please analyze this Android phone screenshot in detail. Describe:\n" +
		"1. What app or screen is shown\n" +
		"2. The main content and visual elements (icons, images, buttons, etc.)\n" +
		"3. Any actionable UI elements and their approximate positions\n" +
		"4. The overall layout and state of the screen\n" +
		"Be concise but thorough. Reply in the same language as the on-screen text."

	if ocrContext != "" {
		prompt += fmt.Sprintf("\n\nFor reference, OCR detected the following text on screen:\n%s", ocrContext)
	}

	imageURL := fmt.Sprintf("data:image/jpeg;base64,%s", imageBase64)

	messages := []protocoltypes.Message{
		{
			Role:    "user",
			Content: prompt,
			Media:   []string{imageURL},
		},
	}

	resp, err := t.visionProvider.Chat(ctx, messages, nil, t.visionModel, map[string]any{
		"max_tokens":  1024,
		"temperature": 0.3,
	})
	if err != nil {
		return "", fmt.Errorf("vision model API call failed: %w", err)
	}

	if resp == nil || resp.Content == "" {
		return "", fmt.Errorf("vision model returned empty response")
	}

	logger.InfoCF("tool", "Vision model analysis completed", map[string]any{
		"model":        t.visionModel,
		"response_len": len(resp.Content),
	})
	if resp.Usage != nil {
		logger.InfoCF("tool", "Vision model token usage", map[string]any{
			"prompt_tokens": resp.Usage.PromptTokens,
			"output_tokens": resp.Usage.CompletionTokens,
		})
	}

	return resp.Content, nil
}

// saveBase64ToTempFile 将 base64 字符串解码并保存到临时文件。
// 支持纯 base64 和 data URI 格式（如 "data:image/jpeg;base64,..."）。
func (t *AccessibilityTool) saveBase64ToTempFile(base64Str, prefix, ext string) (string, error) {
	// 去掉 data URI 前缀（如 "data:image/jpeg;base64,"）
	raw := base64Str
	if idx := strings.Index(raw, ";base64,"); idx >= 0 {
		raw = raw[idx+len(";base64,"):]
	}

	data, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return "", fmt.Errorf("base64 decode failed: %w", err)
	}

	tmpDir := os.TempDir()
	filename := fmt.Sprintf("%s_%s%s", prefix, uuid.New().String()[:8], ext)
	tmpPath := filepath.Join(tmpDir, filename)

	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return "", fmt.Errorf("write temp file failed: %w", err)
	}

	return tmpPath, nil
}

// HasPendingRequest 检查是否有指定 requestID 的待处理请求。
func (t *AccessibilityTool) HasPendingRequest(requestID string) bool {
	_, ok := t.pendingResults.Load(requestID)
	return ok
}

// fixMalformedAction 修正 LLM 生成的畸形 action 格式。
// 某些 LLM 会把参数用 XML 标签嵌入 action 字符串：
//   "click<arg_key>target</arg_key><arg_value>领取" → action="click", {"target": "领取"}
//   "scroll<arg_key>direction</arg_key><arg_value>down" → action="scroll", {"direction": "down"}
// 此函数提取真实的 action 名称和嵌入的参数。
func fixMalformedAction(malformedAction string) (string, map[string]any) {
	extractedArgs := make(map[string]any)

	// 提取真正的 action 名称（第一个 < 之前的内容）
	realAction := malformedAction
	if idx := strings.Index(malformedAction, "<"); idx > 0 {
		realAction = strings.TrimSpace(malformedAction[:idx])
	}

	// 使用正则提取 <arg_key>KEY</arg_key><arg_value>VALUE 模式的参数
	// 注意：VALUE 可能没有闭合标签（LLM 有时生成不完整的 XML）
	re := regexp.MustCompile(`<arg_key>([^<]+)</arg_key><arg_value>([^<]*)(?:</arg_value>)?`)
	matches := re.FindAllStringSubmatch(malformedAction, -1)
	for _, match := range matches {
		if len(match) >= 3 {
			key := strings.TrimSpace(match[1])
			value := strings.TrimSpace(match[2])
			if key != "" && value != "" {
				// 尝试将数字字符串转为 float64，与 JSON 解析行为一致
				// 如 "300" → 300.0，"1630" → 1630.0
				if f, err := strconv.ParseFloat(value, 64); err == nil {
					extractedArgs[key] = f
				} else {
					extractedArgs[key] = value
				}
			}
		}
	}

	return realAction, extractedArgs
}

// MarshalActionPayload 将操作参数序列化为 JSON 字符串。
func MarshalActionPayload(action map[string]any) (string, error) {
	data, err := json.Marshal(action)
	if err != nil {
		return "", fmt.Errorf("failed to marshal action payload: %w", err)
	}
	return string(data), nil
}