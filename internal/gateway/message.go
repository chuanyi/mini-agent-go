package gateway

// GatewayMessage is the unified message envelope passed between channels and the gateway.
type GatewayMessage struct {
	ChannelType string // e.g. "serverim"
	AccountID   string // bot account ID (derived from token prefix or configured)
	SenderID    string // user/chat ID — used to route replies back to the sender
	Content     string // message text
}

// key returns a unique string identifying a channel instance.
func key(channelType, accountID string) string {
	return channelType + ":" + accountID
}
