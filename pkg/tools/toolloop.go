// PicoClaw - Ultra-lightweight personal AI agent
// Inspired by and based on nanobot: https://github.com/HKUDS/nanobot
// License: MIT
//
// Copyright (c) 2026 PicoClaw contributors

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sync"

	"github.com/sipeed/picoclaw/pkg/logger"
	"github.com/sipeed/picoclaw/pkg/providers"
	"github.com/sipeed/picoclaw/pkg/utils"
)

// ToolLoopConfig configures the tool execution loop.
type ToolLoopConfig struct {
	Provider      providers.LLMProvider
	Model         string
	Tools         *ToolRegistry
	MaxIterations int
	LLMOptions    map[string]any
}

// ToolLoopResult contains the result of running the tool loop.
type ToolLoopResult struct {
	Content    string
	Iterations int
}

// repetitiveActionDetector 检测 LLM 是否陷入重复操作的循环。
// 当 LLM 连续多次调用同一工具的同一 action（如 click_position），且坐标变化很小时，
// 视为"低效循环"并注入提示要求 LLM 改变策略。
type repetitiveActionDetector struct {
	// 最近 N 次工具调用记录
	history []toolCallRecord
	// 触发阈值：连续相同 action 超过此数量时告警
	threshold int
}

type toolCallRecord struct {
	toolName string
	action   string         // action 参数值（如 "click_position"）
	args     map[string]any // 完整参数
}

func newRepetitiveActionDetector(threshold int) *repetitiveActionDetector {
	if threshold <= 0 {
		threshold = 5
	}
	return &repetitiveActionDetector{
		threshold: threshold,
	}
}

// recordAndCheck 记录一次工具调用并检测是否陷入循环。
// 返回告警消息（非空表示检测到循环）。
func (d *repetitiveActionDetector) recordAndCheck(toolName string, args map[string]any) string {
	action, _ := args["action"].(string)

	record := toolCallRecord{
		toolName: toolName,
		action:   action,
		args:     args,
	}
	d.history = append(d.history, record)

	// 只保留最近 threshold*2 条记录
	if len(d.history) > d.threshold*2 {
		d.history = d.history[len(d.history)-d.threshold*2:]
	}

	// 对于无 action 参数的工具，不做循环检测
	if action == "" {
		return ""
	}

	// 检查最近 threshold 次调用是否都是同一工具的同一 action
	if len(d.history) < d.threshold {
		return ""
	}

	recent := d.history[len(d.history)-d.threshold:]
	for _, r := range recent {
		if r.toolName != toolName || r.action != action {
			return ""
		}
	}

	// 连续 threshold 次都是同一个 tool + action，生成告警
	// 额外检查：如果是坐标类操作（click_position, shell_tap），检查坐标变化范围
	if action == "click_position" || action == "shell_tap" {
		if d.isCoordinatesClustered(recent) {
			msg := fmt.Sprintf(
				"⚠️ LOOP DETECTED: You have called '%s' with action '%s' %d times consecutively with similar coordinates, but the goal was not achieved. "+
					"Your current approach is NOT working. You MUST try a completely different strategy: "+
					"1) Use 'get_screen' to re-examine available UI elements; "+
					"2) Try 'find_and_click' with a text/resource_id instead of coordinates; "+
					"3) Try 'scroll' to reveal hidden elements; "+
					"4) Use 'global_action' (back/home) to reset the screen state. "+
					"Do NOT continue clicking similar coordinates.",
				toolName, action, d.threshold,
			)
			logger.WarnCF("toolloop", "Repetitive action loop detected", map[string]any{
				"tool":   toolName,
				"action": action,
				"count":  d.threshold,
				"type":   "coordinate_cluster",
			})
			return msg
		}
	}

	// 通用循环检测：连续相同 action
	msg := fmt.Sprintf(
		"⚠️ LOOP DETECTED: You have called '%s' with action '%s' %d times consecutively without success. "+
			"Your current approach is NOT working. Please try a completely different strategy or report that you cannot complete this task.",
		toolName, action, d.threshold,
	)
	logger.WarnCF("toolloop", "Repetitive action loop detected", map[string]any{
		"tool":   toolName,
		"action": action,
		"count":  d.threshold,
		"type":   "same_action",
	})
	return msg
}

// isCoordinatesClustered 检查一组坐标操作是否聚集在相近区域。
// 如果所有坐标的 X 范围和 Y 范围都在 200px 以内，视为聚集。
func (d *repetitiveActionDetector) isCoordinatesClustered(records []toolCallRecord) bool {
	var minX, maxX, minY, maxY float64
	minX, minY = math.MaxFloat64, math.MaxFloat64
	maxX, maxY = -math.MaxFloat64, -math.MaxFloat64

	for _, r := range records {
		x, xOk := toFloat64(r.args["x"])
		y, yOk := toFloat64(r.args["y"])
		if !xOk || !yOk {
			return false
		}
		if x < minX {
			minX = x
		}
		if x > maxX {
			maxX = x
		}
		if y < minY {
			minY = y
		}
		if y > maxY {
			maxY = y
		}
	}

	// X 和 Y 范围都在 200px 以内视为聚集
	return (maxX-minX) <= 200 && (maxY-minY) <= 200
}

// toFloat64 从 any 类型中提取 float64 值（兼容 float64、int、string）。
func toFloat64(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	case string:
		// 不做字符串转数字，避免误判
		return 0, false
	default:
		return 0, false
	}
}

// RunToolLoop executes the LLM + tool call iteration loop.
// This is the core agent logic that can be reused by both main agent and subagents.
func RunToolLoop(
	ctx context.Context,
	config ToolLoopConfig,
	messages []providers.Message,
	channel, chatID string,
) (*ToolLoopResult, error) {
	iteration := 0
	var finalContent string
	detector := newRepetitiveActionDetector(5)

	for iteration < config.MaxIterations {
		iteration++

		logger.DebugCF("toolloop", "LLM iteration",
			map[string]any{
				"iteration": iteration,
				"max":       config.MaxIterations,
			})

		// 1. Build tool definitions
		var providerToolDefs []providers.ToolDefinition
		if config.Tools != nil {
			providerToolDefs = config.Tools.ToProviderDefs()
		}

		// 2. Set default LLM options
		llmOpts := config.LLMOptions
		if llmOpts == nil {
			llmOpts = map[string]any{}
		}
		// 3. Call LLM
		response, err := config.Provider.Chat(ctx, messages, providerToolDefs, config.Model, llmOpts)
		if err != nil {
			logger.ErrorCF("toolloop", "LLM call failed",
				map[string]any{
					"iteration": iteration,
					"error":     err.Error(),
				})
			return nil, fmt.Errorf("LLM call failed: %w", err)
		}

		// 4. If no tool calls, we're done
		if len(response.ToolCalls) == 0 {
			finalContent = response.Content
			logger.InfoCF("toolloop", "LLM response without tool calls (direct answer)",
				map[string]any{
					"iteration":     iteration,
					"content_chars": len(finalContent),
				})
			break
		}

		normalizedToolCalls := make([]providers.ToolCall, 0, len(response.ToolCalls))
		for _, tc := range response.ToolCalls {
			normalizedToolCalls = append(normalizedToolCalls, providers.NormalizeToolCall(tc))
		}

		// 5. Log tool calls
		toolNames := make([]string, 0, len(normalizedToolCalls))
		for _, tc := range normalizedToolCalls {
			toolNames = append(toolNames, tc.Name)
		}
		logger.InfoCF("toolloop", "LLM requested tool calls",
			map[string]any{
				"tools":     toolNames,
				"count":     len(normalizedToolCalls),
				"iteration": iteration,
			})

		// 6. Build assistant message with tool calls
		assistantMsg := providers.Message{
			Role:    "assistant",
			Content: response.Content,
		}
		for _, tc := range normalizedToolCalls {
			argumentsJSON, _ := json.Marshal(tc.Arguments)
			assistantMsg.ToolCalls = append(assistantMsg.ToolCalls, providers.ToolCall{
				ID:        tc.ID,
				Type:      "function",
				Name:      tc.Name,
				Arguments: tc.Arguments,
				Function: &providers.FunctionCall{
					Name:      tc.Name,
					Arguments: string(argumentsJSON),
				},
			})
		}
		messages = append(messages, assistantMsg)

		// 7. Execute tool calls in parallel
		type indexedResult struct {
			result *ToolResult
			tc     providers.ToolCall
		}

		results := make([]indexedResult, len(normalizedToolCalls))
		var wg sync.WaitGroup

		for i, tc := range normalizedToolCalls {
			results[i].tc = tc

			wg.Add(1)
			go func(idx int, tc providers.ToolCall) {
				defer wg.Done()

				argsJSON, _ := json.Marshal(tc.Arguments)
				argsPreview := utils.Truncate(string(argsJSON), 200)
				logger.InfoCF("toolloop", fmt.Sprintf("Tool call: %s(%s)", tc.Name, argsPreview),
					map[string]any{
						"tool":      tc.Name,
						"iteration": iteration,
					})

				var toolResult *ToolResult
				if config.Tools != nil {
					toolResult = config.Tools.ExecuteWithContext(ctx, tc.Name, tc.Arguments, channel, chatID, nil)
				} else {
					toolResult = ErrorResult("No tools available")
				}
				results[idx].result = toolResult
			}(i, tc)
		}
		wg.Wait()

		// Append results in original order
		for _, r := range results {
			contentForLLM := r.result.ContentForLLM()

			messages = append(messages, providers.Message{
				Role:       "tool",
				Content:    contentForLLM,
				ToolCallID: r.tc.ID,
			})

			// 记录工具调用并检测循环
			if warning := detector.recordAndCheck(r.tc.Name, r.tc.Arguments); warning != "" {
				messages = append(messages, providers.Message{
					Role:    "user",
					Content: warning,
				})
			}
		}
	}

	return &ToolLoopResult{
		Content:    finalContent,
		Iterations: iteration,
	}, nil
}
