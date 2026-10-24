// Package usageexport owns the shared transport for authorized user/admin exports.
package usageexport

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// A completion frame is mandatory. Clients never save a partially failed export.
// NDJSON keeps locale-specific column labels and existing CSV/XLSX presentation
// in the UI; the server holds only a batch, while the browser assembles a bounded file.
func Stream(c *gin.Context, s *service.UsageService, actorID int64, filters usagestats.UsageLogFilters, admin bool) {
	controller := http.NewResponseController(c.Writer)
	_ = controller.SetWriteDeadline(time.Now().Add(s.UsageExportTimeout()))
	defer func() { _ = controller.SetWriteDeadline(time.Time{}) }()
	started := false
	count := 0
	write := func(value any) error {
		var data []byte
		var err error
		if admin {
			actor, ok := authz.FromContext(c.Request.Context())
			if !ok {
				return service.ErrAdminPermissionDenied
			}
			data, err = authz.MarshalResponse(value, c.FullPath(), actor, false)
		} else {
			data, err = json.Marshal(value)
		}
		if err != nil {
			return err
		}
		if !started {
			c.Header("Content-Type", "application/x-ndjson")
			c.Header("Cache-Control", "no-store")
			c.Header("X-Accel-Buffering", "no")
			c.Status(http.StatusOK)
			started = true
		}
		if _, err = c.Writer.Write(append(data, '\n')); err != nil {
			return err
		}
		c.Writer.Flush()
		return c.Request.Context().Err()
	}
	err := s.ExportUsage(c.Request.Context(), actorID, filters, func(rows []service.UsageLog) error {
		items := make([]any, 0, len(rows))
		for i := range rows {
			if admin {
				items = append(items, dto.UsageLogFromServiceAdmin(&rows[i]))
			} else {
				items = append(items, dto.UsageLogFromService(&rows[i]))
			}
		}
		if err := write(map[string]any{"type": "rows", "items": items}); err != nil {
			return err
		}
		count += len(rows)
		return nil
	})
	if err != nil {
		logger.FromContext(c.Request.Context()).Warn("usage.export_failed", zap.Int("rows", count), zap.Error(err))
		if !started {
			response.ErrorFrom(c, err)
			return
		}
		message := "Usage export interrupted; no complete file was produced"
		if errors.Is(err, service.ErrUsageExportTooLarge) {
			message = "Export exceeds the row limit; select a smaller date range"
		}
		_ = write(map[string]any{"type": "error", "message": message})
		return
	}
	_ = write(map[string]any{"type": "complete", "rows": count})
}
