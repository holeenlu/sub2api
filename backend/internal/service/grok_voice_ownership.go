package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

func mediaPrincipal(c *gin.Context) (*APIKey, error) {
	if c == nil {
		return nil, ErrMediaNotOwned
	}
	v, ok := c.Get("api_key")
	if !ok {
		return nil, ErrMediaNotOwned
	}
	k, ok := v.(*APIKey)
	if !ok || k == nil || k.UserID <= 0 {
		return nil, ErrMediaNotOwned
	}
	return k, nil
}

// Only documented built-ins are account-independent. Unknown identifiers must
// have durable provenance; old shared upstream voices are never auto-adopted.
func isBuiltinGrokVoice(id string) bool {
	switch strings.ToLower(id) {
	case "ara", "rex", "sal", "eve", "leo":
		return true
	}
	return false
}

func grokVoiceReferences(body []byte) ([]string, error) {
	if len(body) == 0 {
		return nil, nil
	}
	if err := ValidateGatewaySecurityJSON(body); err != nil {
		return nil, err
	}
	var root any
	if err := json.Unmarshal(body, &root); err != nil {
		return nil, err
	}
	out := []string{}
	var walk func(any) error
	walk = func(x any) error {
		switch v := x.(type) {
		case map[string]any:
			for key, value := range v {
				if strings.EqualFold(key, "voice") || strings.EqualFold(key, "voice_id") {
					if key != "voice" && key != "voice_id" {
						return fmt.Errorf("noncanonical voice field")
					}
					switch ref := value.(type) {
					case string:
						if ref != "" {
							out = append(out, ref)
						}
					case map[string]any:
						for field := range ref {
							if field != "id" && field != "type" {
								return fmt.Errorf("unsupported voice reference field")
							}
						}
						id, ok := ref["id"].(string)
						if !ok || id == "" {
							return fmt.Errorf("invalid voice reference")
						}
						out = append(out, id)
					default:
						return fmt.Errorf("invalid voice reference")
					}
				} else if err := walk(value); err != nil {
					return err
				}
			}
		case []any:
			for _, item := range v {
				if err := walk(item); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := walk(root); err != nil {
		return nil, err
	}
	return out, nil
}

// ResolveGrokVoiceAccount checks every reference before selecting any upstream.
// The same user's keys share voices, but the owning group/account never changes.
func (s *OpenAIGatewayService) ResolveGrokVoiceAccount(ctx context.Context, c *gin.Context, endpoint string, body []byte) (int64, error) {
	key, err := mediaPrincipal(c)
	if err != nil {
		return 0, err
	}
	refs := []string{}
	if strings.HasPrefix(endpoint, "custom-voices/") {
		id := strings.Split(strings.TrimPrefix(endpoint, "custom-voices/"), "/")[0]
		if id == "" {
			return 0, ErrMediaNotOwned
		}
		refs = append(refs, id)
	}
	if endpoint == "tts" || endpoint == "realtime" {
		r, err := grokVoiceReferences(body)
		if err != nil {
			return 0, err
		}
		for _, id := range r {
			if !isBuiltinGrokVoice(id) {
				refs = append(refs, id)
			}
		}
	}
	var account int64
	for _, id := range refs {
		if s.mediaRepo == nil {
			return 0, ErrMediaNotOwned
		}
		v, err := s.mediaRepo.GetVoice(ctx, derefGroupID(key.GroupID), key.UserID, id)
		if err != nil {
			return 0, err
		}
		if account != 0 && account != v.AccountID {
			return 0, ErrMediaNotOwned
		}
		account = v.AccountID
	}
	return account, nil
}

func (s *OpenAIGatewayService) ListOwnedGrokVoices(ctx context.Context, c *gin.Context) ([]json.RawMessage, error) {
	key, err := mediaPrincipal(c)
	if err != nil {
		return nil, err
	}
	if s.mediaRepo == nil {
		return nil, fmt.Errorf("media ownership repository unavailable")
	}
	voices, err := s.mediaRepo.ListVoices(ctx, derefGroupID(key.GroupID), key.UserID)
	if err != nil {
		return nil, err
	}
	out := make([]json.RawMessage, 0, len(voices))
	for _, v := range voices {
		out = append(out, v.Metadata)
	}
	return out, nil
}

func (s *OpenAIGatewayService) persistGrokVoice(ctx context.Context, c *gin.Context, account *Account, endpoint string, data []byte) error {
	if !strings.HasPrefix(endpoint, "custom-voices") {
		return nil
	}
	key, err := mediaPrincipal(c)
	if err != nil {
		return err
	}
	if s.mediaRepo == nil {
		return fmt.Errorf("media ownership repository unavailable")
	}
	parts := strings.Split(endpoint, "/")
	if c.Request.Method == http.MethodDelete && len(parts) == 2 {
		return s.mediaRepo.DeleteVoice(ctx, derefGroupID(key.GroupID), key.UserID, parts[1])
	}
	if len(parts) == 3 || c.Request.Method == http.MethodGet {
		return nil
	}
	id := gjson.GetBytes(data, "voice_id").String()
	if id == "" {
		id = gjson.GetBytes(data, "id").String()
	}
	if len(parts) == 2 {
		id = parts[1]
	}
	if id == "" || !gjson.ValidBytes(data) {
		return fmt.Errorf("upstream custom voice response has no identity")
	}
	return s.mediaRepo.PutVoice(ctx, &GatewayMediaVoice{ID: id, AccountID: account.ID, GroupID: derefGroupID(key.GroupID), UserID: key.UserID, Metadata: data})
}
