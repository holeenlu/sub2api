package service

import infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"

// Retain only a tombstone for existing rows; this is not a supported provider.
func IsRetiredPlatform(platform string) bool { return platform == "openai_bps" }

var ErrPlatformRetired = infraerrors.BadRequest("PLATFORM_RETIRED", "The standalone OpenAI BPS platform has been removed; use OpenAI OAuth with Excel / BPS")
