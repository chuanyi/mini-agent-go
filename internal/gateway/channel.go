package gateway

import "context"

// IMChannel is the interface that every IM channel adapter must implement.
// Add a new IM platform by implementing this interface and registering it in gateway.go.
type IMChannel interface {
	// Type returns the channel type identifier, e.g. "serverim".
	Type() string

	// AccountID returns the bot account ID for this channel instance.
	AccountID() string

	// Start begins receiving messages and forwards them to inbound.
	// It blocks until ctx is cancelled or a fatal error occurs.
	Start(ctx context.Context, inbound chan<- *GatewayMessage) error

	// Send delivers a reply message through this channel.
	Send(ctx context.Context, msg *GatewayMessage) error

	// Stop shuts down the channel gracefully.
	Stop() error
}
