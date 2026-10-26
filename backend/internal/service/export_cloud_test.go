//go:build unit

package service

import (
	"context"
	"encoding/json"
)

// Test-only exports so an external test package can drive the Cloud actor
// against the real Redis-backed session store (repository imports service, so
// that integration cannot live inside package service).

type CloudTestWire = cloudRPCFakeConn
type CloudTestRPCMessage = cloudRPCMessage

func NewCloudTestWire() *CloudTestWire { return newCloudRPCFakeConn() }

// CloudTestNextMessage returns the next JSON-RPC frame the gateway wrote to the
// fake Cloud connection.
func CloudTestNextMessage(ctx context.Context, wire *CloudTestWire) (CloudTestRPCMessage, error) {
	select {
	case raw := <-wire.writes:
		var msg cloudRPCMessage
		if err := json.Unmarshal(raw, &msg); err != nil {
			return cloudRPCMessage{}, err
		}
		return msg, nil
	case <-ctx.Done():
		return cloudRPCMessage{}, ctx.Err()
	}
}

// CloudTestSend delivers one frame from the fake Cloud server to the gateway.
func CloudTestSend(wire *CloudTestWire, value any) { cloudTestRead(wire, value) }

// CloudTestSessions builds the session manager exactly as production wires it,
// except that every upstream connection is the supplied fake wire.
func CloudTestSessions(store OpenAICloudSessionStore, wire *CloudTestWire) *OpenAICloudSessions {
	return NewOpenAICloudSessions(store, func(ctx context.Context, _ OpenAICloudOwner) (*cloudRPC, error) {
		rpc := newCloudRPC(ctx, wire)
		if err := rpc.Initialize(ctx); err != nil {
			return nil, err
		}
		return rpc, nil
	}, func(context.Context, OpenAICloudOwner) error { return nil })
}
