package runtimeclient

import "retrom/internal/model"

// ContentFailure exposes only known runtime reason codes and safe review guidance.
// Engine-specific detection remains owned by the runtime.
func ContentFailure(code string) *model.RuntimeContentError {
	var message string
	switch code {
	case "RUNTIME_ARCADE_MACHINE_UNKNOWN":
		message = "当前核心未识别此 ROM 集，请检查游戏文件和核心选择。"
	case "RUNTIME_FLYCAST_MACHINE_UNKNOWN":
		message = "当前 Flycast 核心未识别此 ROM 集，请检查游戏文件和核心选择。"
	case "RUNTIME_FLYCAST_MEDIA_UNSUPPORTED":
		message = "当前 Flycast 核心不支持此游戏的媒体格式。"
	case "RUNTIME_FLYCAST_ROM_MISSING":
		message = "游戏包缺少当前 Flycast 核心所需的 ROM 文件，请补齐游戏文件或父包。"
	case "RUNTIME_FLYCAST_DISC_MISSING":
		message = "缺少当前核心所需的 CHD 光盘文件，请补齐配套文件后重新检查。"
	case "RUNTIME_FLYCAST_DISC_INVALID":
		message = "CHD 光盘文件格式无效，请检查文件完整性后重新上传。"
	case "RUNTIME_FLYCAST_DISC_AMBIGUOUS":
		message = "存在多个同名 CHD 光盘文件，请保留与当前 ROM 集对应的配套文件。"
	case "RUNTIME_FLYCAST_PLATFORM_MISMATCH":
		message = "此 ROM 集与当前平台不匹配，请检查游戏目录与核心选择。"
	case "RUNTIME_PARENT_MISSING":
		message = "缺少当前核心所需的 Parent 父包，请补齐父包后重新检查。"
	case "RUNTIME_ENTRY_MISSING":
		message = "已保存的启动文件不存在，请重新选择游戏启动文件。"
	case "RUNTIME_RPG_ENTRY_INVALID":
		message = "所选文件不是当前 RPG Maker 版本的有效入口，请重新选择入口文件。"
	case "RUNTIME_CONTENT_UNAVAILABLE":
		message = "无法读取游戏内容，请检查文件后重新检查。"
	case "RUNTIME_CONTENT_UNSUPPORTED":
		message = "当前核心不支持此游戏内容，请检查游戏文件和核心选择。"
	default:
		return nil
	}
	return &model.RuntimeContentError{Code: code, Message: message}
}
