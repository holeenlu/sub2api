package service

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

var ErrMediaNotOwned = errors.New("media resource not found")
var ErrMediaPendingCapacity = errors.New("an asynchronous video is still awaiting settlement for this user")

type GatewayMediaVoice struct {
	ID                         string
	UserID, GroupID, AccountID int64
	Metadata                   json.RawMessage
}

// GatewayMediaPricingUnit is the multiplier for a captured UnitCost. It is
// independent of the usage-log BillingMode, which can label flat prices video.
type GatewayMediaPricingUnit string

const (
	GatewayMediaPricingPerRequest     GatewayMediaPricingUnit = "request"
	GatewayMediaPricingPerSecond      GatewayMediaPricingUnit = "video_second"
	GatewayMediaPricingPerOutputToken GatewayMediaPricingUnit = "output_token"
)

type GatewayMediaJob struct {
	Version                                       int64
	HoldAmount                                    float64
	ID, TaskID, State                             string
	UserID, GroupID, AccountID                    int64
	Endpoint                                      GrokMediaEndpoint
	Key                                           *APIKey
	Subscription                                  *UserSubscription
	QuotaPlatform, InboundEndpoint, OriginalModel string
	Pending                                       GrokVideoPendingBilling
	CreatedAt                                     time.Time
	// UnitCost and PricingUnit capture the price and its actual unit at admission.
	// Neither settlement nor hold calculations infer units from BillingMode.
	AccountRateMultiplier float64
	Submitted             bool `json:"-"`
	UnitCost              *CostBreakdown
	PricingUnit           GatewayMediaPricingUnit
	Multiplier            float64
	Result                *OpenAIForwardResult
	Cost                  *CostBreakdown
}

type GatewayMediaRepository interface {
	PutVoice(context.Context, *GatewayMediaVoice) error
	GetVoice(context.Context, int64, int64, string) (*GatewayMediaVoice, error)
	ListVoices(context.Context, int64, int64) ([]GatewayMediaVoice, error)
	DeleteVoice(context.Context, int64, int64, string) error
	CreateJob(context.Context, *GatewayMediaJob) error
	SaveJob(context.Context, *GatewayMediaJob) error
	GetJob(context.Context, int64, int64, string) (*GatewayMediaJob, error)
	ClaimJobs(context.Context, int) ([]GatewayMediaJob, error)
}
