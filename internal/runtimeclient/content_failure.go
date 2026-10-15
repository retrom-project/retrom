package runtimeclient

import "retrom/internal/model"

// ContentFailure exposes only known runtime reason codes and safe review guidance.
// Engine-specific detection remains owned by the runtime.
func ContentFailure(code string) *model.RuntimeContentError {
	message, exists := contentMessages[code]
	if !exists {
		return nil
	}
	return &model.RuntimeContentError{Code: code, Message: message}
}

var contentMessages = map[string]string{
	"RUNTIME_ARCADE_MACHINE_UNKNOWN":      "当前核心未识别此 ROM 集，请检查游戏文件和核心选择。",
	"RUNTIME_FLYCAST_MACHINE_UNKNOWN":     "当前 Flycast 核心未识别此 ROM 集，请检查游戏文件和核心选择。",
	"RUNTIME_FLYCAST_MEDIA_UNSUPPORTED":   "当前 Flycast 核心不支持此游戏的媒体格式。",
	"RUNTIME_FLYCAST_ROM_MISSING":         "游戏包缺少当前 Flycast 核心所需的 ROM 文件，请补齐游戏文件或父包。",
	"RUNTIME_FLYCAST_DISC_MISSING":        "缺少当前核心所需的 CHD 光盘文件，请补齐配套文件后重新检查。",
	"RUNTIME_ARCADE_DISK_MISSING":         "缺少当前核心所需的 CHD 磁盘文件，请补齐配套文件后重新检查。",
	"RUNTIME_FLYCAST_DISC_INVALID":        "CHD 光盘文件格式无效，请检查文件完整性后重新上传。",
	"RUNTIME_ARCADE_DISK_INVALID":         "CHD 磁盘文件不符合当前游戏要求，请确认磁盘版本和文件完整性后重新上传。",
	"RUNTIME_FLYCAST_DISC_AMBIGUOUS":      "存在多个同名 CHD 光盘文件，请保留与当前 ROM 集对应的配套文件。",
	"RUNTIME_ARCADE_DISK_AMBIGUOUS":       "CHD 文件无法唯一映射到所需磁盘，请按缺项名称补齐配套文件。",
	"RUNTIME_ARCADE_DISK_PARENT_REQUIRED": "此 CHD 依赖父镜像，请换用不依赖父镜像的完整 CHD 文件。",
	"RUNTIME_FLYCAST_PLATFORM_MISMATCH":   "此 ROM 集与当前平台不匹配，请检查游戏目录与核心选择。",
	"RUNTIME_PARENT_MISSING":              "缺少当前核心所需的 Parent 父包，请补齐父包后重新检查。",
	"RUNTIME_ENTRY_MISSING":               "已保存的启动文件不存在，请重新选择游戏启动文件。",
	"RUNTIME_RPG_ENTRY_INVALID":           "所选文件不是当前 RPG Maker 版本的有效入口，请重新选择入口文件。",
	"RUNTIME_CONTENT_UNAVAILABLE":         "无法读取游戏内容，请检查文件后重新检查。",
	"RUNTIME_CONTENT_UNSUPPORTED":         "当前核心不支持此游戏内容，请检查游戏文件和核心选择。",
}
