//go:build integration

package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// 固定目录来源账号已退役：投影保留历史账号 ID 供审计，但读取时一律不启用。
