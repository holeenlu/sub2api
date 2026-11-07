package handler

import (
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"go.uber.org/zap"
)

func logRequestBodyReadFailure(reqLog *zap.Logger, req *http.Request, err error) {
	httputil.LogRequestBodyReadFailure(reqLog, req, err, "gateway_handler")
}
