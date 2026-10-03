package handler

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestGatewayModelCatalogUsesKeyGroupWhitelistInsteadOfAccountInventory(t *testing.T) {
	group := &service.Group{ID: 71, Platform: service.PlatformOpenAI, AllowImageGeneration: true,
		ModelAllowlist: service.GroupModelAllowlist{Enabled: true, Models: []string{"chat-open", "gpt-image-supply", "sora-supply", "not-provided"}},
	}
	account := service.Account{ID: 1, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true,
		Credentials: map[string]any{"model_mapping": map[string]any{"chat-open": "chat-open", "chat-hidden": "chat-hidden", "gpt-image-supply": "gpt-image-supply", "sora-supply": "sora-supply"}},
	}
	repo := &gatewayModelsAccountRepoStub{byGroup: map[int64][]service.Account{71: {account}}}
	h := newGatewayModelsHandlerForTest(repo)
	h.modelCatalog = service.NewGroupModelCatalogService(repo, sharedCatalogChannelRepo{}, nil, nil)
	result := requestModelForTest(h, group, "", "")
	require.Equal(t, http.StatusOK, result.Code)
	var response gatewayModelsResponseForTest
	require.NoError(t, json.Unmarshal(result.Body.Bytes(), &response))
	require.Equal(t, []string{"chat-open", "gpt-image-supply", "sora-supply"}, modelIDsForTest(response.Data))
	for _, hidden := range []string{"chat-hidden", "mapping-only", "not-provided"} {
		require.Equal(t, http.StatusNotFound, requestModelForTest(h, group, hidden, "").Code)
	}
	group.ModelAllowlist.Models = []string{"chat-hidden"}
	result = requestModelForTest(h, group, "", "")
	require.NoError(t, json.Unmarshal(result.Body.Bytes(), &response))
	require.Equal(t, []string{"chat-hidden"}, modelIDsForTest(response.Data), "the group selection, not the account inventory, controls what a key lists")
	group.ModelAllowlist = service.GroupModelAllowlist{}
	result = requestModelForTest(h, group, "", "")
	require.NoError(t, json.Unmarshal(result.Body.Bytes(), &response))
	require.ElementsMatch(t, []string{"chat-open", "chat-hidden", "gpt-image-supply", "sora-supply"}, modelIDsForTest(response.Data), "an unrestricted group uses the native account mapping list")
}
