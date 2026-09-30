package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// ValidateGatewaySecurityJSON runs before any gjson lookup or map normalization.
// Object keys are decoded (including Unicode escapes); security envelope fields
// must use their canonical spelling and cannot have duplicate representations.
func ValidateGatewaySecurityJSON(body []byte) error {
	d := json.NewDecoder(bytes.NewReader(body))
	d.UseNumber()
	var object func(bool) error
	object = func(envelope bool) error {
		t, err := d.Token()
		if err != nil {
			return err
		}
		if delim, ok := t.(json.Delim); ok {
			switch delim {
			case '{':
				seen := map[string]bool{}
				for d.More() {
					k, err := d.Token()
					if err != nil {
						return err
					}
					key, ok := k.(string)
					if !ok {
						return fmt.Errorf("invalid JSON object key")
					}
					canonical := strings.ToLower(key)
					sensitive := canonical == "voice" || canonical == "voice_id" || envelope && (canonical == "previous_response_id" || canonical == "model" || canonical == "type" || canonical == "response" || canonical == "session" || canonical == "voice" || canonical == "voice_id")
					if sensitive && (seen[canonical] || key != canonical) {
						return fmt.Errorf("ambiguous JSON field %q", key)
					}
					seen[key] = true
					if err := object(envelope && (key == "response" || key == "session")); err != nil {
						return err
					}
				}
				_, err = d.Token()
				return err
			case '[':
				for d.More() {
					if err := object(false); err != nil {
						return err
					}
				}
				_, err = d.Token()
				return err
			default:
				return fmt.Errorf("invalid JSON delimiter")
			}
		}
		return nil
	}
	if err := object(true); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return fmt.Errorf("invalid trailing JSON")
	}
	if len(bytes.TrimSpace(body)) == 0 || bytes.TrimSpace(body)[0] != '{' {
		return fmt.Errorf("request must be a JSON object")
	}
	// Responses ingress consumes a top-level continuation. Do not permit a second
	// continuation hidden inside a Realtime-style response/session envelope.
	for _, path := range []string{"response.previous_response_id", "session.previous_response_id"} {
		if gjson.GetBytes(body, path).Exists() {
			return fmt.Errorf("previous_response_id must be top-level")
		}
	}
	v := gjson.GetBytes(body, "previous_response_id")
	if v.Exists() && v.Type != gjson.String && v.Type != gjson.Null {
		return fmt.Errorf("previous_response_id must be a string")
	}
	return nil
}

func (s *OpenAIGatewayService) validateWSContinuation(ctx context.Context, c *gin.Context, body []byte) error {
	if err := ValidateGatewaySecurityJSON(body); err != nil {
		return NewOpenAIWSClientCloseError(coderws.StatusPolicyViolation, err.Error(), err)
	}
	// Internal protocol conversions may enter Forward without the Responses
	// handler's marker. Derive the same owner from the authenticated API key.
	if c == nil {
		return nil
	}
	raw, ok := c.Get(openAIHTTPResponseOwnerContextKey)
	if !ok {
		if v, exists := c.Get("api_key"); exists {
			if key, valid := v.(*APIKey); valid && key != nil {
				userID := key.UserID
				if userID <= 0 && key.User != nil {
					userID = key.User.ID
				}
				if userID > 0 && key.ID > 0 {
					SetOpenAIHTTPResponseOwner(c, userID, key.ID)
					raw, ok = c.Get(openAIHTTPResponseOwnerContextKey)
				}
			}
		}
	}
	id := strings.TrimSpace(gjson.GetBytes(body, "previous_response_id").String())
	if id == "" {
		return nil
	}
	// Non-ingress internal calls with no authenticated tenant have no principal
	// to bind. Every public handler supplies a verified API key and user.
	if !ok {
		return nil
	}
	owner, ok := raw.(openAIHTTPResponseOwner)
	if !ok {
		return NewOpenAIWSClientCloseError(coderws.StatusPolicyViolation, "missing response owner", nil)
	}
	owned, err := s.ValidateOpenAIHTTPResponseOwner(ctx, getOpenAIGroupIDFromContext(c), id, owner.userID, owner.apiKeyID)
	if err != nil || !owned {
		return NewOpenAIWSClientCloseError(coderws.StatusPolicyViolation, "previous_response_id is not available for this user", err)
	}
	return nil
}

func openAIWSTenantScope(c *gin.Context) string {
	if c != nil {
		if raw, ok := c.Get(openAIHTTPResponseOwnerContextKey); ok {
			if owner, ok := raw.(openAIHTTPResponseOwner); ok && owner.userID > 0 {
				return fmt.Sprintf("%d/user/%d", getOpenAIGroupIDFromContext(c), owner.userID)
			}
		}
	}
	return fmt.Sprintf("%d/key/%d", getOpenAIGroupIDFromContext(c), getAPIKeyIDFromContext(c))
}
