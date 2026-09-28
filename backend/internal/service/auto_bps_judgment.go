package service

type QualityJudgeConfig struct {
	GroupID int64  `json:"group_id"`
	ModelID string `json:"model_id"`
	Prompt  string `json:"prompt"`
}
type QualityJudgment struct {
	Verdict   string `json:"verdict"`
	Reason    string `json:"reason"`
	AccountID int64  `json:"account_id,omitempty"`
	GroupID   int64  `json:"group_id,omitempty"`
	ModelID   string `json:"model_id,omitempty"`
}
