package admin

import (
	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func init() {
	authz.DefineRequestSchema("account.sso", GrokSSOToOAuthRequest{})
	authz.DefineRequestSchema("account.pat", OpenAICodexPATCreateRequest{})
	authz.DefineRequestSchema("account.codex", CodexSessionImportRequest{})
	authz.DefineRequestSchema("group.create", CreateGroupRequest{})
	authz.DefineRequestSchema("group.update", UpdateGroupRequest{})
	authz.DefineRequestSchema("channel.create", createChannelRequest{})
	authz.DefineRequestSchema("channel.update", updateChannelRequest{})
	authz.DefineRequestSchema("plan.create", service.CreatePlanRequest{})
	authz.DefineRequestSchema("plan.update", service.UpdatePlanRequest{})
	authz.DefineRequestSchema("account.create", CreateAccountRequest{})
	authz.DefineRequestSchema("account.update", UpdateAccountRequest{})
	authz.DefineRequestSchema("account.bulk", BulkUpdateAccountsRequest{})
	authz.DefineRequestSchema("account.batch", batchCreateAccountRequest{})
	authz.DefineRequestSchema("account.import", DataImportRequest{})
}
