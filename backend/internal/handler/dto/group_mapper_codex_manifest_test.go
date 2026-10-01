package dto

import (
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// 固定账号 manifest 配置必须在管理端 DTO 完整序列化：漏映射会让编辑对话框回显
// 关闭状态，管理员保存无关字段时会把已开启的配置静默关掉。用户侧 DTO 不携带该字段。
